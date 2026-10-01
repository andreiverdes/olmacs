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

// Contradiction reports that the ad names two different machines — the title
// one, the body another — so which is for sale cannot be read off the ad. It is
// not a Reject: this is a Mac, and probably a qualifying one. It is reported
// rather than resolved, because resolving it means picking a side.
type Contradiction struct{ Reason string }

func (c Contradiction) Error() string { return c.Reason }

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

	// The title and the body have to agree on the generation. IDkSiRI was titled
	// "Macbook Pro M5 Max 16’ 36GB 1TB" over a body opening "Vând MacBook Pro M4
	// Max 16”, 36GB RAM, 1TB stocare" — one machine, named as two. The title wins
	// below, so the page would have carried a 15 000 lei M5 Max and counted it in
	// the M5 column of every chart.
	//
	// Only a body naming exactly one generation counts. Ads that compare models
	// ("mai rapid decat M4 Max") name several and settle nothing, and most name
	// none at all.
	if body := gensIn(strings.ToLower(stripHTML(desc))); len(body) == 1 && !body[m.Gen] {
		return m, Contradiction{fmt.Sprintf(
			"the title says %s but the description says %s — the ad names two "+
				"different machines", m.Gen, only(body))}
	}

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
//
// The bucket is a range, and only the open-ended one can hold a machine this
// project tracks: OLX's other values are closed ranges topping out at 16 GB, so
// a listing carrying one states a machine below the floor rather than an
// unstated large one. Matching on "16" alone read "12 - 16 GB" as "> 16 GB" and
// would have published a 16 GB mini as a 24 GB machine.
//
// A boundary this does not recognise returns false, which sends the listing to
// the skipped report — visible, rather than guessed at.
func InferFromBucket(bucketLabel string, kind Kind) (gb int, evidence string, ok bool) {
	if kind != KindMini {
		return 0, "", false
	}
	if bucket(bucketLabel) != ">16gb" {
		return 0, "", false
	}
	return 24, fmt.Sprintf("RAM field reads %q — the seller never states the exact size", bucketLabel), true
}

// bucket normalises an OLX select label so spacing is not treated as meaning.
func bucket(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), ""))
}

// subject is the opening of a title, where Romanian ads put what they are selling.
func subject(t string) string {
	if f := strings.Fields(t); len(f) > 4 {
		return strings.Join(f[:4], " ")
	}
	return t
}

func distinctGens(s string) int { return len(gensIn(s)) }

// gensIn is the set of Apple silicon generations named in s, keyed "M3", "M4".
func gensIn(s string) map[string]bool {
	seen := map[string]bool{}
	for _, mm := range reAnyGen.FindAllStringSubmatch(s, -1) {
		seen["M"+mm[1]] = true
	}
	return seen
}

func only(set map[string]bool) string {
	for k := range set {
		return k
	}
	return ""
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
//
// Failing that, a memory size written as the first half of "memory/storage"
// counts: IDkXDQ7 is "MacBook Pro 16 M5 Pro 48/1tb sigilat", and the 48 carries
// no unit at all. The second half has to be a storage size — terabytes with
// their unit, or 256/512 — which is what keeps core counts ("18/40", "14/20") out.
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
	for _, loc := range reRAMSlash.FindAllStringSubmatchIndex(s, -1) {
		n, err := strconv.Atoi(s[loc[2]:loc[3]])
		if err != nil || !ramSizes[n] {
			continue
		}
		lo := max(0, loc[0]-28)
		return n, strings.TrimSpace(collapse(s[lo:min(len(s), loc[1]+24)])), true
	}
	return 0, "", false
}

// Storage reads the SSD size, in GB, out of a listing. 0 means the seller never
// wrote it down in a form that can be told apart from memory. The title is
// read first, because a description is where a seller mentions the external
// drive they are throwing in.
//
// Terabytes are unambiguous. 256 and 512 GB are also Studio Ultra memory sizes,
// so they only count when nothing names them memory: storage words around
// them, the second half of "36/512", or a GB unit with no memory word beside it.
func Storage(title, desc string) int {
	if gb := findStorage(title); gb != 0 {
		return gb
	}
	return findStorage(stripHTML(desc))
}

func findStorage(s string) int {
	best, at := 0, len(s)
	if loc := reTB.FindStringSubmatchIndex(s); loc != nil {
		n, _ := strconv.Atoi(s[loc[2]:loc[3]])
		best, at = n*1024, loc[0]
	}
	for _, loc := range reGBStorage.FindAllStringSubmatchIndex(s, -1) {
		if loc[0] >= at {
			break
		}
		n, _ := strconv.Atoi(s[loc[2]:loc[3]])
		if n%1000 == 0 { // "1000 SSD" — sellers round the terabyte
			n = n / 1000 * 1024
		}
		before := s[max(0, loc[0]-16):loc[0]]
		after := s[loc[1]:min(len(s), loc[1]+16)]
		// Storage words beat a memory word before the size: "36GB RAM 512GB SSD"
		// has RAM right in front of the 512, and the 512 is still the SSD.
		switch {
		case reMemoryAfter.MatchString(after):
			continue
		case loc[6] >= 0, reStorageAfter.MatchString(after), reStorageBefore.MatchString(before),
			reSlashBefore.MatchString(before):
			return n
		case reMemoryBefore.MatchString(before) && !reMemoryClosed.MatchString(before):
			continue
		case loc[4] >= 0: // a GB unit, and nothing calls it memory
			return n
		}
	}
	return best
}

var (
	// "48/1tb", "36 / 512GB" — memory first, storage second, the memory unitless.
	reRAMSlash = regexp.MustCompile(`(?i)\b(\d{1,3})\s*/\s*(?:(?:1|2|4|8)\s*tb?|(?:256|512)(?:\s*gb?)?)\b`)
	// "1TB", "2 Tb", "1 Terra", "4T". The unit is what makes a bare digit storage.
	reTB = regexp.MustCompile(`(?i)\b(1|2|4|8|16)\s*(?:terra|tera|tb|t)\b`)
	// The unit is optional here: "512 SSD", "512SSD" and "36/512" carry none.
	reGBStorage     = regexp.MustCompile(`(?i)\b(256|512|1000|2000)(?:\s*(gb|g\b|giga)|(ssd)|\b)`)
	reStorageBefore = regexp.MustCompile(`(?i)(ssd|stocare|storage|spatiu|spațiu|disk|hdd)\W*$`)
	// Only a memory word directly in front — "RAM 512GB", "memorie: 256 GB" —
	// and not one that closes the figure before it: "36gb Ram 512gb".
	reMemoryBefore = regexp.MustCompile(`(?i)\b(ram|memorie|memory|unified)\s*:?\s*$`)
	reMemoryClosed = regexp.MustCompile(`(?i)\d\s*(gb|g)?\s*(ram|memorie|memory|unified)\s*:?\s*$`)
	reMemoryAfter  = regexp.MustCompile(`(?i)^\W*(ram|memorie|memory|unified|unificat|unificată)`)
	reSlashBefore  = regexp.MustCompile(`/\s*$`)
)

var (
	reTag = regexp.MustCompile(`<[^>]*>`)
	reWS  = regexp.MustCompile(`\s+`)
)

func stripHTML(s string) string {
	s = strings.NewReplacer("<br />", " ", "<br>", " ", "&nbsp;", " ").Replace(s)
	return reWS.ReplaceAllString(reTag.ReplaceAllString(s, " "), " ")
}

func collapse(s string) string { return reWS.ReplaceAllString(s, " ") }
