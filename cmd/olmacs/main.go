// Command olmacs refreshes the listing data behind the page.
//
//	go run ./cmd/olmacs sweep            # re-check, discover, write data/listings.js
//	go run ./cmd/olmacs sweep -dry-run   # same, but print the diff and write nothing
//
// A sweep does two passes. First it asks OLX about every listing already on the
// page — a 410 means gone. Then it searches for machines it has not seen. It
// never deletes anything: a listing that goes is kept and struck through, so the
// price history stays readable.
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/andreiverdes/olmacs/internal/mac"
	"github.com/andreiverdes/olmacs/internal/olx"
	"github.com/andreiverdes/olmacs/internal/site"
)

// The memory floor per machine type. Minis top out at 24 GB, so holding them to
// the laptop threshold would exclude the whole category.
const (
	laptopFloor = 36
	miniFloor   = 24
)

// Search angles. One query never surfaces everything: OLX matches all terms, so
// each of these reaches a different slice of the same market.
var queries = []string{
	"macbook 36gb", "macbook pro 48gb", "macbook pro 64gb", "macbook pro 128gb",
	"macbook m4 max", "macbook m5 max", "macbook pro m5 pro", "macbook pro m3 max",
	"macbook pro m4 pro 48gb", "mac studio", "mac studio m4", "mac mini m4 pro",
	"macbook pro 36gb ram", "macbook pro m4 max",
}

var reOID = regexp.MustCompile(`ID([A-Za-z0-9]+)\.html`)

