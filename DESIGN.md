---
name: magnet Mini App
description: Telegram Mini App read as a printed column — a merged watchlist, four tabs, a two-level settings surface and a device-local language switch
colors:
  ground: "#ffffff"
  ink: "#09090b"
  ink-muted: "#71717a"
  ink-muted-strong: "#66666e"
  ink-faint: "#8d8d96"
  rule: "rgba(9, 9, 11, 0.12)"
  rule-strong: "rgba(9, 9, 11, 0.22)"
  fill: "rgba(9, 9, 11, 0.045)"
  fill-strong: "rgba(9, 9, 11, 0.08)"
  surface-2: "#f4f4f5"
  accent: "#1668a8"
  accent-soft: "rgba(22, 104, 168, 0.12)"
  primary: "#18181b"
  primary-ink: "#fafafa"
  destructive: "#dc2626"
  destructive-fill: "#dc2626"
  destructive-soft: "rgba(220, 38, 38, 0.1)"
  success: "#16a34a"
  scrim: "rgba(0, 0, 0, 0.42)"
  ground-dark: "#17212b"
  ink-dark: "#f5f5f5"
  ink-muted-dark: "#93a2b1"
  ink-muted-strong-dark: "#a9b7c5"
  ink-faint-dark: "#6f8090"
  rule-dark: "rgba(245, 245, 245, 0.14)"
  rule-strong-dark: "rgba(245, 245, 245, 0.26)"
  fill-dark: "rgba(245, 245, 245, 0.06)"
  fill-strong-dark: "rgba(245, 245, 245, 0.12)"
  surface-2-dark: "#232e3c"
  accent-dark: "#6ab3f3"
  accent-soft-dark: "rgba(106, 179, 243, 0.16)"
  primary-dark: "#5288c1"
  primary-ink-dark: "#ffffff"
  destructive-dark: "#f87171"
  destructive-fill-dark: "#b91c1c"
  destructive-soft-dark: "rgba(248, 113, 113, 0.16)"
  success-dark: "#4ade80"
typography:
  display:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans SC', 'Microsoft YaHei', Arial, sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.3
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans SC', 'Microsoft YaHei', Arial, sans-serif"
    fontSize: "17px"
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: "-0.02em"
  lead:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans SC', 'Microsoft YaHei', Arial, sans-serif"
    fontSize: "15px"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "-0.01em"
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans SC', 'Microsoft YaHei', Arial, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
  meta:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans SC', 'Microsoft YaHei', Arial, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans SC', 'Microsoft YaHei', Arial, sans-serif"
    fontSize: "11px"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0.12em"
  data:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.4
rounded:
  none: "0"
  control: "6px"
  tile: "10px"
  sheet: "14px"
  pill: "999px"
  circle: "50%"
spacing:
  gutter: "16px"
  row: "11px"
  group: "26px"
  tabbar: "58px"
components:
  topbar:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "10px 16px"
    height: "61px"
  runhead:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "18px 16px 12px"
  stat:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "10px 8px 11px"
  segmented:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.control}"
    padding: "3px"
    height: "40px"
  search:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "0 10px"
    height: "40px"
  row:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "11px 16px"
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-ink}"
    rounded: "{rounded.control}"
    height: "40px"
  switch:
    backgroundColor: "{colors.fill-strong}"
    rounded: "{rounded.pill}"
    width: "48px"
    height: "30px"
  sheet:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sheet}"
    padding: "6px 0 14px"
  toast:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.ground}"
    rounded: "{rounded.tile}"
    padding: "10px 12px"
  tabbar:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.none}"
    height: "58px"
---

# Design System: magnet Mini App

## Overview

**Creative North Star: "The Printed Column, bilingual"**

The Mini App is read, not operated like a control panel. Its reading surface behaves
like a printed column: a head line that says what you are looking at and how fresh it
is, a packed strip of counters divided by rules, then entries separated by hairlines —
no cards, no tiles, no shadows in the flow. The operator opens it between meetings,
glances down one vertical rhythm, and closes it.

v4 keeps that column and changes the information architecture around it: the watchlist
merges keywords and alarm words into one tab, archiving is a reversible state instead of
a deletion, an administrator can open anyone else's library read-only, settings split
into a short root plus an admin console, and language is a device-local switch in exactly
one place. The page still speaks two scripts with one component set: Chinese overrides
variables (leading, label size and tracking), never selectors.

