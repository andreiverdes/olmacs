package mac

import "testing"

// Every case here is a real olx.ro listing seen during a sweep. When the
// classifier gets something wrong in the wild, add the listing here first.
func TestClassify(t *testing.T) {
	cases := []struct {
		name, title, desc string
		wantKind          Kind
		wantGen, wantChip string
		wantRAM           int
	}{
		{
			// "16 pro" is the MacBook Pro, not an M3 Pro — the chip is a plain M3.
			name: "memory in the title", title: "Macbook 16 pro m3, 36 gb ram",
			wantKind: KindMacBook, wantGen: "M3", wantChip: "M3", wantRAM: 36,
		},
		{
			name:     "memory only in the description",
			title:    "MacBook Pro M4 1Tb",
			desc:     "procesor M4 PRO cu 12C CPU | stocare 1 Terra / 1000GB ssd | 36GB RAM !",
			wantKind: KindMacBook, wantGen: "M4", wantChip: "M4 Pro", wantRAM: 36,
		},
		{
			name:     "storage must not be read as memory",
			title:    "Macbook pro m3 16 pro 36 gb ram 512gb ssd",
			wantKind: KindMacBook, wantGen: "M3", wantChip: "M3", wantRAM: 36,
		},
		{
			name: "asterisks instead of spaces", title: "Mac Studio**M4 Max**36 GB**1 TB SSD**",
			wantKind: KindStudio, wantGen: "M4", wantChip: "M4 Max", wantRAM: 36,
		},
		{
			name: "no space before the chip", title: `Macbook Pro 16" 48GB 512SSD M4PRO 2025`,
			wantKind: KindMacBook, wantGen: "M4", wantRAM: 48,
		},
		{
			name: "mini", title: "Mac Mini m4 pro 24GB RAM",
			wantKind: KindMini, wantGen: "M4", wantChip: "M4 Pro", wantRAM: 24,
		},
		{
			// Studio Ultra memory runs past the laptop ceiling. 256 is also a
			// common SSD size, so it only counts with "RAM" beside it.
			name:     "Studio Ultra, memory larger than any laptop",
			title:    "Mac Studio M3 Ultra 32/80 | 256GB RAM | 4TB SSD",
			wantKind: KindStudio, wantGen: "M3", wantChip: "M3 Ultra", wantRAM: 256,
		},
		{
			name:     "seller writes Pro Max for what Apple calls Max",
			title:    `MacBook Pro 16.2"  M4 Pro Max, 36gb/1tb SSD Sigilat`,
			wantKind: KindMacBook, wantGen: "M4", wantChip: "M4 Max", wantRAM: 36,
		},
		{
			name:     "comparison chip named in the title does not win",
			title:    `Apple Macbook 14" M3 Pro CA NOU ! 36GB 18GPU 12CPU 1TB Apple / vs m4 24gb max`,
			wantKind: KindMacBook, wantGen: "M3", wantChip: "M3 Pro", wantRAM: 36,
		},
		{
			// "48" carries no unit; the storage after the slash is what says it is memory.
			name:     "memory written as the first half of memory/storage",
			title:    "MacBook Pro 16 M5 Pro 48/1tb sigilat",
			wantKind: KindMacBook, wantGen: "M5", wantChip: "M5 Pro", wantRAM: 48,
		},
		{
			// M6 Macs reach OLX from late Oct 2026; the chip pattern has to admit them.
			name:     "M6 generation",
			title:    "MacBook Pro 14 M6 Max 48GB 2TB sigilat",
			wantKind: KindMacBook, wantGen: "M6", wantChip: "M6 Max", wantRAM: 48,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Classify(c.title, c.desc)
			if err != nil {
				t.Fatalf("Classify(%q) = error %v, want a machine", c.title, err)
			}
			if m.Kind != c.wantKind {
				t.Errorf("kind = %q, want %q", m.Kind, c.wantKind)
			}
			if m.Gen != c.wantGen {
				t.Errorf("gen = %q, want %q", m.Gen, c.wantGen)
			}
			if c.wantChip != "" && m.Chip != c.wantChip {
				t.Errorf("chip = %q, want %q", m.Chip, c.wantChip)
			}
			if m.RAM != c.wantRAM {
				t.Errorf("ram = %d, want %d (evidence %q)", m.RAM, c.wantRAM, m.RAMEvidence)
			}
			if !m.RAMStated {
				t.Errorf("ram_stated = false, want true — memory is written in this listing")
			}
		})
	}
}

