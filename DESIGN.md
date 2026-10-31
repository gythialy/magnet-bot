---
name: magnet Mini App
description: Telegram Mini App read as a printed column — notices, keywords, alarm records and administrator settings
colors:
  ground: "#ffffff"
  ink: "#09090b"
  ink-muted: "#71717a"
  rule: "rgba(9, 9, 11, 0.14)"
  rule-dark: "rgba(250, 250, 250, 0.2)"
  input-edge: "rgba(9, 9, 11, 0.14)"
  accent-mark: "#1668a8"
  primary: "#18181b"
  primary-ink: "#fafafa"
  destructive: "#dc2626"
  danger-soft: "rgba(220, 38, 38, 0.35)"
  scrim: "rgba(0, 0, 0, 0.5)"
  success: "#16a34a"
typography:
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "11px"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0.14em"
  caption:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.2
    letterSpacing: "0.1em"
  meta:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: "-0.011em"
  control:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "14px"
    fontWeight: 500
    lineHeight: 1.3
    letterSpacing: "-0.011em"
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "-0.011em"
  lead:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "15px"
    fontWeight: 600
    lineHeight: 1.35
    letterSpacing: "-0.011em"
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "17px"
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: "-0.011em"
  data:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.4
  display:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"
    fontSize: "20px"
    fontWeight: 650
    lineHeight: 1.25
    letterSpacing: "-0.011em"
rounded:
  none: "0"
  mark: "2px"
  sm: "4px"
  control: "6px"
  card: "10px"
  sheet: "14px"
  pill: "999px"
  circle: "50%"
spacing:
  row: "12px"
  gutter: "16px"
  group: "8px"
  section: "14px"
  tabbar: "58px"
components:
  column-head:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "2px 0 10px"
  stat-strip:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "10px 8px"
    height: "62px"
  entry:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "12px 0"
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-ink}"
    rounded: "{rounded.control}"
    padding: "9px 16px"
    height: "40px"
  button-outline:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "9px 16px"
    height: "40px"
  chip:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "5px 14px"
    height: "34px"
  chip-selected:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-ink}"
    rounded: "{rounded.pill}"
    padding: "5px 14px"
    height: "34px"
  input:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "9px 12px"
    height: "40px"
  sheet:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sheet}"
    padding: "18px"
---

# Design System: magnet Mini App

## Overview

**Creative North Star: "The Printed Column"**

The Mini App is read, not operated like a control panel. Its reading surface behaves
like a printed column: a head line that says what you are looking at and how fresh it
is, a packed strip of counters divided by rules, then entries separated by hairlines —
no cards, no tiles, no shadows. The operator opens it between meetings, glances down one
vertical rhythm, and closes it.

The system keeps the client's ground and ink, and spends its one accent on a single
piece of meaning: a mark for something new since the last look. Type does the sorting:
one grotesque, six steps, a bold lead line over a quiet meta line, tracked capitals for
labels. Controls stay conventional — a button looks like a button, an input like an
input — because the world lends density and typography, never an unfamiliar control.

Anything that floats may lift; anything in the flow may only be divided.

**Key Characteristics:**
- Hairline rules as the only separator; no borders wrapped around content, no cards.
- One accent, used for new records and the active filter.
- Six type steps (11 / 12 / 13 / 14 / 15 / 17 / 20) with tracked caps labels.
- Numbers are tabular; the column head is the only thing that animates on a range switch.
- Controls keep a 6px radius; sheets get 14px; entries get none.

## Colors

The palette is the client's, plus exactly one accent and two semantic signals.

### Primary
- **Client Action Ink** (`#18181b` fallback): filled buttons and the selected chip. At
  runtime this is the Telegram theme's `button_color`, so the app never fights the
  client's own palette.

### Neutral
- **Ground** (`#ffffff` fallback): the page. Cards do not exist, so there is no second
  surface colour to manage.
- **Ink** (`#09090b`): lead lines, body copy, active tab.
- **Muted Ink** (`#71717a`): meta lines, labels, inactive tabs.
- **Rule** (`rgba(9, 9, 11, 0.14)`; dark `rgba(250, 250, 250, 0.2)`): every separator in
  the system. There is exactly one weight.

### Tertiary (semantic only)
- **Accent Mark** (`#1668a8`): the new-record mark and the active filter underline.
  Derived from the client's link colour until it clears 4.5:1.
- **Destructive Red** (`#dc2626`) and **Danger Soft** (`rgba(220, 38, 38, 0.35)`): error
  text and the failed state's rule.
- **Scrim** (`rgba(0, 0, 0, 0.5)`): the backdrop behind a sheet.
- **Success Green** (`#16a34a`): success toasts only.

### Named Rules
**The One Accent Rule.** The accent marks the new and the selected. Any other use is
decoration and gets removed.

**The Client Ground Rule.** Background, ink and primary come from `themeParams`; a
hardcoded brand surface is a bug, not a preference.

## Typography

**Body Font:** `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif`

**Data Font:** `ui-monospace, SFMono-Regular, Menlo, Consolas, monospace` — identifiers
and endpoint URLs only (a webhook address, a version string), never prose.