**Key Characteristics:**
- Hairline rules are the only separator; no borders wrapped around content, no cards.
- Four colours, four meanings: ink for selection and primary action, accent for
  position / new content / focus, green for status only, red for destructive only.
- One type ramp with two parameters: Latin is tighter, CJK larger with more leading.
- Every state has an exit: loading, failed refresh, empty, rate limited.
- Reversible actions offer the undo toast; irreversible ones name the object.
- Long lists never sit on the settings root; they live on a sub-page.

## Colors

The palette is the client's, plus one accent and two semantic signals. Telegram
`themeParams` supply ground, ink, hint, button and secondary surfaces at runtime; the
derived neutrals and the dark values below are registered here because they are
hard-coded.

### Primary
- **Client Action Ink** (`#18181b`, dark fallback `#5288c1`): filled buttons, the selected
  segment, the switch's "on" state, the toast. At runtime this is the theme's
  `button_color`.
- **Primary Ink** (`#fafafa`, dark `#ffffff`): the label on a primary fill.

### Neutral
- **Ground** (`#ffffff`, dark `#17212b`): the page, and the only surface colour.
- **Ink** (`#09090b`, dark `#f5f5f5`): lead lines, body copy, the active tab.
- **Muted Ink** (`#71717a`, dark `#93a2b1`): meta lines, labels, inactive tabs.
- **Muted Strong** (`#66666e`, dark `#a9b7c5`): names beside ids, sheet values.
- **Faint Ink** (`#8d8d96`, dark `#6f8090`): separators inside meta lines, inactive row
  actions on archived rows.
- **Rule** (`rgba(9, 9, 11, 0.12)`, dark `rgba(245, 245, 245, 0.14)`): every separator.
- **Rule Strong** (`rgba(9, 9, 11, 0.22)`, dark `rgba(245, 245, 245, 0.26)`): the one
  inline error border, the sheet grab handle.
- **Fill** (`rgba(9, 9, 11, 0.045)`, dark `rgba(245, 245, 245, 0.06)`): press wash,
  avatar ground, pinned row ground.
- **Fill Strong** (`rgba(9, 9, 11, 0.08)`, dark `rgba(245, 245, 245, 0.12)`): switch
  track off, skeleton pulse.

### Semantic
- **Accent** (`#1668a8`, dark `#6ab3f3`): the active tab's 2px mark, the newest-unseen
  dot, the selected member tint, link text, focus rings. Derived from the client's link
  colour until it clears 4.5:1 on the current ground; when a client colour needs help it
  is mixed toward the ground's opposite (the dark derived values `#3d6f9e` / `#b91c1c`
  are examples of that step, not extra tokens).
- **Accent Soft** (`rgba(22, 104, 168, 0.12)`, dark `rgba(106, 179, 243, 0.16)`): the
  read-only banner, the selected member row, the search focus ring.
- **Success** (`#16a34a`, dark `#4ade80`): exactly one use — the access-control shield.
  It never fills a control.
- **Destructive** (`#dc2626`, dark `#f87171`): the delete icon, the revoke action, the
  inline-error icon.
- **Destructive Fill** (`#dc2626`, dark `#b91c1c`): the confirm button of an
  irreversible action only.
- **Destructive Soft** (`rgba(220, 38, 38, 0.1)`, dark `rgba(248, 113, 113, 0.16)`):
  press wash of a destructive icon.
- **Scrim** (`rgba(0, 0, 0, 0.42)`): the backdrop behind a sheet.

### Named Rules
**The One Accent Rule.** The accent marks where you are, what is new and what has focus.
Any other use is decoration and gets removed.

**The Status Green Rule.** Green is a status colour (the access shield), never a control
fill. A green "on" switch was removed in v4 because green there meant "success", not
"selected".

**The Client Ground Rule.** Background, ink, hint and primary come from `themeParams`;
a hardcoded brand surface is a bug, not a preference. Appearance auto/light/dark is a
device-local override that writes the palette above, not a second theme.

## Typography

