package main

import (
	"bytes"
	"context"
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
	matched, next := p.Matches, p.State
	if err == nil {
		var fresh []Site
		fresh, matched, next = Diff(sites, p.Filter, p.State)
		if len(fresh) > 0 && p.Topic != "" {
			if err = w.Notify(ctx, alert(p.Topic, name, fresh)); err != nil {
				next = p.State // keep the plots unannounced so the next check retries
			}
		}
	}
	errText := ""
	if err != nil {
		errText = err.Error()
		log.Printf("check %q: %v", name, err)
	}
	if err := w.store.Update(name, func(q *Profile) {
		q.State, q.Matches, q.LastCheck, q.LastError = next, matched, now, errText
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

func (w *Watcher) Notify(ctx context.Context, msg ntfyMessage) error {
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

func alert(topic, username string, fresh []Site) ntfyMessage {
	lines := []string{}
	for i, s := range fresh {
		if i == maxAlertLines {
			lines = append(lines, fmt.Sprintf("…and %d more", len(fresh)-i))
			break
		}
		lines = append(lines, describe(s))
	}
	click := searchPageURL
	if len(fresh) == 1 {
		click = siteURL(fresh[0])
	}
	return ntfyMessage{
		Topic:   topic,
		Title:   fmt.Sprintf("%d new plot(s) for %s", len(fresh), username),
		Message: strings.Join(lines, "\n"),
		Click:   click,
		Tags:    []string{"house"},
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
