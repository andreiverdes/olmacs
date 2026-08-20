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
}

// Only OLX's open-ended bucket can hold a machine this project tracks. The
// others are closed ranges topping out at 16 GB, and reading one of those as
// "above 16" published IDjXsTM — "Mac mini M4 16GB/512 SSD", bucket
// "12 - 16 GB" — as a 24 GB machine at 4 000 lei, under every real 24 GB mini
// on the page.
func TestInferFromBucketRejectsClosedRanges(t *testing.T) {
	for _, bucket := range []string{"12 - 16 GB", "8 - 12 GB", "4 - 6 GB", "< 4 GB", ""} {
		if gb, _, ok := InferFromBucket(bucket, KindMini); ok {
			t.Errorf("InferFromBucket(%q) = (%d, true), want false — the bucket "+
				"tops out below the 24 GB floor", bucket, gb)
		}
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