**One family, two scripts, two parameter sets.** The same component writes `.lead`,
`.body`, `.meta`, `.label`; `html[lang^="zh"]` only changes the variables behind them.
No external fonts.

| Step | English | Chinese |
| --- | --- | --- |
| Headline (page, sheet, stat value) | 17 / 650 / 1.3 / -0.02em | 17 / 650 / 1.4 / 0 |
| Lead (row and entry titles) | 15 / 600 / 1.4 / -0.01em | 15 / 600 / 1.55 / 0 |
| Body (copy, errors, empty notes) | 15 / 400 / 1.5 | 15 / 400 / 1.7 |
| Meta (dates, ids, supporting lines) | 13 / 400 / 1.45 | 13 / 400 / 1.6 |
| Label (field keys, stat labels) | 11 / 600 / 0.12em / uppercase | 12 / 600 / 0.04em / none |

- Labels are one shared `.label` class driven by `--label-size`, `--label-ls`,
  `--label-transform`; a component never restyles a label for a script.
- Chinese adds `line-break: strict` so punctuation does not start a line.
- Long identifiers wrap with `overflow-wrap: anywhere`; English leads clamp at two lines.
- The mono `data` face is for identifiers and endpoints only, never prose.

### Named Rules
**The Tabular Rule.** Every number that lines up in a column uses tabular figures. The
counters change while the page is open; proportional digits would move the page.

**The Variable Rule.** A script difference is a variable override. If a language rule
needs its own selector, the component is wrong.

## Layout

The shell is a full-height flex column: `#view` holds one absolute `.view` at a time and
the tab bar is its own row, so a scrolling `.screen` always ends above the bar.

- Tab roots: Today, Watchlist, Records, Settings. Sub-pages (`#/members`, `#/admin`)
  are full pages without a tab; the parent tab stays highlighted and their top bar is
  back + title + spacer.
- Top bar: sticky, brand + refresh on tab roots; back + title on sub-pages. 16px side
  gutters, `safe-area-inset-top` honoured.
- Today: running head (title + count + unit + updated time), four-cell stat strip, range
  segment + search, then entries. Watchlist: page head (+ Members for admins), word-type
  segment + search + Add, list head with the archived toggle, then rows and — when the
  toggle is on — the archived group. Settings: fixed-height groups, About before
  Administration. Records: page head, search, rows.
- One rule per separation, edge to edge. Rows are square; controls are 6px; sheets are
  14px; marks are 2px.

## Elevation & Depth

Flat. Nothing in the flow has a shadow; separation is one 1px rule. Only sheets and
toasts lift, with the two registered shadows (`--shadow-lift`, `--shadow-float`).

### Named Rules
**The Flow Is Flat Rule.** If it scrolls with the page it is flat: rule, background, or
nothing. Shadows are for surfaces that cover other content.

## Shapes

One drawn icon family: 24-unit viewBox, 1.6px stroke, round caps, `currentColor`, 16px
inline / 18px in the top bar. No emoji, no font glyphs.

Controls use 6px, tiles and toasts 10px, sheets 14px, pills and avatars are round. List
rows, entries and group labels are square; rounding a row reads as a card and breaks the
column.

## Components

### Top bar
Sticky, ground background, one bottom rule. Root: brand (7px accent dot + wordmark) and
a 40px refresh icon button. Sub-page: back chevron, title, 40px spacer to keep the title
optically centred. A 44px touch target in a 40px box.

### Running head
`title` 17/650, count in accent 17/650, unit 13 muted, updated time 11 muted on the
right. The count and time are the only things that change on a refresh.

### Stat strip (signature)
Four cells in one row, edges packed, divided by hairlines, no background: 17/650 tabular
value over a label. It replaces metric cards; it is never interactive.

### Segmented control
40px tall, 6px radius, 1px rule, 3px inner padding. The selected segment fills with
primary ink; inactive segments are muted. Used for word type (Keywords / Alarm words) and
date range (Today / 7 days / All).

### Search
40px tall, 1px rule, leading glyph; focus turns the border to accent and adds the 3px
accent-soft ring. Search is in-place for the watchlist (already-loaded rows) and
server-side where the dataset is paged (members, records, notices).

### Rows and entries
- Row: identity or word on the left, a 13px meta line beneath, actions on the right, one
  rule below. Actions are 40px icon targets, muted at rest, ink on press; destructive
  turns red only on press.
