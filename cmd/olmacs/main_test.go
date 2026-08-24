package main

import (
	"testing"

	"github.com/andreiverdes/olmacs/internal/olx"
	"github.com/andreiverdes/olmacs/internal/site"
)

// IDkqOtQ, seen on the 14 Aug sweep: the seller swapped an M5 Max 128 GB for a
// plain M5 32 GB and the price fell by 15,300 lei. The new text states no readable
// memory size, so this must be retired rather than kept and repriced.
func TestReusedAdSlot(t *testing.T) {
	cases := []struct {
		name     string
		l        site.Listing
		o        olx.Offer
		wantGone bool
	}{
		{
			name:     "unchanged ad is left alone",
			l:        site.Listing{Title: "MacBook Pro 14 M4 Max 48GB", Chip: "M4 Max", RAM: 48},
			o:        olx.Offer{Title: "MacBook Pro 14 M4 Max 48GB"},
			wantGone: false,
		},
		{
			name:     "reworded, same machine",
			l:        site.Listing{Title: "MacBook Pro 14 M4 Max 48GB", Chip: "M4 Max", RAM: 48},
			o:        olx.Offer{Title: "Vand MacBook Pro 14 M4 Max 48GB RAM, ca nou"},
			wantGone: false,
		},
		{
			name: "swapped for a machine whose memory cannot be read",
			l: site.Listing{
				Title: " Apple MacBook Pro 14**M5 Max**18-core CPU 40-core GPU 128GB",
				Chip:  "M5 Max", RAM: 128,
			},
			o: olx.Offer{
				Title:       " Apple MacBook Pro 14**M5** 32 GB**4 TB",
				Description: "MacBook Pro 14 inch M5**Nou in cutie stocare 4 TB SSD",
			},
			wantGone: true,
		},
		{
			name: "swapped for a machine below the floor",
			l:    site.Listing{Title: "MacBook Pro 16 M4 Max 48GB", Chip: "M4 Max", RAM: 48},
			o:    olx.Offer{Title: "MacBook Air 13 M4 16GB RAM 256GB SSD"},
			// Air 16 GB is under the 36 GB laptop floor.
			wantGone: true,
		},
		{
			name: "swapped for a different qualifying machine",
			l:    site.Listing{Title: "MacBook Pro 16 M4 Max 48GB", Chip: "M4 Max", RAM: 48},
			o:    olx.Offer{Title: "MacBook Pro 16 M4 Pro 64GB RAM"},
			// Still qualifies, but it is not the machine the page has been showing.
			wantGone: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			note, gone := reused(&c.l, c.o)
			if gone != c.wantGone {
				t.Fatalf("reused() = (%q, %v), want gone=%v", note, gone, c.wantGone)
			}
			if gone && note == "" {
				t.Error("a retired slot must carry a note explaining why")
			}
			if !gone && note != "" {
				t.Errorf("a kept slot must carry no note, got %q", note)
			}
		})
	}
}

// The sweep that prompted this: OLX started blocking the client's HTTP/2
// fingerprint, every request came back 403, and the run was one -dry-run away
// from recording a sweep that said the market had not moved.
func TestReachabilityCheck(t *testing.T) {
	cases := []struct {
		name    string
		r       reachability
		wantErr bool
	}{
		{
			name: "everything answered",
			r:    reachability{recheckTried: 28, searchTried: 14},
		},
		{
			name: "one flaky listing is tolerated",
			r:    reachability{recheckTried: 28, recheckFailed: 1, searchTried: 14},
		},
		{
			name:    "blocked outright",
			r:       reachability{recheckTried: 28, recheckFailed: 28, searchTried: 14, searchFailed: 14},
			wantErr: true,
		},
		{
			name:    "re-checks answer but discovery is blocked",
			r:       reachability{recheckTried: 28, searchTried: 14, searchFailed: 14},
			wantErr: true,
		},
		{
			name:    "half the re-checks fail",
			r:       reachability{recheckTried: 28, recheckFailed: 14, searchTried: 14},
			wantErr: true,
		},
		{
			// The first ever run has nothing to re-check. That is not a failure to
			// reach OLX, and must not be read as one.
			name: "empty dataset, searches fine",
			r:    reachability{searchTried: 14},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.r.check()
			if c.wantErr && err == nil {
				t.Fatalf("check() = nil, want a refusal for %+v", c.r)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("check() = %v, want nil for %+v", err, c.r)
			}
		})
	}
}