// A memory word vouches only for the size it sits beside. Both listings put an
// SSD size within reach of a memory word that names a different number, and
// were read as 256 and 512 GB machines. Neither states its memory in a form the
// classifier can use, so the honest answer is "not stated" — the skipped
// report — never the SSD size.
func TestMemoryWordBelongsToTheNearerNumber(t *testing.T) {
	cases := []struct{ oid, title, desc string }{
		// "RAM" belongs to the unitless 18, not the 512 in front of it.
		{"IDkZrdu", "MacBook M3Pro 16inch 512GB 18 RAM", ""},
		// "unificată" names the 16, which the SSD after it disqualifies; it
		// must not carry on to the 256.
		{"IDkYEa7", "Macbook 13 inch M4",
			"Cip M4, memorie unificată de 16 GB SSD de 256 GB 1 GB = 1 miliard de octeți"},
	}
	for _, c := range cases {
		m, err := Classify(c.title, c.desc)
		if err == nil {
			t.Errorf("%s: read %d GB from %q, want memory not stated", c.oid, m.RAM, m.RAMEvidence)
			continue
		}
		if _, ok := err.(Reject); ok {
			t.Errorf("%s: rejected as not a Mac: %v", c.oid, err)
		}
	}
}

// Accessory ads always name the machine they fit, so naming a Mac cannot be
// what saves a listing from rejection.
func TestClassifyRejects(t *testing.T) {
	cases := []struct{ name, title, desc string }{
		{"display assembly", "Ansamblu Display original MacBook Pro M4, M4 Pro", ""},
		{"empty box", "Cutie MacBook M4 Pro Max 16 Inch", ""},
		{"case", "Carcasa protectie pentru Apple Mac Mini M4 si M4 Pro 2024", ""},
		{"dock", "Dock pentru Mac Mini M4, suport Mac Mini din aluminiu 10GB", ""},
		{"charger", "Incarcator Apple MacBook Pro M4 96W", ""},
		{"catch-all shop ad", `MacBook Air 13" / 15" / M1 / M2 / M3 / M4 / Garantie 2 ani`, ""},
		{"wanted ad", "Caut MacBook Pro M4 Max 48GB", ""},
		{"Intel era", "MacBook Pro 16 2019 i9 64GB RAM 1TB SSD", ""},
		{"not a Mac", "Lenovo ThinkPad P16 gen 2 i9 32GB DDR5", ""},
		{"iPad", "iPad Pro 12.9 M4 256GB", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Classify(c.title, c.desc)
			if err == nil {
				t.Fatalf("Classify(%q) = %+v, want rejection", c.title, m)
			}
			if _, ok := err.(Reject); !ok {
				t.Fatalf("Classify(%q) = %v (%T), want a Reject", c.title, err, err)
			}
		})
	}
}

// A Mac whose seller never wrote the memory down is not rejected — it is
// reported as a machine with an unreadable configuration, so the caller can
// decide whether to fall back to the OLX bucket.
func TestClassifyMemoryUnstated(t *testing.T) {
	m, err := Classify("Mini mac M4 pro (Apple)", "Este aproape nou, l-am folosit doar la search")
	if err == nil {
		t.Fatalf("want an error for a listing with no memory stated, got %+v", m)
	}
	if _, isReject := err.(Reject); isReject {
		t.Fatalf("want a plain error, not a Reject: %v", err)
	}
	if m.Kind != KindMini || m.Gen != "M4" {
		t.Fatalf("kind/gen should still be known: %+v", m)
	}
	gb, ev, ok := InferFromBucket("> 16 GB", m.Kind)
	if !ok || gb != 24 {
		t.Fatalf("InferFromBucket = (%d, %q, %v), want (24, …, true)", gb, ev, ok)
	}
}