- Entry (Today): 15/600 lead clamped to two lines, a 13px meta line (new dot, time,
  project code, tender tag) and three icon actions (PDF, PNG, open) on the same line.
- Archived rows mute the lead and swap actions to restore + delete.
- Disabled member rows mute the name; the meta line says "Disabled".

### Identity
Every list of people uses one rule: `remark` → Telegram name → `@username` → `#id
unnamed`. The name comes first, the id second and always stays visible; with no name the
row says "unnamed" instead of pretending the id is a name. The read-only banner adds the
role, and swaps it for "Disabled" when the account is disabled.

### Switch
48×30 (40×24 inline), pill track. Off is fill-strong + rule; on is primary ink. Green is
never the on-state. The inline "Show archived" label carries the archived count in a
count chip beside it.

### Sheets and choices
Bottom sheet, scrim, grab handle, 14px top radius, title 17/650 + kicker 12 muted,
11px label columns in the consequence rows, actions split evenly. Reversible confirm
sheets show consequence first (what happens, who it affects, whether it can be undone).
Choice rows (language, appearance) show a check mark for the current value; the language
sheet is the only language switch in the app.

### Toasts
Ink ground, ground text, 10px radius, one line, optional underline action. The archive
toast is the canonical reversible toast: it names the word and offers Undo for five
seconds.

### States
- **Loading**: skeleton blocks at the final row height; one `role="status"` line names
  the list being loaded; counters reserve their space.
- **Failed refresh**: keep the rows, add one inline error with the time of the last good
  list and one retry. Never replace data with an empty state.
- **Empty**: title, one explanation and the action that fills it (Add, or send the word
  to the bot); a filter with no match says so and offers nothing destructive.
- **Rate limited (429)**: the same error bar shape with the "too many requests" copy.
  The current build surfaces write throttling as the same toast component; the bar is the
  intended host when a countdown is available.

### Tab bar
Four tabs, 58px plus safe-area, 11px labels without tracking, the active tab in ink with
a 22px 2px accent mark above it. Each tab owns one icon from the shared family.

## Bilingual & Copy Rules

- Strings live in `js/locales/en.js` and `js/locales/zh.js`; `js/i18n.js` exposes
  `t`, `plural`, `num`, `date`, `time`. No English fragment is concatenated with a
  translated one.
- Plural selection goes through `Intl.PluralRules`; numbers through
  `Intl.NumberFormat`; dates through `Intl.DateTimeFormat`.
- Language resolution: stored preference → Telegram `language_code` → device languages
  → English. `<html lang>` and `document.title` follow. Chinese variants (`zh`,
  `zh-hans`, `zh-cn`, `zh-tw`) all resolve to `zh-Hans`.
- Switching language rewrites text nodes only. It must not rebuild the DOM, close a
  sheet, drop a draft, move focus or jump the scroll position.
- Every accessible name has a key (`aria.*`); no bare `aria-label` literals.
- Visible copy is short and consequence-first. Destructive confirmations name the object.

## Do's and Don'ts

### Do:
- **Do** separate with a single 1px rule; a second weight or a double border is undefined.
- **Do** keep rows and entries square and edge-to-edge; rounding turns a row into a card.
- **Do** teach a state its exit: skeleton → data, failure → retry, empty → action,
  429 → wait or retry.
- **Do** use ink for selection and primary action, accent for position / new / focus,
  green only for the shield, red only for destructive.
- **Do** keep long lists off the settings root; About keeps its position for every role.
- **Do** make reversible actions undoable for five seconds, and name the object.
- **Do** express script differences as variable overrides on `[lang]`.

### Don't:
- **Don't** put cards in a reading flow, or wrap a list in a card that wraps another.
- **Don't** give an in-flow surface a shadow, a gradient, or a coloured left border.
- **Don't** introduce a second accent, or use success green as a control fill.
- **Don't** use emoji or font glyphs as icons.
- **Don't** duplicate the language switch in the top bar; settings → this device is the
  only entry.
- **Don't** ship dashboard vocabulary: hero metrics, coloured stat tiles, sparklines.
- **Don't** put a member list or a denied list back on the settings root.
