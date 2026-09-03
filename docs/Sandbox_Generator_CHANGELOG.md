# Sandbox Generator — Version History

Unofficial changelog for the four PDF releases of *Sandbox Generator*
(Atelier Clandestin), reconstructed by comparing the files themselves.
The publisher ships no changelog; this was derived from embedded PDF
metadata and extracted text.

| Version | Released | Pages | Size | Producer |
|---|---|---|---|---|
| `Sandbox_Generator_v2023-04-04.pdf` | 2023-04-04 16:14 CEST | 158 | 5.9 MB | pdftk 1.41 / iText |
| `Sandbox_Generator_v2023-10-03.pdf` | 2023-10-03 10:20 CEST | 158 | 3.5 MB | (none; AcroForm) |
| `Sandbox_Generator_v2024-07-05.pdf` | 2024-07-05 09:57 CEST | 159 | 5.8 MB | Affinity Publisher 2.0.0 / PDFlib |
| `Sandbox_Generator_v2024-07-15.pdf` | 2024-07-15 10:34 CEST | 159 | 5.8 MB | Affinity Publisher 2.0.0 / PDFlib |

**Current version: `v2024-07-15`.**

All four came from DriveThruRPG product `rpg/12407`. The original filenames
used DD-MM-YY dates; files have been renamed to ISO dates so they sort
chronologically. The first release was distributed with no date in its
filename, which made it look like the newest — it is in fact the oldest.

---

## v2023-04-04 — First edition

Baseline. 158 pages, no ISBN block, copyright line only. Assembled with
pdftk from a separate export rather than published directly from the
layout tool, unlike every later release.

A 155-page preview (`previewb.pdf`, created 19 minutes after this file)
corresponded to this edition. It was fully rasterized — no extractable
text, 25 MB — and has been deleted; it is re-downloadable from the
product page if needed.

---

## v2023-10-03 — ISBN registration and errata

A light copy-editing pass. Page count unchanged at 158; the table of
contents and all page numbers are identical. Every change is listed below
— this diff is small enough to be exhaustive.

### Publication data added

- Publisher name "Atelier Clandestin" added to the title page.
- ISBN/EAN block added for three formats: softcover (978-2-9603330-1-5),
  hardcover (978-2-9603330-2-2), PDF (978-2-9603330-3-9).
- Back cover gained the copyright line, the Belgian legal deposit
  (`D/2023/Atelier Clandestin, publisher.`), and the PDF ISBN/EAN.

### Table fix

- **Abbey names (p. 28).** The d10 roll table listed only entries 1–8,
  leaving rolls of 9 and 10 undefined. Entry 8 "Saint-…" was rebased to
  cover **8–10**, and the instruction was simplified from
  "Roll 1d10 (then 1d3 or 1d30 if needed)" to "Roll 1d10", with the
  sub-table now referenced as "(see next table)".

### Content edits

- Heraldry charges: "4 Halberd" → "4 **Halberd/Spear**".
- Tavern names: new cross-reference added — "The tables on pp. 28-31 can
  also be used to generate tavern names."
- Worked example: the sample tavern renamed "The Red Oak" → "**The Red Den**".

### Typos and wording

- "Access to *an* special location" → "a special location".
- "They *were been* petrified by a:" → "They were petrified by a:".
- "Priest(**ress**)" → "Priest(**ess**)".
- "The keep jails contain…" → "The jails of the keep contain…".
- "If you *don't* have a d24" → "If you *do not* have a d24".

---

## v2024-07-05 — Major revision

The largest change of the four, and the only one that alters game
mechanics. The book was re-typeset from scratch in Affinity Publisher
(the two 2023 files were stitched with pdftk), so the whole text reflows;
the substantive changes are separated from that noise below.

Chapter structure is **unchanged** — the table of contents and every
section page number match v2023-10-03 exactly. One page was added (158 →
159) in the back matter. All 100 special rooms are still present; none
were added or dropped.

### Mechanical changes

- **City services are now randomized.** Every city previously had exactly
  `n` of each service (where `n` was city size). It now has `1dn` of each
  — so a size-3 city rolls 1d3 separately for blacksmiths, cemeteries,
  churches, general stores, markets, stables and taverns instead of
  getting three of each. The worked example changed accordingly, from
  "three of each" to "2 blacksmiths, 2 cemeteries, 1 church, 3 general
  stores, 1 market, 2 stables and 3 taverns".