// "32/80" is CPU/GPU cores. A slash only marks memory when a storage size with
// its unit follows, and 18, 24 and 32 are all memory sizes as well as core counts.
func TestCoreCountsAreNotMemory(t *testing.T) {
	if m, err := Classify("Mac Studio M3 Ultra 32/80 | 4TB SSD", ""); err == nil {
		t.Errorf("Classify = %d GB from %q, want memory unstated", m.RAM, m.RAMEvidence)
	}
}

// The titles are real olx.ro listings. 256 and 512 are memory on a Studio Ultra
// and storage everywhere else, so the words around them decide.
func TestStorage(t *testing.T) {
	cases := []struct {
		title, desc string
		want        int
	}{
		{"Macbook Pro M4 Max 16’ 36GB 1TB", "", 1024},
		{"MacBook Pro 16 M5 Max 48/2tb Sigilat", "", 2048},
		{"Macbook pro m3 16 pro 36 gb ram 512gb ssd", "", 512},
		{`Macbook Pro 16" 48GB 512SSD M4PRO 2025`, "", 512},
		{"MacBook Pro 16 M4 Pro 14/20, 48GB RAM, SSD 512GB, Space Black", "", 512},
		{"Macbook Pro M3 Pro 16-inch 2023 36gb/512Gb/BATERIE96", "", 512},
		{"Mac mini M4 16GB/512 SSD", "", 512},
		{"MacBook Pro M4 1Tb", "procesor M4 PRO cu 12C CPU | stocare 1 Terra / 1000GB ssd", 1024},
		{"Mac Studio M3 Ultra 32/80 | 256GB RAM | 4TB SSD", "", 4096},
		{"Mac Studio M3 Ultra 512GB memorie unificata 8TB SSD", "", 8192},
		{"Mac Studio M4 Max 36gb Ram", "Stare impecabila. 36GB RAM, 512GB.", 512},
		{"Mac Mini M4 PRO 24GB", "Pret 5120 lei, M4 Pro 14 core", 0},
		{"MacBook Pro 16 inch M4 Max 48GB RAM", "", 0},
		{"Apple Macbook M3 Pro 16” 18 Core 36gb Ram 512gb", "", 512},
		{"MacBook Pro 16 inch M4 Max **36 GB**1000 SSD**Sigilat", "", 1024},
	}
	for _, c := range cases {
		if got := Storage(c.title, c.desc); got != c.want {
			t.Errorf("Storage(%q, %q) = %d, want %d", c.title, c.desc, got, c.want)
		}
	}
}

// 256 and 512 are Apple memory sizes AND ordinary SSD sizes. They must never be
// read as memory on the strength of the number alone.
func TestLargeSizesNeedMemoryWords(t *testing.T) {
	if _, err := Classify(`Macbook Pro 14" M4 Pro 512GB SSD`, ""); err == nil {
		t.Error("512GB SSD was read as 512 GB of memory")
	}
	if _, err := Classify("Mac Studio M3 Ultra 256GB SSD 4TB", ""); err == nil {
		t.Error("256GB SSD was read as 256 GB of memory")
	}
	m, err := Classify("Mac Studio M3 Ultra 512GB memorie unificata 8TB SSD", "")
	if err != nil || m.RAM != 512 {
		t.Errorf("Classify = (%d, %v), want 512 GB — Romanian memory words count too", m.RAM, err)
	}

	// A Max tops out at 128 GB, so "512 GB Memorie" on one is its SSD — Romanian
	// sellers call storage memorie too. IDkYVaj went on the page as a 512 GB
	// Mac Studio; its body states the real figure.
	m, err = Classify("NOU! Apple Mac Studio M4 Max - 32 GB, 512 GB Memorie",
		"Procesor: Cip Apple M4 Max cu 14 nuclee CPU, 32 nuclee GPU<br />Memorie: 36GB<br />Stocare: 512GB")
	if err != nil || m.RAM != 36 {
		t.Errorf("Classify = (%d, %v), want 36 GB from the body", m.RAM, err)
	}

	// A mini's SSD is never below 256 GB, so a bare 16 is its memory. IDl1sk5
	// was skipped for want of a memory word.
	m, err = Classify("Mac Mini M4/16GB/256GB", "")
	if err != nil || m.RAM != 16 {
		t.Errorf("Classify = (%d, %v), want 16 GB", m.RAM, err)
	}
}