// Curated notes are the only prose on the page that is not re-derived at render
// time, so they are the only prose that can go false while the page keeps
// publishing it. Four did in two days: "Cheapest machine on the page" after a
// 4 900 lei mini arrived, "Ties the cheapest 36 GB machines here" after two
// cheaper ones did, "Cheapest 48 GB machine on the page" after that listing was
// removed, and "Only 64 GB machine left" after a second 64 GB machine appeared.
// Each was a ranking, and a ranking is exactly the kind of claim a sweep can
// falsify — so a note that makes one is worth re-reading every sweep.
func TestRanksItsListing(t *testing.T) {
	ranks := []string{
		"Cheapest machine on the page now that the 4 500 lei minis are gone.",
		"Ties the cheapest 36 GB machines here.",
		"Cheapest 48 GB machine on the page.",
		"Only 64 GB machine left after the Alienstore ad went.",
		"The most expensive 24 GB mini listed here.",
		"Base M4, 10-core, 256 GB — the weakest build host of the minis.",
	}
	for _, s := range ranks {
		if !ranksItsListing(s) {
			t.Errorf("ranksItsListing(%q) = false, want true", s)
		}
	}
	// These state facts about the machine. Flagging them would train the reader
	// to skim the block, which costs more than it catches.
	plain := []string{
		"19 battery cycles, Space Black, full box. In-person handover preferred, no trades.",
		"Battery health 92%, 12–13 months of warranty left. Handover in Brașov only, no courier.",
		"Two small scratches, battery health 97%. Handover in Sibiu only, no courier.",
		"24 GB is assumed from the base M4 Pro mini config, not stated. Confirm before buying.",
		"Title says “M4 Pro Max”; the description specifies M4 Max, 14-core CPU / 32-core GPU.",
		"DeluxGSM reused this ad: it now advertises an M2 Pro 16GB Silver at 9 000 lei.",
	}
	for _, s := range plain {
		if ranksItsListing(s) {
			t.Errorf("ranksItsListing(%q) = true, want false", s)
		}
	}
}

// A sweep is compared against the one before it, so its baseline has to be a
// different day. Running twice in a day replaces that day's sweep rather than
// adding one (site.AppendSweep), so reading the baseline off Summary.Checked
// made the page compare today against itself — "0 added since 24 Aug 2026",
// printed on 24 August.
func TestPreviousSweepDate(t *testing.T) {
	d := &site.Dataset{Sweeps: []site.Sweep{
		{Date: "22 Aug 2026", ISO: "2026-08-22"},
		{Date: "24 Aug 2026", ISO: "2026-08-24"},
	}}
	if got := previousSweepDate(d, "2026-08-24"); got != "22 Aug 2026" {
		t.Errorf("re-run on a swept day = %q, want the day before it", got)
	}
	if got := previousSweepDate(d, "2026-08-25"); got != "24 Aug 2026" {
		t.Errorf("a fresh day = %q, want the last sweep", got)
	}
	// Nothing to compare against on the very first sweep, and the caller
	// substitutes today rather than printing an empty date.
	if got := previousSweepDate(&site.Dataset{}, "2026-08-24"); got != "" {
		t.Errorf("first ever sweep = %q, want empty", got)
	}
}

// "New" has to mean "first seen since the previous sweep", not "first seen
// today". They are the same on a normal run and differ the moment a day is
// swept twice: the re-check pass flattens everything it confirms to "live", so
// a listing found in the morning and confirmed in the evening lost its badge
// and the header reported that nothing arrived on a day something had.
func TestMarkFresh(t *testing.T) {
	d := &site.Dataset{Main: []site.Listing{
		{OID: "arrived-since", FirstSeen: "2026-08-24", Status: "live"},
		{OID: "arrived-earlier", FirstSeen: "2026-08-20", Status: "new"},
		{OID: "on-the-baseline-day", FirstSeen: "2026-08-22", Status: "new"},
		{OID: "gone-stays-gone", FirstSeen: "2026-08-24", Status: "gone"},
	}}
	markFresh(d, "2026-08-22")

	want := map[string][2]string{
		"arrived-since":       {"new", "available,new"},
		"arrived-earlier":     {"live", "available"},
		"on-the-baseline-day": {"live", "available"},
		"gone-stays-gone":     {"gone", ""},
	}
	for _, l := range d.Main {
		w := want[l.OID]
		if l.Status != w[0] {
			t.Errorf("%s status = %q, want %q", l.OID, l.Status, w[0])
		}
		if w[1] != "" && l.FacetStatus != w[1] {
			t.Errorf("%s facet = %q, want %q", l.OID, l.FacetStatus, w[1])
		}
	}
}
