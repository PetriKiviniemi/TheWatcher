package main

import (
	"reflect"
	"testing"
)

func num(v float64) *float64 { return &v }

func site(mod func(*Site)) Site {
	s := Site{Id: 1, UnitClassId: 1, SubArea: "Virpiniemi, Kellonlahti", Address: "Kellonlahdentie 5",
		PropertyLabel: "564-1-2-3", Area: num(900), Price: num(50000)}
	if mod != nil {
		mod(&s)
	}
	return s
}

func ids(sites []Site) []int {
	out := []int{}
	for _, s := range sites {
		out = append(out, s.Id)
	}
	return out
}

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		f    Filter
		s    Site
		want bool
	}{
		{"empty filter matches all", Filter{}, site(nil), true},
		{"keyword any-of, case-insensitive", Filter{Keywords: []string{"foo", "KELLONLAHTI"}}, site(nil), true},
		{"keyword miss", Filter{Keywords: []string{"Haukipudas"}}, site(nil), false},
		{"price within limit", Filter{MaxPrice: num(60000)}, site(nil), true},
		{"price over limit", Filter{MaxPrice: num(40000)}, site(nil), false},
		{"price limit excludes rent-only plot", Filter{MaxPrice: num(60000)}, site(func(s *Site) { s.Price = nil }), false},
		{"blank keywords are ignored", Filter{Keywords: []string{" ", ""}}, site(nil), true},
		{"keyword matches property label", Filter{Keywords: []string{"564-1-2"}}, site(nil), true},
		{"rent within limit", Filter{MaxRent: num(1000)}, site(func(s *Site) { s.Rent = num(900) }), true},
		{"rent limit excludes sale-only plot", Filter{MaxRent: num(1000)}, site(nil), false},
		{"unit class mismatch", Filter{UnitClassIds: []int{4}}, site(nil), false},
		{"min area excludes unknown area", Filter{MinArea: num(1)}, site(func(s *Site) { s.Area = nil }), false},
	}
	for _, c := range cases {
		if got := c.f.Normalized().Matches(c.s); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDiff(t *testing.T) {
	f := Filter{MaxPrice: num(60000)}
	two := site(func(s *Site) { s.Id = 2 })

	fresh, matched, st := Diff([]Site{site(nil)}, f, WatchState{})
	if matched == nil || fresh == nil {
		t.Fatal("results must be non-nil so they encode as JSON []")
	}
	if len(fresh) != 0 {
		t.Fatal("first run must baseline silently")
	}
	fresh, matched, st = Diff([]Site{site(nil), two}, f, st)
	if !reflect.DeepEqual(ids(fresh), []int{2}) || !reflect.DeepEqual(ids(matched), []int{1, 2}) {
		t.Fatalf("want fresh [2] and matched [1 2], got %v and %v", ids(fresh), ids(matched))
	}
	if _, _, same := Diff(nil, Filter{MaxPrice: num(60000), Keywords: []string{" "}, UnitClassIds: []int{}}, st); same.FilterKey != st.FilterKey {
		t.Fatal("equivalent filters must share a key")
	}
	if fresh, _, _ := Diff([]Site{site(nil), two, site(func(s *Site) { s.Id = 3 })}, Filter{}, st); len(fresh) != 0 {
		t.Fatal("changed filter must re-baseline silently")
	}
	// Plot 1 leaves the range, then comes back: report it again.
	_, _, st = Diff([]Site{site(func(s *Site) { s.Price = num(70000) })}, f, st)
	fresh, _, _ = Diff([]Site{site(nil)}, f, st)
	if !reflect.DeepEqual(ids(fresh), []int{1}) {
		t.Fatalf("want re-entered plot 1, got %v", ids(fresh))
	}
}