// A bucket reads as the smallest mini it can hold. Reading "12 - 16 GB" as
// "above 16" once published IDjXsTM — "Mac mini M4 16GB/512 SSD" — as a 24 GB
// machine at 4 000 lei, under every real 24 GB mini on the page.
func TestInferFromBucketRejectsClosedRanges(t *testing.T) {
	// Every mini from the M4 on has at least 16 GB, so only the two buckets
	// at or above 16 can hold one.
	for _, bucket := range []string{"8 - 12 GB", "4 - 6 GB", "< 4 GB", ""} {
		if gb, _, ok := InferFromBucket(bucket, KindMini); ok {
			t.Errorf("InferFromBucket(%q) = (%d, true), want false — no M4-or-later "+
				"mini fits that bucket", bucket, gb)
		}
	}
	if gb, _, ok := InferFromBucket("12 - 16 GB", KindMini); !ok || gb != 16 {
		t.Errorf("InferFromBucket(\"12 - 16 GB\") = (%d, %v), want (16, true)", gb, ok)
	}
	if gb, _, ok := InferFromBucket(">16GB", KindMini); !ok || gb != 24 {
		t.Errorf("InferFromBucket(\">16GB\") = (%d, %v), want (24, true) — "+
			"spacing in the label is not meaning", gb, ok)
	}
	if gb, _, ok := InferFromBucket("> 16 GB", KindMacBook); ok {
		t.Errorf("InferFromBucket(…, MacBook) = (%d, true), want false — only "+
			"minis have a config the bucket pins down", gb)
	}
}

// An ad that names two different machines cannot be read as either. IDkSiRI was
// titled "Macbook Pro M5 Max 16’ 36GB 1TB" over a body opening "Vând MacBook Pro
// M4 Max 16”, 36GB RAM, 1TB stocare" — one machine, named as two. The title wins
// in Classify, so the page would have carried a 15 000 lei M5 Max: below the
// other M5 Max 36 GB at 17 499, above the M4 Max 36 GB at 13 500, and counted in
// the M5 column of every chart.
func TestTitleAndBodyMustAgreeOnGeneration(t *testing.T) {
	_, err := Classify("Macbook Pro M5 Max 16’ 36GB 1TB",
		`Vând MacBook Pro M4 Max 16”, 36GB RAM, 1TB stocare.<br /> Este în stare `+
			`foarte bună, folosit în principal pentru editare video.`)
	if _, ok := err.(Contradiction); !ok {
		t.Fatalf("Classify err = %v (%T), want Contradiction", err, err)
	}

	// A body that names several generations is comparing models, and nothing
	// follows from that — this is the shape of half the shop ads on OLX.
	if _, err := Classify("Apple Macbook PRO 16” M5 MAX SiGiLAT ! 36GB 2TB",
		"Configuratie DE TOP: cu procesor M5 MAX 36GB RAM. Mai rapid decat M4 Max si M3 Max."); err != nil {
		t.Errorf("a body comparing models should not be a contradiction: %v", err)
	}
	// A body that names none is the common case and says nothing either way.
	if m, err := Classify("MacBook Pro M4 Max 36GB RAM",
		"Stare impecabila, cutie si factura. Predare in Cluj."); err != nil || m.Chip != "M4 Max" {
		t.Errorf("Classify = (%q, %v), want M4 Max from the title alone", m.Chip, err)
	}
	// Agreement is the normal case and must stay silent.
	if m, err := Classify("MacBook Pro M4 Max 36GB RAM",
		"Vand MacBook Pro M4 Max, 36 GB RAM, 1TB SSD."); err != nil || m.Chip != "M4 Max" {
		t.Errorf("Classify = (%q, %v), want M4 Max", m.Chip, err)
	}
}