func main() {
	fs := flag.NewFlagSet("sweep", flag.ExitOnError)
	dataPath := fs.String("data", "data/listings.js", "path to the generated data file")
	notesPath := fs.String("notes", "data/notes.json", "path to curated per-listing notes")
	dryRun := fs.Bool("dry-run", false, "report changes without writing")
	limit := fs.Int("limit", 40, "results to pull per search query")

	if len(os.Args) < 2 || os.Args[1] != "sweep" {
		fmt.Fprintln(os.Stderr, "usage: olmacs sweep [-data path] [-dry-run] [-limit n]")
		os.Exit(2)
	}
	_ = fs.Parse(os.Args[2:])

	if err := sweep(*dataPath, *notesPath, *limit, *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func sweep(dataPath, notesPath string, limit int, dryRun bool) error {
	d, err := site.Load(dataPath)
	if err != nil {
		return err
	}
	notes, err := site.LoadNotes(notesPath)
	if err != nil {
		return err
	}
	c := olx.New()
	// fetched up front: euro-denominated listings need it the moment they are read
	rate, rateSource := fetchEURRON(d.Summary.EURRON, d.Summary.EURRONSource)
	now := time.Now()
	iso, display := now.Format("2006-01-02"), formatDate(now)
	prev := d.Summary.Checked
	if prev == "" {
		prev = display
	}

	known := map[string]*site.Listing{}
	for i := range d.Main {
		known[d.Main[i].OID] = &d.Main[i]
	}
	for i := range d.Minis {
		known[d.Minis[i].OID] = &d.Minis[i]
	}

	var wentAway, repriced, added, skipped []string

	// pass 1 — is what we already show still for sale?
	fmt.Fprintf(os.Stderr, "re-checking %d known listings…\n", len(known))
	for oid, l := range known {
		if l.Status == "gone" {
			continue
		}
		if l.ID == 0 { // legacy row with no numeric id; nothing to ask about
			continue
		}
		alive, offer, err := c.Alive(l.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v (left as-is)\n", oid, err)
			continue
		}
		if !alive {
			l.Status, l.FacetStatus, l.GoneReason = "gone", "gone", "sold"
			wentAway = append(wentAway, fmt.Sprintf("%s  %s", oid, trim(l.Title, 52)))
			continue
		}
		// Still up, but is it still the same machine? Sellers reuse ad slots.
		if m, err := mac.Classify(offer.Title, offer.Description); err == nil &&
			!qualifies(m) {
			l.Status, l.FacetStatus, l.GoneReason = "gone", "gone", "reused"
			l.Note = fmt.Sprintf("The seller reused this ad: it now advertises %s %d GB. "+
				"The machine listed here is no longer on sale.", m.Chip, m.RAM)
			wentAway = append(wentAway, fmt.Sprintf("%s  reused → %s", oid, trim(offer.Title, 40)))
			continue
		}
		l.Status, l.FacetStatus = "live", "available"
		// Compare the price the seller set, not the lei conversion: a euro-priced
		// ad shifts by a few lei whenever the reference rate moves, and reporting
		// that as the seller changing their mind would be wrong.
		beforePrice, beforeRON := l.Price, l.RON
		if applyOffer(l, *offer, rate) && l.Price != beforePrice {
			l.PriceWas = beforePrice
			repriced = append(repriced, fmt.Sprintf("%s  %.0f → %.0f %s (%d → %d lei)",
				oid, beforePrice, l.Price, l.Currency, beforeRON, l.RON))
		}
	}

	// pass 2 — anything new?
	seen := map[string]bool{}
	for _, q := range queries {
		offers, err := c.Search(q, limit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! search %q: %v\n", q, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-26s %3d results\n", q, len(offers))
		for _, o := range offers {
			oid := oidOf(o.URL)
			if oid == "" || seen[oid] {
				continue
			}
			seen[oid] = true
			if _, ok := known[oid]; ok {
				continue
			}
			m, err := mac.Classify(o.Title, o.Description)
			if err != nil {
				if _, isReject := err.(mac.Reject); !isReject {
					// a Mac whose memory the seller never wrote down
					if gb, ev, ok := mac.InferFromBucket(
						o.SelectParam("capacitate_memorie_ram"), m.Kind); ok {
						m.RAM, m.RAMEvidence, m.RAMStated = gb, ev, false
					} else {
						skipped = append(skipped, fmt.Sprintf("%s  %s (%v)", oid, trim(o.Title, 44), err))
						continue
					}
				} else {
					continue
				}
			}
			if !qualifies(m) {
				continue
			}
			l := site.Listing{
				OID: oid, ID: o.ID, Title: o.Title, URL: o.URL,
				Kind: string(m.Kind), Chip: m.Chip, Gen: m.Gen,
				RAM: m.RAM, RAMStated: m.RAMStated, RAMEvidence: m.RAMEvidence,
				City: o.Location.City.Name, Region: o.Location.Region.Name,
				Business: o.Business, Created: dateOnly(o.CreatedTime),
				Refreshed: dateOnly(o.LastRefreshTime), Desc: o.Description,
				Status: "new", FacetStatus: "available,new",
				BelowThreshold: m.RAM < laptopFloor, FirstSeen: iso,
			}
			applyOffer(&l, o, rate)
			if l.RON == 0 {
				skipped = append(skipped, fmt.Sprintf("%s  %s (no price)", oid, trim(o.Title, 44)))
				continue
			}
			if m.Kind == mac.KindMini {
				d.Minis = append(d.Minis, l)
			} else {
				d.Main = append(d.Main, l)
			}
			added = append(added, fmt.Sprintf("%s  %s %d GB  %d lei  %s",
				oid, m.Chip, m.RAM, l.RON, trim(o.Title, 38)))
		}
	}

	// last sweep's arrivals are no longer new
	demoteStale(d, iso)
	d.NormalizeCities()
	flagUnderpriced(d)
	applyNotes(d, notes) // a curated note always wins over a generated one

	d.Recompute(display, prev, rate, rateSource)
	d.AppendSweep(display, iso)
	d.Recompute(display, prev, rate, rateSource) // sold_since needs the sweep in place

	report(d, wentAway, repriced, added, skipped, rate, rateSource)
	if dryRun {
		fmt.Fprintln(os.Stderr, "\ndry run — nothing written")
		return nil
	}
	if err := d.Save(dataPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nwrote %s\n", dataPath)
	return nil
}

func qualifies(m mac.Machine) bool {
	if m.Gen == "" || m.RAM == 0 {
		return false
	}
	if m.Kind == mac.KindMini {
		return m.RAM >= miniFloor
	}
	return m.RAM >= laptopFloor
}

// applyOffer copies the live price onto a listing. It reports whether a price
// was found at all; listings without one are not tracked.
//
// Euro-denominated ads keep their euro price and carry a lei figure converted at
// the day's reference rate, so their lei number moves with the rate even when the
// seller has not touched the ad.
func applyOffer(l *site.Listing, o olx.Offer, rate float64) bool {
	value, currency, label, _, ok := o.Price()
	if !ok {
		return false
	}
	l.Price, l.Currency, l.PriceLabel = value, currency, label
	l.Refreshed = dateOnly(o.LastRefreshTime)
	if currency == "EUR" {
		l.RON = int(math.Round(value * rate))
	} else {
		l.RON = int(math.Round(value))
	}
	return true
}

func demoteStale(d *site.Dataset, iso string) {
	for _, set := range [][]site.Listing{d.Main, d.Minis} {
		for i := range set {
			if set[i].Status == "new" && set[i].FirstSeen != "" && set[i].FirstSeen != iso {
				set[i].Status, set[i].FacetStatus = "live", "available"
			}
		}
	}
}

// flagUnderpriced warns on listings priced far under the going rate for their
// exact configuration. On OLX that pattern is far more often a scam or a bait
// price than a bargain, and the page should say so rather than quietly rank it
// first. Groups of fewer than three are skipped — with two listings a "median"
// says nothing.
func flagUnderpriced(d *site.Dataset) {
	const suspiciousBelow = 0.5

	byConfig := map[string][]int{}
	for _, l := range d.All() {
		if l.Status != "gone" {
			key := fmt.Sprintf("%s/%d", l.Kind, l.RAM)
			byConfig[key] = append(byConfig[key], l.RON)
		}
	}
	median := map[string]int{}
	for k, v := range byConfig {
		if len(v) < 3 {
			continue
		}
		sort.Ints(v)
		median[k] = v[len(v)/2]
	}

	for _, set := range [][]site.Listing{d.Main, d.Minis} {
		for i := range set {
			l := &set[i]
			med, ok := median[fmt.Sprintf("%s/%d", l.Kind, l.RAM)]
			if !ok || l.Status == "gone" || float64(l.RON) >= float64(med)*suspiciousBelow {
				continue
			}
			l.Note = fmt.Sprintf("Priced at %d lei against a median of %d lei for a %d GB %s. "+
				"That gap is usually a scam or a bait price on OLX, not a bargain — "+
				"check it carefully before travelling or paying anything up front.",
				l.RON, med, l.RAM, l.Kind)
		}
	}
}

func applyNotes(d *site.Dataset, notes map[string]string) {
	for _, set := range [][]site.Listing{d.Main, d.Minis} {
		for i := range set {
			if n, ok := notes[set[i].OID]; ok {
				set[i].Note = n
			}
		}
	}
}

func report(d *site.Dataset, gone, repriced, added, skipped []string, rate float64, source string) {
	block := func(title string, xs []string) {
		if len(xs) == 0 {
			return
		}
		sort.Strings(xs)
		fmt.Fprintf(os.Stderr, "\n%s (%d)\n", title, len(xs))
		for _, x := range xs {
			fmt.Fprintln(os.Stderr, "  "+x)
		}
	}
	block("GONE since last sweep", gone)
	block("NEW", added)
	block("PRICE CHANGED", repriced)
	block("SKIPPED — a Mac, but could not read the memory", skipped)
	fmt.Fprintf(os.Stderr, "\n%d listings · %d on offer · %d gone · EUR %.4f (%s)\n",
		d.Summary.Total, d.Summary.Available, d.Summary.Sold, rate, source)
}

func oidOf(u string) string {
	if m := reOID.FindStringSubmatch(u); m != nil {
		return "ID" + m[1]
	}
	return ""
}

func dateOnly(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ""
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func formatDate(t time.Time) string {
	return fmt.Sprintf("%d %s %d", t.Day(), t.Format("Jan"), t.Year())
}

// fetchEURRON resolves the euro rate used to price euro-denominated ads in lei.
//
// BNR is the rate Romanians quote, so it is tried first, but bnr.ro refuses
// connections from some networks. The ECB daily reference file publishes RON too
// and is reliably reachable, so it stands in. Whichever answered is recorded and
// named on the page — the two differ in the fourth decimal and the page should
// not claim a number it did not use. If both fail the stored rate is kept, so a
// network blip never silently rewrites every euro price.
func fetchEURRON(fallback float64, fallbackSource string) (float64, string) {
	if v, err := bnrRate(); err == nil {
		return v, "BNR"
	} else {
		fmt.Fprintf(os.Stderr, "! BNR unavailable (%v), trying ECB\n", err)
	}
	if v, err := ecbRate(); err == nil {
		return v, "ECB"
	} else {
		fmt.Fprintf(os.Stderr, "! ECB unavailable (%v), keeping %.4f\n", err, fallback)
	}
	if fallbackSource == "" {
		fallbackSource = "BNR"
	}
	return fallback, fallbackSource
}

func fetchXML(url string, out any) error {
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return xml.Unmarshal(body, out)
}

func bnrRate() (float64, error) {
	var doc struct {
		Body struct {
			Cube struct {
				Rate []struct {
					Currency string  `xml:"currency,attr"`
					Value    float64 `xml:",chardata"`
				} `xml:"Rate"`
			} `xml:"Cube"`
		} `xml:"Body"`
	}
	if err := fetchXML("https://www.bnr.ro/nbrfxrates.xml", &doc); err != nil {
		return 0, err
	}
	for _, r := range doc.Body.Cube.Rate {
		if r.Currency == "EUR" && r.Value > 0 {
			return r.Value, nil
		}
	}
	return 0, fmt.Errorf("no EUR rate in feed")
}

func ecbRate() (float64, error) {
	// Envelope > Cube > Cube(time) > Cube(currency, rate) — three levels, not four.
	var doc struct {
		Cube struct {
			Day struct {
				Rate []struct {
					Currency string  `xml:"currency,attr"`
					Rate     float64 `xml:"rate,attr"`
				} `xml:"Cube"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	}
	if err := fetchXML("https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml", &doc); err != nil {
		return 0, err
	}
	for _, r := range doc.Cube.Day.Rate {
		if r.Currency == "RON" && r.Rate > 0 {
			return r.Rate, nil
		}
	}
	return 0, fmt.Errorf("no RON rate in feed")
}
