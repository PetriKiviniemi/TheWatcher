package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// fakes stands in for the city's API and ntfy; tests change its fields between checks.
type fakes struct {
	sites      []Site
	apiStatus  int
	ntfyStatus int
	sent       []ntfyMessage
}

func newTestApp(t *testing.T) (*Store, *Watcher, *fakes) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakes{sites: []Site{site(nil)}, apiStatus: http.StatusOK, ntfyStatus: http.StatusOK}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(f.apiStatus)
		json.NewEncoder(w).Encode(f.sites)
	}))
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m ntfyMessage
		json.NewDecoder(r.Body).Decode(&m)
		if f.ntfyStatus == http.StatusOK {
			f.sent = append(f.sent, m)
		}
		w.WriteHeader(f.ntfyStatus)
	}))
	t.Cleanup(func() { api.Close(); ntfy.Close() })
	w := NewWatcher(store)
	w.apiBase, w.ntfyURL = api.URL+"/", ntfy.URL
	return store, w, f
}

// checkAt runs a scheduled check at the given time with a fresh plot list.
func checkAt(w *Watcher, at time.Time) {
	w.cachedAt = time.Time{}
	w.checkDue(context.Background(), at)
}

func TestWatcherAlertsOnlyNewPlots(t *testing.T) {
	store, w, f := newTestApp(t)
	store.Update("anna", func(p *Profile) { p.Topic = "topic1" })
	start := time.Now()

	checkAt(w, start)
	if len(f.sent) != 0 || len(store.Get("anna").Matches) != 1 {
		t.Fatalf("first check must baseline silently; sent=%v", f.sent)
	}
	f.sites = append(f.sites, site(func(s *Site) { s.Id = 2; s.Address = "Uusitie 1" }))
	checkAt(w, start.Add(29*time.Minute))
	if len(f.sent) != 0 {
		t.Fatal("must not check before the interval has passed")
	}
	checkAt(w, start.Add(31*time.Minute))
	if len(f.sent) != 1 || f.sent[0].Topic != "topic1" || !strings.Contains(f.sent[0].Message, "Uusitie 1") {
		t.Fatalf("want one alert about Uusitie 1, got %+v", f.sent)
	}
	checkAt(w, start.Add(62*time.Minute))
	if len(f.sent) != 1 {
		t.Fatalf("an announced plot must not be announced again, got %d alerts", len(f.sent))
	}
}

func TestGuardBlocksForeignSites(t *testing.T) {
	store, w, _ := newTestApp(t)
	h := newServer(store, w, "47321", fstest.MapFS{"index.html": {Data: []byte("ok")}}, func() {})
	body := `{"topic":"","intervalMinutes":30,"filter":{"unitClassIds":[],"keywords":[]}}`

	cases := []struct {
		name, host, origin, contentType string
		want                            int
	}{
		{"own origin", "127.0.0.1:47321", "http://127.0.0.1:47321", "application/json", http.StatusOK},
		{"DNS rebinding host", "evil.example:47321", "http://evil.example:47321", "application/json", http.StatusForbidden},
		{"cross-site origin", "127.0.0.1:47321", "https://evil.example", "application/json", http.StatusForbidden},
		{"form post", "127.0.0.1:47321", "", "text/plain", http.StatusUnsupportedMediaType},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPut, "/api/profiles/anna", strings.NewReader(body))
		r.Host = c.host
		r.Header.Set("Origin", c.origin)
		r.Header.Set("Content-Type", c.contentType)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body)
		}
	}
}

func TestSettingsValidation(t *testing.T) {
	bad := []Settings{
		{IntervalMinutes: 9},
		{IntervalMinutes: 30, Topic: "has spaces"},
		{IntervalMinutes: 30, Filter: Filter{MaxPrice: num(-1)}},
	}
	for _, s := range bad {
		if _, err := s.Validated(); err == nil {
			t.Errorf("want error for %+v", s)
		}
	}
}

func TestCorruptStoreIsMovedAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	os.WriteFile(path, []byte("null"), 0o600)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update("anna", func(*Profile) {}); err != nil {
		t.Fatal(err)
	}
}

func TestQuitStopsOnlyFromOwnPage(t *testing.T) {
	store, w, _ := newTestApp(t)
	quits := 0
	h := newServer(store, w, "47321", fstest.MapFS{}, func() { quits++ })
	for origin, want := range map[string]int{"https://evil.example": http.StatusForbidden, "http://127.0.0.1:47321": http.StatusNoContent} {
		r := httptest.NewRequest(http.MethodPost, "/api/quit", strings.NewReader("{}"))
		r.Host = "127.0.0.1:47321"
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d", origin, rec.Code, want)
		}
	}
	if quits != 1 {
		t.Fatalf("want exactly one quit (own origin), got %d", quits)
	}
}

func TestFailedAlertIsRetried(t *testing.T) {
	store, w, f := newTestApp(t)
	store.Update("anna", func(p *Profile) { p.Topic = "topic1" })
	start := time.Now()
	checkAt(w, start)

	f.sites = append(f.sites, site(func(s *Site) { s.Id = 2 }))
	f.ntfyStatus = http.StatusTooManyRequests
	checkAt(w, start.Add(31*time.Minute))
	if store.Get("anna").LastError == "" {
		t.Fatal("a failed alert must be reported in the UI")
	}
	f.ntfyStatus = http.StatusOK
	checkAt(w, start.Add((31+minIntervalMinutes)*time.Minute)) // retried sooner, not a full interval later
	if len(f.sent) != 1 || store.Get("anna").LastError != "" {
		t.Fatalf("want the plot alerted on retry and the error cleared, got %+v / %q", f.sent, store.Get("anna").LastError)
	}
}

func TestFailedCheckIsRetriedSooner(t *testing.T) {
	store, w, f := newTestApp(t)
	store.Update("anna", func(p *Profile) { p.Topic = "topic1"; p.IntervalMinutes = maxIntervalMinutes })
	start := time.Now()
	checkAt(w, start)

	f.apiStatus = http.StatusServiceUnavailable
	checkAt(w, start.Add(24*time.Hour))
	if store.Get("anna").LastError == "" {
		t.Fatal("a failed fetch must be reported in the UI")
	}
	f.apiStatus = http.StatusOK
	f.sites = append(f.sites, site(func(s *Site) { s.Id = 2 }))
	checkAt(w, start.Add(24*time.Hour+minIntervalMinutes*time.Minute))
	if len(f.sent) != 1 {
		t.Fatal("after a failed check, a daily profile must retry within the minimum interval, not a day later")
	}
}

func TestNoTopicStaysQuiet(t *testing.T) {
	store, w, f := newTestApp(t)
	store.Update("anna", func(*Profile) {})
	start := time.Now()
	checkAt(w, start)
	f.sites = append(f.sites, site(func(s *Site) { s.Id = 2 }))
	checkAt(w, start.Add(31*time.Minute))
	if len(f.sent) != 0 || len(store.Get("anna").Matches) != 2 {
		t.Fatalf("without a topic nothing is sent but matches still update; sent=%v", f.sent)
	}
}

func TestAlertMessage(t *testing.T) {
	one := alert("t", "anna", []Site{site(nil)})
	if one.Click != siteURL(site(nil)) || !strings.Contains(one.Message, "Kellonlahdentie 5") {
		t.Errorf("a single plot must link to itself: %+v", one)
	}
	many := make([]Site, maxAlertLines+3)
	for i := range many {
		many[i] = site(func(s *Site) { s.Id = i })
	}
	msg := alert("t", "anna", many)
	if lines := strings.Count(msg.Message, "\n") + 1; lines != maxAlertLines+1 || !strings.HasSuffix(msg.Message, "…and 3 more") {
		t.Errorf("want %d lines ending in a summary, got %d:\n%s", maxAlertLines+1, lines, msg.Message)
	}
}
