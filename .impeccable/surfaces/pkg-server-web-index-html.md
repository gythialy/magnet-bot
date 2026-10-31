---
version: 1
slug: "pkg-server-web-index-html"
primary_target: "pkg/server/web/index.html"
related_targets: []
---

# Surface brief — Telegram Mini App shell

**Scope:** the whole Mini App shell (`pkg/server/web`): Today, Keywords, Alarms, Records,
Settings. Mode: **Operate** (the visitor completes a task).

**Audience / job:** the operator and a few allow-listed colleagues, inside the Telegram
client on a phone, answering "did anything relevant come in, and does the right person
have access" in seconds.

**Chosen direction:** The Classified Column — a dense, rule-separated reading surface
with a column head, packed stat cells and tracked caps labels. One accent marks new
records. No cards in the reading flow.

**Memorable moment:** switching the date range changes only the column head's count and
the entries below it; the page does not jump or re-skeleton.

**Unresolved:** whether Keywords rows keep their count column on narrow phones.

## Direction contract

THESIS: The app is a printed column read in seconds, not a dashboard of cards. It refuses
the category default of rounded card-per-item and metric tiles, and refuses the shadcn
skin this project briefly carried.

OWN-WORLD: client ground with an ink-neutral text ramp; one accent (client button colour)
used only for new/unread marks and the active filter; 1px hairlines as the only
separator; no radius on list rows, 6px on controls, 12px on sheets; type is one grotesque
at 15/600 lead, 13/400 body, 11/500 tracked caps labels.

STORY: the visitor opens Today, reads the column head (what am I looking at, how many,
when updated), scans the entries in one pass, and either opens one, converts it, or
switches tab. Access state stays one tap away in Settings.

FIRST VIEWPORT: a full-bleed column head band (date + range label + count + updated
time), a single-row stat strip of four packed cells split by hairlines, then the search
field and range chips on one line, then hairline-separated entries with a bold lead line
and a 13px meta line, actions revealed on the entry's right edge; fixed tab bar below.

FORM: The Classified Column, ranked fifth of seven, raised by three donors (registration
furniture, whole-cell packing, per-cell update). seed:ec023492d215fd3e

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish
review, the verdict, DESIGN.md, and every shipping raster carrying its provenance.
