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

func newTestApp(t *testing.T) (*Store, *Watcher, *[]ntfyMessage, *[]Site) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	sites := []Site{site(nil)}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(sites)
	}))
	sent := []ntfyMessage{}
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m ntfyMessage
		json.NewDecoder(r.Body).Decode(&m)
		sent = append(sent, m)
	}))
	t.Cleanup(func() { api.Close(); ntfy.Close() })
	w := NewWatcher(store)
	w.apiBase, w.ntfyURL = api.URL+"/", ntfy.URL
	return store, w, &sent, &sites
}

func TestWatcherAlertsOnlyNewPlots(t *testing.T) {
	store, w, sent, sites := newTestApp(t)
	store.Update("anna", func(p *Profile) { p.Topic = "topic1" })

	w.checkDue(context.Background(), time.Now())
	if len(*sent) != 0 || len(store.Get("anna").Matches) != 1 {
		t.Fatalf("first check must baseline silently; sent=%v", *sent)
	}
	*sites = append(*sites, site(func(s *Site) { s.Id = 2; s.Address = "Uusitie 1" }))
	w.cachedAt = time.Time{} // expire the plot list cache
	w.checkDue(context.Background(), time.Now().Add(29*time.Minute))
	if len(*sent) != 0 {
		t.Fatal("must not check before the interval has passed")
	}
	w.checkDue(context.Background(), time.Now().Add(31*time.Minute))
	if len(*sent) != 1 || (*sent)[0].Topic != "topic1" || !strings.Contains((*sent)[0].Message, "Uusitie 1") {
		t.Fatalf("want one alert about Uusitie 1, got %+v", *sent)
	}
}

func TestGuardBlocksForeignSites(t *testing.T) {
	store, w, _, _ := newTestApp(t)
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
	store, w, _, _ := newTestApp(t)
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
