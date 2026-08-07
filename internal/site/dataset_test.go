package site

import "testing"

func TestNormalizeCities(t *testing.T) {
	d := &Dataset{
		Main: []Listing{
			{OID: "a", City: "Popești-Leordeni"}, // added by hand, proper spelling
			{OID: "b", City: "Popesti-Leordeni"}, // straight from the OLX API
			{OID: "c", City: "Constanta"},        // only ever seen unaccented
		},
		Sweeps: []Sweep{{Offer: []Snapshot{{OID: "b", City: "Popesti-Leordeni"}}}},
	}
	d.NormalizeCities()

	for _, l := range d.Main[:2] {
		if l.City != "Popești-Leordeni" {
			t.Errorf("listing %s city = %q, want the accented spelling to win", l.OID, l.City)
		}
	}
	if got := d.Sweeps[0].Offer[0].City; got != "Popești-Leordeni" {
		t.Errorf("sweep snapshot city = %q, want it normalised too", got)
	}
	if d.Main[2].City != "Constanta" {
		t.Errorf("city = %q, want an unaccented-only name left alone", d.Main[2].City)
	}

	d.Recompute("7 Aug 2026", "6 Aug 2026", 5.25, "ECB")
	if d.Summary.Cities != 2 {
		t.Errorf("cities = %d, want 2 — one town must not produce two filter chips",
			d.Summary.Cities)
	}
}
