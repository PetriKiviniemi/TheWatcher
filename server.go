package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

const maxBodySize = 64 << 10

var usernamePattern = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,32}$`)

type server struct {
	store   *Store
	watcher *Watcher
	hosts   map[string]bool
	quit    func()

	unitClassesMu sync.Mutex
	unitClasses   json.RawMessage // cached after the first successful fetch
}

func newServer(store *Store, watcher *Watcher, port string, static fs.FS, quit func()) http.Handler {
	s := &server{store: store, watcher: watcher, quit: quit,
		hosts: map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/profiles/{name}", s.getProfile)
	mux.HandleFunc("PUT /api/profiles/{name}", s.putProfile)
	mux.HandleFunc("POST /api/profiles/{name}/test", s.testAlert)
	mux.HandleFunc("GET /api/unit-classes", s.getUnitClasses)
	mux.HandleFunc("POST /api/quit", s.postQuit)
	mux.Handle("GET /", http.FileServerFS(static))
	return s.guard(mux)
}

// guard keeps other websites in the user's browser away from this local API:
// the Host check defeats DNS rebinding; the Origin and JSON content-type checks defeat cross-site writes.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hosts[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && !s.hosts[strings.TrimPrefix(origin, "http://")] {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
				http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *server) username(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := strings.ToLower(r.PathValue("name"))
	if !usernamePattern.MatchString(name) {
		writeError(w, http.StatusBadRequest, "a username is 1–32 letters, digits, '.', '_' or '-'")
		return "", false
	}
	return name, true
}

func (s *server) getProfile(w http.ResponseWriter, r *http.Request) {
	if name, ok := s.username(w, r); ok {
		writeJSON(w, s.store.Get(name).View())
	}
}

func (s *server) putProfile(w http.ResponseWriter, r *http.Request) {
	name, ok := s.username(w, r)
	if !ok {
		return
	}
	var in Settings
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings")
		return
	}
	in, err := in.Validated()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	secret, err := nextEmailSecret(in.Email, s.store.Get(name))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Email.Password = "" // only the encrypted copy is stored
	if err := s.store.Update(name, func(p *Profile) { p.Settings, p.EmailSecret, p.EmailError = in, secret, "" }); err != nil {
		log.Printf("saving %q: %v", name, err)
		writeError(w, http.StatusInternalServerError, "could not save settings")
		return
	}
	// Refresh matches so the UI reflects the new filter at once; finish even if the tab closes.
	s.watcher.CheckNow(context.WithoutCancel(r.Context()), name)
	writeJSON(w, s.store.Get(name).View())
}

func (s *server) testAlert(w http.ResponseWriter, r *http.Request) {
	name, ok := s.username(w, r)
	if !ok {
		return
	}
	p := s.store.Get(name)
	if !p.hasChannel() {
		writeError(w, http.StatusBadRequest, "save an ntfy topic or an email address first")
		return
	}
	text := "Plot alerts for " + name + " will arrive here."
	_, pushErr, emailErr := s.watcher.deliver(r.Context(), p, Alert{Title: "Tonttivahti test", Short: text, Body: text, Click: searchPageURL})
	if p.Email.Enabled() {
		s.store.Update(name, func(q *Profile) { q.EmailError = errText(emailErr) })
	}
	if err := errors.Join(pushErr, emailErr); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) getUnitClasses(w http.ResponseWriter, r *http.Request) {
	s.unitClassesMu.Lock()
	classes := s.unitClasses
	s.unitClassesMu.Unlock()
	if classes == nil {
		if err := s.watcher.GetJSON(r.Context(), "UnitClasses?locale=fi", &classes); err != nil {
			writeError(w, http.StatusBadGateway, "could not load plot types from the city's service")
			return
		}
		s.unitClassesMu.Lock()
		s.unitClasses = classes
		s.unitClassesMu.Unlock()
	}
	writeJSON(w, classes)
}

// postQuit stops the whole program; the guard ensures only this app's own page can call it.
func (s *server) postQuit(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	s.quit()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writing response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
