package main

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
)

// Site is a plot from eSite SiteSearchApi/SiteList, limited to the fields we use.
type Site struct {
	Id            int
	UnitClassId   int
	SubArea       string
	Address       string
	PropertyLabel string
	Area          *float64
	BuildingRight *float64
	Price         *float64
	Rent          *float64
}

// Filter holds a user's watch criteria. Empty lists and nil limits mean "any".
type Filter struct {
	UnitClassIds []int    `json:"unitClassIds"`
	Keywords     []string `json:"keywords"`
	MaxPrice     *float64 `json:"maxPrice"`
	MaxRent      *float64 `json:"maxRent"`
	MinArea      *float64 `json:"minArea"`
}

// WatchState is what the watcher remembers per user between checks.
type WatchState struct {
	FilterKey   string `json:"filterKey"`
	NotifiedIds []int  `json:"notifiedIds"`
}

// withinMax treats a missing value as not matching a set limit:
// a price limit excludes plots that have no price at all.
func withinMax(value, max *float64) bool {
	return max == nil || (value != nil && *value <= *max)
}

// Normalized returns the canonical form of f: trimmed lowercase keywords without blanks,
// sorted and de-duplicated lists, never nil. Equal filters therefore have equal keys.
func (f Filter) Normalized() Filter {
	keywords := []string{}
	for _, k := range f.Keywords {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			keywords = append(keywords, k)
		}
	}
	slices.Sort(keywords)
	f.Keywords = slices.Compact(keywords)
	f.UnitClassIds = slices.Compact(slices.Sorted(slices.Values(f.UnitClassIds)))
	if f.UnitClassIds == nil {
		f.UnitClassIds = []int{}
	}
	return f
}

// Matches expects a normalized filter.
func (f Filter) Matches(s Site) bool {
	if len(f.UnitClassIds) > 0 && !slices.Contains(f.UnitClassIds, s.UnitClassId) {
		return false
	}
	text := strings.ToLower(s.Address + " " + s.SubArea + " " + s.PropertyLabel)
	if len(f.Keywords) > 0 && !slices.ContainsFunc(f.Keywords, func(k string) bool { return strings.Contains(text, k) }) {
		return false
	}
	if f.MinArea != nil && (s.Area == nil || *s.Area < *f.MinArea) {
		return false
	}
	return withinMax(s.Price, f.MaxPrice) && withinMax(s.Rent, f.MaxRent)
}

func (f Filter) key() string {
	b, _ := json.Marshal(f) // cannot fail for this struct
	return string(b)
}

// Diff returns the plots that newly match since prev, and the state to store.
// A new or edited filter re-baselines silently, so saving a filter never floods the user.
func Diff(sites []Site, f Filter, prev WatchState) (fresh, matched []Site, next WatchState) {
	f = f.Normalized()
	fresh, matched = []Site{}, []Site{}
	seen := map[int]bool{}
	for _, id := range prev.NotifiedIds {
		seen[id] = true
	}
	next.FilterKey = f.key()
	next.NotifiedIds = []int{}
	for _, s := range sites {
		if !f.Matches(s) {
			continue
		}
		matched = append(matched, s)
		next.NotifiedIds = append(next.NotifiedIds, s.Id)
		if prev.FilterKey == next.FilterKey && !seen[s.Id] {
			fresh = append(fresh, s)
		}
	}
	return fresh, matched, next
}

// shortcut: links by property label only; eSite appends "/<Id>" when a label is not unique (none are today).
func siteURL(s Site) string {
	return "https://kartta.ouka.fi/eSite/fi/SiteSearch#/site/" + url.PathEscape(s.PropertyLabel)
}
