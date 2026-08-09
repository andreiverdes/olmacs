// Package mac turns a free-text OLX listing into the machine it is advertising.
//
// OLX exposes memory only as a coarse bucket ("> 16 GB"), so the exact figure has
// to come out of the seller's own words. Everything here is therefore best-effort
// and says so: Classify reports how it reached a number, and refuses to guess
// where guessing would be dishonest.
package mac

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Kind string

const (
	KindMacBook Kind = "MacBook"
	KindMini    Kind = "Mac mini"
	KindStudio  Kind = "Mac Studio"
)

// Machine is what a listing was determined to be advertising.
type Machine struct {
	Kind Kind
	Gen  string // "M3", "M4", "M5"
	Chip string // "M3 Pro", "M4 Max", "M5", …
	RAM  int    // GB of unified memory

	// RAMStated is false when the size was inferred rather than written down.
	// The page renders those with a "config unstated" badge.
	RAMStated bool
	// RAMEvidence is the text the number was read out of, for the card.
	RAMEvidence string
}

// Reject explains why a listing is not a machine this project tracks.
type Reject struct{ Reason string }

func (r Reject) Error() string { return r.Reason }

// Sizes Apple actually ships as unified memory. A number outside this set is
// storage or noise, whatever the surrounding words claim.
//
// 192/256/512 are Mac Studio Ultra configurations. They are also ordinary SSD
// capacities, so they are deliberately absent from ramOnlySizes below and only
// count when memory words sit beside them.
var ramSizes = map[int]bool{8: true, 16: true, 18: true, 24: true, 32: true,
	36: true, 48: true, 64: true, 96: true, 128: true, 192: true, 256: true,
	512: true}

// Sizes Apple has never sold as an SSD, so a bare "36 GB" cannot be storage.
var ramOnlySizes = map[int]bool{18: true, 24: true, 36: true, 48: true, 96: true}

var (
	reGB = regexp.MustCompile(`(?i)(\d{1,3})\s*(?:gb|g\b|giga)`)
	// One pattern for both generation and variant. The optional \s* is what makes
	// "M4PRO" parse, which sellers write about as often as "M4 Pro".
	reChip = regexp.MustCompile(`(?i)\bm([345])\s*(pro\s*max|max|pro|ultra)?\b`)
	// Catch-all detection has to see the whole Apple-silicon range, not just the
	// generations this project tracks: "M1 / M2 / M3 / M4" is one shop ad, not a machine.
	reAnyGen = regexp.MustCompile(`(?i)\bm([1-5])\b`)

	// Accessory nouns, tested against the SUBJECT of the title only. Every case
	// ad names the machine it fits ("Carcasa pentru Mac Mini M4"), so matching
	// anywhere would reject the machines too — and a real listing that happens to
	// mention "incarcator original" at the end is still a machine.
	reAccessorySubject = regexp.MustCompile(`(?i)\b(ansamblu|display|ecran|cutie|husa|husă|` +
		`carcasa|carcasă|incarcator|încărcător|cablu|dock|suport|stand|adaptor|priza|priză|` +
		`stylus|pencil|tastatura|tastatură|folie|protectie|protecție|piese|baterie|` +
		`placa|placă|geanta|geantă|rucsac|memorie ram)\b`)
	// These are never the subject of a machine listing, wherever they appear.
	reNeverAMachine = regexp.MustCompile(`(?i)\b(licenta|licență|deblocare|decodare|` +
		`resoftare|reparatii|reparații|dezmembr)\b`)
	reWanted = regexp.MustCompile(`(?i)\b(caut|cumpar|cumpăr|achizitionez|achiziționez|` +
		`schimb cu)\b`)
	reIntel = regexp.MustCompile(`(?i)\b(intel|core i[3579]|retina 201[0-9]|a1[0-9]{3})\b`)

	// Storage words that, right after a size, mark it as not memory.
	reStorageAfter = regexp.MustCompile(`(?i)^\s*(ssd|hdd|stocare|storage|memorie interna|nvme|disk)`)
	reRAMNear      = regexp.MustCompile(`(?i)(ram|memorie|memory|unified|unificat|unificată)`)
)