**Character:** One grotesque at six sizes. The lead line is 15/600, its meta 13/400, the
section label 11/600 in capitals with `0.14em` tracking. Nothing is italic, nothing is
decorative; the column reads top to bottom.

### Hierarchy
- **Display** (650, 20px, 1.25): the gate and empty-state titles.
- **Headline** (650, 17px, 1.3): sheet titles and stat values.
- **Lead** (600, 15px, 1.35): entry lead lines, card titles, section headings.
- **Body** (400, 15px, 1.5): message text, record details.
- **Control** (500, 14px): buttons, inputs, chips, tabs.
- **Meta** (400, 13px, 1.4): dates, identifiers, supporting lines.
- **Caption** (500, 12px, `0.1em`): stat labels, badges.
- **Label** (600, 11px, `0.14em`, uppercase): column heads, section labels, sheet keys.

### Named Rules
**The Tabular Rule.** Every number that lines up in a column uses tabular figures. The
counters change while the page is open; proportional digits would move the page.

**The Caps Label Rule.** A label is 11px, 600, uppercase, `0.14em`. If it needs to be
bigger to be read, it is a heading, not a label.

## Layout

One column inside `#view`: 16px side gutters, 16px above the first rule plus
`safe-area-inset-top`, and enough bottom padding for the fixed 58px tab bar plus 20px.
The reading surface has no horizontal padding inside rows — rules run edge to edge.

Order on Today: column head, stat strip, search, range chips, entries. On Records:
column head, search, entries. Entries are 12px apart vertically, separated by one rule;
a group never exceeds four stat cells or one head line.

## Elevation & Depth

Flat. Nothing in the flow has a shadow; separation is a single 1px rule. Sheets, dialogs
and toasts are the only lifted things and carry the one shadow
(`0 10px 38px rgba(9, 9, 11, 0.18)`, dark `rgba(0, 0, 0, 0.5)`).

### Named Rules
**The Flow Is Flat Rule.** If it scrolls with the page it is flat: rule, background, or
nothing. Shadows are for surfaces that cover other content.

## Shapes

Icons are one drawn family: 24-unit viewBox, 1.6px stroke, round caps, 16px on screen
(12px inside a tag), `currentColor` only. Edit, delete, PDF, PNG, external link, close
and the tender mark all come from it; no emoji or font glyph stands in for an icon.

Entries are square: a list row with rounded corners reads as a card and breaks the
column. Controls take 6px, chips and badges are pills, sheets take 14px. Marks (the new
indicator, the tab underline) are 2px and square-ended. Chevrons and the active tab
indicator are drawn with borders, never with font glyphs.

## Components

### Column head
The band above a list: `LABEL` (11px caps) on the left, the count in 13/600 tabular
beside it, `updated HH:MM` in 11px muted on the right, and one rule below. The count is
the only element that changes on a filter switch, so it is the only element that fades.

### Stat strip (signature)
Four cells in one row, edges packed, divided by 1px rules, no background of their own:
17/650 tabular value over an 11px caps label. It replaces the category's metric cards.

### Entry (signature)
A full-width row: 15/600 lead clamped to two lines, 13px meta beneath, actions on the
right edge, one rule below, square corners. The whole row opens the detail.

### Buttons
6px radius, 40px tall; primary is the theme's button colour, outline is a 1px
`{colors.input-edge}` stroke on the ground, ghost paints `--accent-fill` on press. Focus
is a 2px accent ring, never a glow.

### Chips
Pills, 34px tall, 1px stroke; the selected chip inverts to the theme button colour. Range
selection only.

### Inputs
40px tall, 6px radius, 1px `{colors.input-edge}` stroke, ground background; focus turns
the stroke to the accent and adds a 3px 16%-alpha ring.

### Tab bar
Fixed, 58px, 11px caps labels with `0.12em` tracking; the active tab is ink with a 20px
2px accent mark on the top edge. Inactive tabs are muted ink.

### Row actions
Quiet by default: a 36px square target with a transparent border, the icon in muted ink;
press or hover adds the `accent-fill` wash and ink. A destructive action only turns red
on press, never at rest.

### Sheets
14px top radius, grab handle, 18px padding, 17px title, rows separated by rules with
11px caps keys. The only lifted surface.

## Do's and Don'ts

### Do:
- **Do** separate with a single 1px rule; a second weight or a double border is undefined.
- **Do** keep the reading column edge-to-edge and square; rounding a row turns it back
  into a card.
- **Do** put the count and the timestamp in the column head, not in the entries.
- **Do** use tabular figures and `nowrap` for counters so a value change cannot reflow.
- **Do** state a mode twice when it matters: the access switch is drawn and the mode is
  written beside it.

### Don't:
- **Don't** put cards in a reading flow, or wrap a list in a card that wraps another.
- **Don't** give an in-flow surface a shadow, a gradient, or a coloured left border.
- **Don't** introduce a second accent; the mark is the only colour that does a job.
- **Don't** use emoji or font glyphs as icons — draw chevrons, marks and arrows.
- **Don't** ship the dashboard vocabulary: hero metrics, coloured stat tiles, sparklines.
