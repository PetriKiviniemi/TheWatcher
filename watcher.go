package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIBase  = "https://kartta.ouka.fi/eSite/api/SiteSearchApi/"
	defaultNtfyURL  = "https://ntfy.sh/"
	searchPageURL   = "https://kartta.ouka.fi/eSite/fi/SiteSearch"
	userAgent       = "Tonttivahti/1.0 (personal plot alert)"
	maxAlertLines   = 10
	maxResponseSize = 10 << 20
	siteListTTL     = 2 * time.Minute // caps API traffic no matter how often settings are saved
)

type ntfyMessage struct {
	Topic   string   `json:"topic"`
	Title   string   `json:"title"`
	Message string   `json:"message"`
	Click   string   `json:"click,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// Watcher polls the plot API for every due profile and sends ntfy alerts for new matches.
type Watcher struct {
	mu      sync.Mutex // one check at a time, so the same plot is never alerted twice
	store   *Store
	client  *http.Client
	apiBase string
	ntfyURL string
	smtpTLS *tls.Config // nil in production; tests trust their fake server's certificate

	cachedSites []Site // guarded by mu
	cachedAt    time.Time
}

func NewWatcher(store *Store) *Watcher {
	return &Watcher{store: store, client: &http.Client{Timeout: 30 * time.Second}, apiBase: defaultAPIBase, ntfyURL: defaultNtfyURL}
}

// Run checks due profiles once a minute until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		w.checkDue(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Watcher) checkDue(ctx context.Context, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	due := w.store.Due(now)
	if len(due) == 0 {
		return
	}
	sites, err := w.fetchSites(ctx) // one fetch serves every due profile
	for _, name := range due {
		w.check(ctx, name, sites, err, now)
	}
}

// CheckNow checks one profile immediately, e.g. right after its settings change.
func (w *Watcher) CheckNow(ctx context.Context, name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sites, err := w.fetchSites(ctx)
	w.check(ctx, name, sites, err, time.Now())
}

func (w *Watcher) check(ctx context.Context, name string, sites []Site, err error, now time.Time) {
	p := w.store.Get(name)
	matched, next, emailError := p.Matches, p.State, p.EmailError
	if err == nil {
		var fresh []Site
		fresh, matched, next = Diff(sites, p.Filter, p.State)
		if len(fresh) > 0 && p.hasChannel() {
			delivered, pushErr, emailErr := w.deliver(ctx, p, alert(name, fresh))
			if !delivered {
				next = p.State // keep the plots unannounced so the next check retries
			}
			if p.Email.Enabled() {
				emailError = errText(emailErr)
			}
			err = errors.Join(pushErr, emailErr)
		}
	}
	if err != nil {
		log.Printf("check %q: %v", name, err)
	}
	if err := w.store.Update(name, func(q *Profile) {
		q.State, q.Matches, q.LastCheck, q.LastError, q.EmailError = next, matched, now, errText(err), emailError
	}); err != nil {
		log.Printf("saving %q: %v", name, err)
	}
}

// fetchSites must be called with mu held.
func (w *Watcher) fetchSites(ctx context.Context) ([]Site, error) {
	if time.Since(w.cachedAt) < siteListTTL {
		return w.cachedSites, nil
	}
	var sites []Site
	if err := w.GetJSON(ctx, "SiteList", &sites); err != nil {
		return nil, fmt.Errorf("fetching plots: %w", err)
	}
	// Treating an empty list as data would make every plot "new" again once the API recovers.
	if len(sites) == 0 {
		return nil, errors.New("the plot list was empty; the API may have changed")
	}
	w.cachedSites, w.cachedAt = sites, time.Now()
	return sites, nil
}

// GetJSON fetches path from the plot API and decodes it into v.
func (w *Watcher) GetJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.apiBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", path, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxResponseSize)).Decode(v)
}

// Alert is one notification, rendered short for push and in full for email.
type Alert struct {
	Title string
	Short string // push text: at most maxAlertLines plots
	Body  string // email text: every plot with its link
	Click string
}

// deliver sends a to every channel the profile has. It counts as delivered if any channel succeeded,
// so one broken channel cannot cause repeat alerts on the others; failures are still returned.
func (w *Watcher) deliver(ctx context.Context, p Profile, a Alert) (delivered bool, pushErr, emailErr error) {
	if p.Topic != "" {
		pushErr = w.Notify(ctx, p.Topic, a)
		delivered = pushErr == nil
	}
	if p.Email.Enabled() {
		if emailErr = w.email(ctx, p, a); emailErr != nil {
			emailErr = fmt.Errorf("sending email: %w", emailErr)
		} else {
			delivered = true
		}
	}
	return delivered, pushErr, emailErr
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (w *Watcher) email(ctx context.Context, p Profile, a Alert) error {
	password, err := unprotect(p.EmailSecret)
	if err != nil {
		return fmt.Errorf("reading the saved app password: %w", err)
	}
	return sendMail(ctx, p.Email, string(password), a.Title, a.Body, w.smtpTLS)
}

func (w *Watcher) Notify(ctx context.Context, topic string, a Alert) error {
	msg := ntfyMessage{Topic: topic, Title: a.Title, Message: a.Short, Click: a.Click, Tags: []string{"house"}}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.ntfyURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	res, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending alert: %w", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("sending alert: ntfy returned HTTP %d", res.StatusCode)
	}
	return nil
}

func alert(username string, fresh []Site) Alert {
	short, full := []string{}, []string{}
	for i, s := range fresh {
		if i < maxAlertLines {
			short = append(short, describe(s))
		} else if i == maxAlertLines {
			short = append(short, fmt.Sprintf("…and %d more", len(fresh)-i))
		}
		full = append(full, describe(s)+"\n"+siteURL(s))
	}
	click := searchPageURL
	if len(fresh) == 1 {
		click = siteURL(fresh[0])
	}
	return Alert{
		Title: fmt.Sprintf("%d new plot(s) for %s", len(fresh), username),
		Short: strings.Join(short, "\n"),
		Body:  strings.Join(full, "\n\n") + "\n\nAll plots: " + searchPageURL,
		Click: click,
	}
}

func describe(s Site) string {
	parts := []string{s.Address + ", " + s.SubArea}
	if s.Area != nil {
		parts = append(parts, fmt.Sprintf("%.0f m²", *s.Area))
	}
	if s.Price != nil {
		parts = append(parts, fmt.Sprintf("%.0f €", *s.Price))
	}
	if s.Rent != nil {
		parts = append(parts, fmt.Sprintf("rent %.0f €/yr", *s.Rent))
	}
	return strings.Join(parts, " · ")
}
