# olmacs

A single-page tracker for **M3, M4 and M5 Macs listed on olx.ro with 36 GB or more of
unified memory**, plus the 24 GB Mac minis as a cheaper fleet alternative.

**Live page:** https://andreiverdes.github.io/olmacs/

Everything — data, styles, charts — lives in one self-contained `index.html`. No build
step, no dependencies, no network calls at runtime. Open the file locally and it works.

## What's on the page

**The listing grid.** Every qualifying listing as a card: memory, chip, type, price,
city, the exact text that proves the memory claim, and a note where something needs
flagging. Filters across type / chip / memory / city / status combine — more than one
chip in a row widens it, picking across rows narrows. A shortlist is saved to
`localStorage` and mirrored into the URL hash, so a copied link keeps the selection.

**Price by memory and by chip.** For each memory tier and chip generation: the low–high
spread of what's on offer, the median asking price at each sweep as a dumbbell, plus
lei/GB. Every plotted value also appears in a table beneath the chart.

**Chip and memory over time.** The mix of machines on offer at each sweep, and a dot
strip placing every ad on the day it was posted — which is the only view with real
temporal depth while there are just two sweeps.

Listings that disappear are struck through rather than deleted, so the price history
stays readable.

## How the data is gathered

Each sweep does two things:

1. **Re-check every listing already on the page.** A listing is marked gone when its
   OLX page returns HTTP 410 *and* it no longer appears in an OLX search that should
   surface it. Both signals, because either alone gives false positives.
2. **Sweep OLX for new listings** across several query angles (`q-macbook-36gb`,
   `q-macbook-pro-48gb`, `q-macbook-m4-max`, `q-mac-studio`, `q-mac-mini-m4-pro`, …),
   then open each candidate to confirm chip, memory, price, city and posting date from
   the ad body rather than the title.

Prices are recorded as listed. Euro-denominated ads keep their euro price; the lei
figure is converted at the BNR reference rate and moves with it.

## Known limits

These are stated on the page too, but collected here:

- **Two sweeps is not a trend.** Almost all movement between sweeps is composition —
  ads leaving and arriving — not sellers repricing. At the 7 Aug sweep exactly one
  listing had changed its own asking price.
- **Small cells.** Some memory tiers are a single listing. Every chart shows `n` on
  both sides of a comparison for that reason.
- **The posting-date strip covers 21 of 27 machines on offer.** Five never exposed a
  date, and one sits in an ad slot opened years before the machine existed (a reused
  ad), so its date is meaningless and it is excluded.
- **Survivorship.** The strip only describes ads that were still up at a sweep.
  Anything listed and sold between sweeps never enters it.
- **Memory is sometimes inferred.** Where a seller never states it, the card carries a
  `config unstated` badge and the note says what was assumed and why.

## Updating it

All state is the `const DATA = {…}` object near the top of the `<script>` block:

- `main` / `minis` — the listings. Set `status` to `live`, `new` or `gone`, and
  `facet_status` to `available`, `available,new` or `gone` to match.
- `sweeps` — one entry per sweep, oldest first: `{date, iso, offer:[…]}`. **Append a
  new entry each sweep** and the over-time charts gain a point automatically.
- `summary` — counts, price range, the BNR rate, and the `built` / `prev` / `checked`
  dates shown in the header.

The charts derive everything else. Nothing is hard-coded per tier.

## Design notes

Charts follow a few fixed rules, worth knowing before editing them:

- Chip generation and memory tier are **ordered**, so they use an ordinal single-hue
  ramp of the page's teal rather than categorical colours. The steps are validated
  against both the light (`#FFFFFF`) and dark (`#151D1E`) page surfaces — monotone
  lightness, adjacent gaps ≥ 0.06, and the step nearest the surface clearing 2:1.
- The same ramp step means the same tier in every chart.
- Dark mode uses its own steps chosen for the dark surface, not an inverted copy of the
  light ones — which is why the ramp reads darker-is-more in light mode and
  lighter-is-more in dark. The legend carries the order in both.
- Segment counts are not printed inside bars: two ramp steps cannot hold 10.5px text at
  4.5:1 contrast. The table twin under each chart carries every number instead.
- Deltas use arrow glyphs and signed values in text ink, never red/green alone.

## Licence

[MIT](LICENSE) — covers the page, its styles and its charting code.

It does not cover the listing content. Titles, descriptions, prices and photos belong
to the people who posted the ads on olx.ro; they are reproduced here with a link back
to each original listing.