- **"City size" renamed "city grade"** throughout, to avoid collision with
  physical size.
- **Libraries dropped** from the guaranteed city services list.
- **Lairs chapter restructured.** The old `1) Inhabitants` subsection was
  folded into a flowing introduction, and a new **`1) Procedure`**
  subsection was added covering how a lair is found in play — spelling out
  two encounter scenarios and the monster's *% in lair*. The % in lair
  mechanic was previously flagged "(Optional)"; it is now the standard
  procedure.
- **Cities gained a new subsection** early in the chapter (a **Color**
  table — greyscale, sand and terracotta, etc.), pushing the later
  subsections down: Points of Interest 4→5, People 7→8, Events 9→10.

### Renamed entries

| Old | New |
|---|---|
| Natural landmark: `Vegetal` | `Flora` |
| Natural landmark: `Geological` | `Geology` |
| Natural landmark: `Water` | `Hydrology` |
| Artificial landmark: `History` | `Events` |
| Magic landmark: `Mysterious` | `Mystery` |
| Magic landmark: `Religious` | `Ruin` |
| Biome: `Coast` | `Coast/Beach` |
| Special room 26: `Fake gold items` | `Fake treasure` |
| Special room 53: `Medicine cabinet` | `Medical office` |
| Special room 88: `Tentacle room` | `Tentacles room` |
| `Castles/Cities` | `Cities/Castles` |
| Sea creature: `siren` | `mermaid` |

### Style and consistency pass

- **Contractions eliminated** — 32 occurrences of don't / can't / isn't /
  doesn't / aren't / didn't / won't reduced to 2, matching the formal
  register the Oct 2023 pass had started.
- **Stat notation spaced out**: `+3HD, +2AC, +2ML` → `+ 3 HD, + 2 AC, + 2 ML`.
- **Units normalized**: `1h` / `24h` / `min` → a consistent `h` form;
  `xp` → `XP`; `hp` → `HP`.
- Typos fixed: `Sheeps` → `Sheep`, `Rased` → `Razed`, `istands`,
  `Self-confidence` → `self-confidence`, `comrade` → `comrades`.
- Headings capitalized consistently (`Type & treasure` → `Type & Treasure`).

### Publication data

- Fourth ISBN added for a **spiral-bound** edition (978-2-931269-03-9),
  and the ISBN block reordered to lead with the PDF edition.
- Storefront links updated: blog moved from `atelierclandestin.wixsite.com/home`
  to **`atelierclandestin.net`**; **Amazon** and **Lulu** author pages
  added; **Redbubble** removed; remaining URLs dropped their `www.` prefix.

---

## v2024-07-15 — Single-sentence errata

Ten days after the previous release, and the smallest change of the set.
Byte size differs by under 2 KB. **Exactly one edit**, in special room
**64) No way back** — the last stray "malus" in the book, replaced with
"penalty" and the sentence reordered:

> **Before:** All of their reaction rolls have a malus (− 3).
> **After:** They have a penalty (− 3) to all of their reaction rolls.

No other textual difference exists between v2024-07-05 and v2024-07-15.

---

## Method and limitations

Dates are the PDFs' embedded `CreationDate` / XMP `xmp:CreateDate`, shown
in the publisher's own timezone (CEST, +02:00). Filesystem timestamps
carry no signal — all four were downloaded in a single session on
2026-09-03.

Comparisons were made on `pdftotext -layout` output, normalized for
whitespace and page numbers. Two caveats:

- The book is set in **two columns**, which the extractor interleaves. Raw
  line diffs therefore contain heavy false-positive noise; every change
  listed above was confirmed by targeted term-frequency checks across
  versions rather than taken from the line diff alone. Several apparent
  additions and deletions in the raw diff turned out to be interleaving
  artifacts and were excluded.
- **Only text was compared.** Changes to artwork, maps, page layout,
  typography or color are not detectable this way and are not reflected
  here. The v2024-07-05 re-typeset in particular is likely to carry
  visual changes beyond what is listed.
