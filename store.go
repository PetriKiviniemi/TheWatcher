package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"regexp"
	"sync"
	"time"
)

const (
	minIntervalMinutes = 10 // keeps the load on the city's API polite
	maxIntervalMinutes = 24 * 60
	maxKeywords        = 20
	maxKeywordLength   = 60
)

var topicPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Settings is what the user edits in the UI.
type Settings struct {
	Topic           string        `json:"topic"`
	Email           EmailSettings `json:"email"`
	IntervalMinutes int           `json:"intervalMinutes"`
	Filter          Filter        `json:"filter"`
}

// Status is what the watcher reports back to the UI.
type Status struct {
	LastCheck time.Time `json:"lastCheck"`
	LastError string    `json:"lastError"`
	// EmailError stays until an email gets through or the settings are saved, because a push that
	// still works would otherwise hide a broken email setup after the next quiet check.
	EmailError string `json:"emailError"`
	Matches    []Site `json:"matches"`
}

type Profile struct {
	Settings
	Status
	State       WatchState `json:"state"`
	EmailSecret []byte     `json:"emailSecret,omitempty"` // the app password, encrypted with protect
}

// ProfileView is a profile as the UI sees it, without watcher state or the email password.
type ProfileView struct {
	Settings
	Status
	EmailPasswordSet bool `json:"emailPasswordSet"`
}

func newProfile() Profile {
	return Profile{
		Settings: Settings{IntervalMinutes: 30, Filter: Filter{}.Normalized()},
		Status:   Status{Matches: []Site{}},
	}
}

func (p Profile) View() ProfileView {
	v := ProfileView{p.Settings, p.Status, len(p.EmailSecret) > 0}
	v.Email = v.Email.withDefaults()
	v.Email.Password = ""
	return v
}

func (p Profile) hasChannel() bool { return p.Topic != "" || p.Email.Enabled() }

// Validated checks settings that came from the UI and returns them with a normalized filter.
func (s Settings) Validated() (Settings, error) {
	f := s.Filter
	switch {
	case s.Topic != "" && !topicPattern.MatchString(s.Topic):
		return s, errors.New("the ntfy topic may only contain letters A–Z, digits, '_' and '-' (at most 64)")
	case s.IntervalMinutes < minIntervalMinutes || s.IntervalMinutes > maxIntervalMinutes:
		return s, errors.New("the check interval must be between 10 and 1440 minutes")
	case len(f.Keywords) > maxKeywords || len(f.UnitClassIds) > 100:
		return s, errors.New("too many keywords or plot types")
	}
	for _, k := range f.Keywords {
		if len([]rune(k)) > maxKeywordLength {
			return s, errors.New("keywords can be at most 60 characters")
		}
	}
	for _, limit := range []*float64{f.MaxPrice, f.MaxRent, f.MinArea} {
		if limit != nil && *limit < 0 {
			return s, errors.New("limits cannot be negative")
		}
	}
	email, err := s.Email.withDefaults().validated()
	if err != nil {
		return s, err
	}
	s.Email = email
	s.Filter = f.Normalized()
	return s, nil
}

// Store keeps all profiles in one JSON file. Every change is written to disk immediately.
type Store struct {
	mu       sync.Mutex
	path     string
	profiles map[string]*Profile
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, profiles: map[string]*Profile{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &s.profiles)
	if err == nil && s.profiles == nil {
		err = errors.New("file contains null")
	}
	if err != nil {
		// Keep the damaged file for inspection and start fresh rather than refusing to run.
		backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		log.Printf("profiles file unreadable (%v); moved to %s", err, backup)
		s.profiles = map[string]*Profile{}
		return s, os.Rename(path, backup)
	}
	return s, nil
}

// Get returns a copy of the named profile, or a default profile if it does not exist yet.
func (s *Store) Get(name string) Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.profiles[name]; ok {
		return *p
	}
	return newProfile()
}

// Update applies fn to the named profile, creating it if needed, and saves the store.
func (s *Store) Update(name string, fn func(*Profile)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[name]
	if !ok {
		created := newProfile()
		p = &created
		s.profiles[name] = p
	}
	fn(p)
	return s.save()
}

// Due lists the profiles whose check interval has passed.
// After a failed check, a profile is retried within the minimum interval instead of waiting a full one.
func (s *Store) Due(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []string
	for name, p := range s.profiles {
		minutes := p.IntervalMinutes
		if p.LastError != "" {
			minutes = min(minutes, minIntervalMinutes)
		}
		if !now.Before(p.LastCheck.Add(time.Duration(minutes) * time.Minute)) {
			due = append(due, name)
		}
	}
	return due
}

// save writes to a temporary file first so a crash never leaves a half-written store.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