// Classify reads a listing. It returns a Reject error for anything that is not an
// M3/M4/M5 Mac, and a plain error when the machine is one but its memory could
// not be established.
func Classify(title, desc string) (Machine, error) {
	var m Machine
	t := strings.ToLower(title)

	if reWanted.MatchString(t) {
		return m, Reject{"wanted ad or trade, not a sale"}
	}
	if reNeverAMachine.MatchString(t) {
		return m, Reject{"service or licence, not hardware"}
	}
	// Romanian ads name their subject first, so an accessory noun in the opening
	// words is decisive even when the rest of the title names a Mac.
	if reAccessorySubject.MatchString(subject(t)) {
		return m, Reject{"accessory or part, not a machine"}
	}

	switch {
	case strings.Contains(t, "mac studio"):
		m.Kind = KindStudio
	case strings.Contains(t, "mac mini"), strings.Contains(t, "macmini"),
		strings.Contains(t, "mini mac"), strings.Contains(t, "mini-mac"):
		m.Kind = KindMini
	case strings.Contains(t, "macbook"), strings.Contains(t, "mac book"):
		m.Kind = KindMacBook
	default:
		return m, Reject{"not a MacBook, Mac mini or Mac Studio"}
	}

	// A shop ad covering the whole back catalogue ("M1 / M2 / M3 / M4 / M5") is
	// not one machine and has no single price.
	if distinctGens(t) >= 3 {
		return m, Reject{"catch-all shop ad spanning several generations"}
	}
	gen := reChip.FindStringSubmatch(t)
	if gen == nil {
		if reIntel.MatchString(t) {
			return m, Reject{"Intel-era machine"}
		}
		return m, Reject{"no M3, M4 or M5 chip named in the title"}
	}
	m.Gen = "M" + gen[1]
	// The variant is often only spelled out in the body ("procesor M4 PRO"),
	// so fall back to it when the title just says "M4".
	m.Chip = variant(t, m.Gen)
	if m.Chip == m.Gen {
		m.Chip = variant(strings.ToLower(stripHTML(desc)), m.Gen)
	}

	// Memory: the seller's words first, the OLX bucket only as a floor.
	if gb, ev, ok := findRAM(title); ok {
		m.RAM, m.RAMEvidence, m.RAMStated = gb, ev, true
		return m, nil
	}
	if gb, ev, ok := findRAM(stripHTML(desc)); ok {
		m.RAM, m.RAMEvidence, m.RAMStated = gb, ev, true
		return m, nil
	}
	return m, fmt.Errorf("memory not stated in title or description")
}

// InferFromBucket is the deliberate fallback for a listing whose seller never
// wrote the memory down. It only ever returns the smallest size the OLX bucket
// allows, and the caller must surface it as unstated.
func InferFromBucket(bucketLabel string, kind Kind) (gb int, evidence string, ok bool) {
	b := strings.ToLower(bucketLabel)
	switch {
	case strings.Contains(b, "16"): // "> 16 GB" — the smallest Apple config above it
		if kind == KindMini {
			return 24, fmt.Sprintf("RAM field reads %q — the seller never states the exact size", bucketLabel), true
		}
	}
	return 0, "", false
}

// subject is the opening of a title, where Romanian ads put what they are selling.
func subject(t string) string {
	if f := strings.Fields(t); len(f) > 4 {
		return strings.Join(f[:4], " ")
	}
	return t
}

func distinctGens(s string) int {
	seen := map[string]bool{}
	for _, mm := range reAnyGen.FindAllStringSubmatch(s, -1) {
		seen[mm[1]] = true
	}
	return len(seen)
}

func variant(s, gen string) string {
	for _, mm := range reChip.FindAllStringSubmatch(s, -1) {
		if !strings.EqualFold("m"+mm[1], gen) {
			continue
		}
		switch v := strings.ToLower(strings.Join(strings.Fields(mm[2]), " ")); v {
		case "pro", "max", "ultra":
			return gen + " " + strings.ToUpper(v[:1]) + v[1:]
		case "pro max": // sellers write this for what Apple calls Max
			return gen + " Max"
		}
	}
	return gen
}

// findRAM looks for a memory size in s. A size counts when Apple sells it as
// memory and either the words around it say so, or Apple has never sold it as
// storage (so it cannot be anything else).
func findRAM(s string) (gb int, evidence string, ok bool) {
	for _, loc := range reGB.FindAllStringSubmatchIndex(s, -1) {
		n, err := strconv.Atoi(s[loc[2]:loc[3]])
		if err != nil || !ramSizes[n] {
			continue
		}
		after := s[loc[1]:min(len(s), loc[1]+24)]
		if reStorageAfter.MatchString(after) {
			continue
		}
		lo := max(0, loc[0]-28)
		around := s[lo:min(len(s), loc[1]+24)]
		if ramOnlySizes[n] || reRAMNear.MatchString(around) {
			return n, strings.TrimSpace(collapse(around)), true
		}
	}
	return 0, "", false
}

var (
	reTag = regexp.MustCompile(`<[^>]*>`)
	reWS  = regexp.MustCompile(`\s+`)
)

func stripHTML(s string) string {
	s = strings.NewReplacer("<br />", " ", "<br>", " ", "&nbsp;", " ").Replace(s)
	return reWS.ReplaceAllString(reTag.ReplaceAllString(s, " "), " ")
}

func collapse(s string) string { return reWS.ReplaceAllString(s, " ") }
