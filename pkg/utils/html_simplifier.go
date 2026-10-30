package utils

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// SimplifyHTML rewrites notice HTML into the markup Telegram's HTML parse mode
// renders: b/i/u/s/code/pre/blockquote/a/span.tg-spoiler plus literal newlines.
//
// It rewrites instead of stripping because Telegram renders the message as
// HTML: block elements have to become line breaks, unsupported elements must be
// unwrapped (their text kept), and text must be escaped so a stray "<" cannot
// make the message unparseable.
//
// Relative links are resolved against baseURL; when it is empty they degrade
// to their text.
//
// The second result reports whether the notice contains a table too complex to
// flatten (see complexTableColumns/complexTableRows): such a table is replaced
// by a placeholder line and the caller is expected to attach the original page
// as a PDF instead.
//
// Call it once per notice: the result encodes line breaks as literal newlines
// (Telegram has no <br>), so running it again would fold them into spaces.
//
// See https://core.telegram.org/bots/api#html-style
func SimplifyHTML(content, baseURL string) (string, bool) {
	root, err := html.Parse(strings.NewReader(content))
	if err != nil {
		// Not even leniently parseable: keep the text so nothing is lost.
		return escapeText(collapseSpaces(strings.TrimSpace(content))), false
	}

	r := htmlRenderer{base: baseURL}
	r.children(root)
	return strings.ReplaceAll(strings.TrimSpace(string(r.b)), " \n", "\n"), r.complexTable
}

const (
	// complexTableColumns and complexTableRows mark a table as too complex to
	// flatten: beyond them a " | " text table stops being readable.
	complexTableColumns = 6
	complexTableRows    = 10

	// complexTablePlaceholder replaces such a table in the message body.
	complexTablePlaceholder = "（表格较大，已省略；完整内容见附件 PDF）"
)

// telegramTags maps a source element onto the tag Telegram renders.
var telegramTags = map[string]string{
	"b": "b", "strong": "b",
	"i": "i", "em": "i",
	"u": "u", "ins": "u",
	"s": "s", "strike": "s", "del": "s",
	"code":       "code",
	"pre":        "pre",
	"blockquote": "blockquote",
	"tg-spoiler": "tg-spoiler",
}

// blockTags start a new line in the rendered message. pre and blockquote are
// also formatting tags, so they appear in both maps.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true,
	"footer": true, "main": true, "aside": true, "nav": true, "figure": true,
	"figcaption": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "ul": true, "ol": true, "dl": true, "li": true,
	"dd": true, "dt": true, "table": true, "thead": true, "tbody": true,
	"tfoot": true, "tr": true, "pre": true, "blockquote": true, "hr": true,
}

// droppedTags are removed together with their content: none of them can be
// rendered in a Telegram message, and their text is script/style noise.
var droppedTags = map[string]bool{
	"script": true, "style": true, "head": true, "title": true, "meta": true,
	"link": true, "noscript": true, "img": true, "svg": true, "iframe": true,
	"input": true, "button": true, "select": true, "textarea": true,
}

// htmlRenderer accumulates the Telegram-safe markup.
type htmlRenderer struct {
	b   []byte
	pre int // nesting depth inside <pre>, where whitespace is preserved

	base         string // base URL for relative links
	complexTable bool   // a table was replaced by complexTablePlaceholder
}

func (r *htmlRenderer) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			r.text(c.Data)
		case html.ElementNode:
			r.element(c)
		}
	}
}

func (r *htmlRenderer) element(n *html.Node) {
	name := strings.ToLower(n.Data)

	if droppedTags[name] {
		return
	}

	switch name {
	case "br", "hr":
		r.newline()
		return
	case "a":
		r.link(n)
		return
	case "span":
		if hasClass(n, "tg-spoiler") {
			r.b = append(r.b, `<span class="tg-spoiler">`...)
			r.children(n)
			r.b = append(r.b, "</span>"...)
			return
		}
		r.children(n) // plain span: unwrap, the style is not renderable
		return
	case "table":
		r.table(n)
		return
	case "li":
		r.newline()
		r.b = append(r.b, "• "...)
		r.children(n)
		r.newline()
		return
	}

	if tag, ok := telegramTags[name]; ok {
		block := blockTags[name]
		if block {
			r.newline()
		}
		if name == "pre" {
			r.pre++
		}
		r.b = append(r.b, "<"+tag+">"...)
		r.children(n)
		r.b = append(r.b, "</"+tag+">"...)
		if name == "pre" {
			r.pre--
		}
		if block {
			r.newline()
		}
		return
	}

	if blockTags[name] {
		r.newline()
		r.children(n)
		r.newline()
		return
	}

	// Unknown inline element (font, small, ...): keep the text, drop the tag.
	r.children(n)
}

func (r *htmlRenderer) link(n *html.Node) {
	href := ResolveURL(r.base, attr(n, "href"))
	if !isSafeHref(href) {
		r.children(n) // keep the link text, drop the unusable target
		return
	}
	r.b = append(r.b, `<a href="`...)
	r.b = append(r.b, escapeAttr(href)...)
	r.b = append(r.b, `">`...)
	r.children(n)
	r.b = append(r.b, "</a>"...)
}

// table flattens a table into one line per row with cells joined by " | ".
// Telegram has no table support, and a flattened table stays readable. A table
// too wide or too tall to read that way is replaced by a placeholder and
// reported to the caller, which attaches the original page as a PDF.
func (r *htmlRenderer) table(n *html.Node) {
	if r.isComplexTable(n) {
		r.complexTable = true
		r.newline()
		r.b = append(r.b, complexTablePlaceholder...)
		r.newline()
		return
	}

	// A one-row, one-cell table is layout, not data: the publishing system wraps
	// whole notices in one so it can apply its stylesheet. Flattening that cell
	// onto a single line would merge every paragraph, so render it as block
	// content instead and keep the line breaks.
	if cell, ok := layoutTableCell(n); ok {
		r.newline()
		r.children(cell)
		r.newline()
		return
	}

	r.newline()
	for _, line := range flattenTable(n, r.base) {
		r.newline()
		r.b = append(r.b, line...)
	}
	r.newline()
}

// layoutTableCell reports whether the table is a one-row, one-cell wrapper used
// for page layout, and returns that cell.
func layoutTableCell(n *html.Node) (*html.Node, bool) {
	rows := tableRows(n)
	if len(rows) != 1 {
		return nil, false
	}
	cells := rowCells(rows[0])
	if len(cells) != 1 {
		return nil, false
	}
	return cells[0], true
}

// isComplexTable reports whether a table is too wide or too tall to flatten.
// Merged cells are not counted: the grid layout handles them.
func (r *htmlRenderer) isComplexTable(n *html.Node) bool {
	rows := tableRows(n)
	if len(rows) >= complexTableRows {
		return true
	}

	columns := 0
	for _, row := range rows {
		width := 0
		for _, cell := range rowCells(row) {
			width += clampSpan(attrInt(cell, "colspan"))
		}
		columns = max(columns, width)
	}
	return columns >= complexTableColumns
}

// tableSlot is one grid position. A rowspan cell shares its slot with every row
// it covers, so its text is repeated there and each line reads on its own.
type tableSlot struct {
	text string
}

// flattenTable lays the table out on a grid so merged cells keep the columns
// aligned: a colspan cell fills its first column only (the spare columns are
// reserved, not rendered), and a rowspan cell is repeated on every row it
// covers.
func flattenTable(n *html.Node, baseURL string) []string {
	rows := tableRows(n)
	grid := make([][]*tableSlot, len(rows))

	for r, row := range rows {
		col := 0
		for _, cell := range rowCells(row) {
			for col < len(grid[r]) && grid[r][col] != nil {
				col++ // skip columns taken by a rowspan/colspan from above
			}

			text := renderFragment(cell, baseURL)
			if text != "" && strings.EqualFold(cell.Data, "th") {
				text = "<b>" + text + "</b>" // header cells stand out
			}

			colspan := clampSpan(attrInt(cell, "colspan"))
			// A rowspan cannot reach past the last row of this table.
			rowspan := min(clampSpan(attrInt(cell, "rowspan")), len(rows)-r)

			slot := &tableSlot{text: text}
			grid[r] = growRow(grid[r], col+colspan)
			grid[r][col] = slot
			for k := 1; k < colspan; k++ {
				grid[r][col+k] = &tableSlot{} // reserved column
			}

			for rr := r + 1; rr < r+rowspan; rr++ {
				grid[rr] = growRow(grid[rr], col+colspan)
				grid[rr][col] = slot
				for k := 1; k < colspan; k++ {
					grid[rr][col+k] = &tableSlot{}
				}
			}
			col += colspan
		}
	}

	lines := make([]string, 0, len(grid))
	for _, row := range grid {
		cells := make([]string, 0, len(row))
		for _, slot := range row {
			if slot == nil || slot.text == "" {
				continue
			}
			cells = append(cells, slot.text)
		}
		if len(cells) == 0 {
			continue
		}
		lines = append(lines, strings.Join(cells, " | "))
	}
	return lines
}

// growRow extends row so it has at least width slots.
func growRow(row []*tableSlot, width int) []*tableSlot {
	for len(row) < width {
		row = append(row, nil)
	}
	return row
}

// attrInt reads a numeric attribute, returning 0 when it is missing or invalid.
func attrInt(n *html.Node, name string) int {
	v, err := strconv.Atoi(strings.TrimSpace(attr(n, name)))
	if err != nil {
		return 0
	}
	return v
}

// clampSpan keeps a rowspan/colspan inside sane bounds.
func clampSpan(v int) int {
	switch {
	case v < 1:
		return 1
	case v > 100:
		return 100
	}
	return v
}

// renderFragment renders one node subtree in isolation (a table cell). A cell
// has to stay on one line, otherwise the flattened row would break apart.
func renderFragment(n *html.Node, baseURL string) string {
	r := htmlRenderer{base: baseURL}
	r.children(n)
	text := strings.ReplaceAll(strings.TrimSpace(string(r.b)), " \n", "\n")
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.Join(strings.Fields(text), " ")
}

func tableRows(n *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node)
	walk = func(parent *html.Node) {
		for c := parent.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch strings.ToLower(c.Data) {
			case "tr":
				rows = append(rows, c)
			case "thead", "tbody", "tfoot":
				walk(c)
			}
		}
	}
	walk(n)
	return rows
}

func rowCells(tr *html.Node) []*html.Node {
	var cells []*html.Node
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch strings.ToLower(c.Data) {
		case "td", "th":
			cells = append(cells, c)
		}
	}
	return cells
}

func (r *htmlRenderer) text(s string) {
	if s == "" {
		return
	}
	if r.pre > 0 {
		r.b = append(r.b, escapeText(s)...)
		return
	}

	lead := isHTMLSpace(s[0])
	trail := isHTMLSpace(s[len(s)-1])
	core := collapseSpaces(strings.TrimSpace(s))
	if core == "" {
		r.space()
		return
	}
	if lead {
		r.space()
	}
	r.b = append(r.b, escapeText(core)...)
	if trail {
		r.space()
	}
}

// space keeps a single separating space, never one at the start of a line.
func (r *htmlRenderer) space() {
	switch r.last() {
	case 0, ' ', '\n':
		return
	}
	r.b = append(r.b, ' ')
}

// newline starts a new line, dropping trailing spaces and empty lines.
func (r *htmlRenderer) newline() {
	for len(r.b) > 0 && r.b[len(r.b)-1] == ' ' {
		r.b = r.b[:len(r.b)-1]
	}
	if len(r.b) == 0 || r.b[len(r.b)-1] == '\n' {
		return
	}
	r.b = append(r.b, '\n')
}

func (r *htmlRenderer) last() byte {
	if len(r.b) == 0 {
		return 0
	}
	return r.b[len(r.b)-1]
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

// ResolveURL turns a link reference into an absolute URL: absolute references
// are returned unchanged, relative ones are joined to base. An empty base
// leaves a relative reference unresolved (""), so the caller can drop it.
func ResolveURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if u, err := url.Parse(ref); err == nil && u.Scheme != "" {
		return ref
	}

	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	switch {
	case base == "":
		return ""
	case strings.HasPrefix(ref, "//"):
		return "https:" + ref
	case strings.HasPrefix(ref, "#"):
		return base + ref
	case strings.HasPrefix(ref, "/"):
		return base + ref
	default:
		return base + "/" + ref
	}
}

// isSafeHref reports whether a link target can be rendered in a message.
func isSafeHref(href string) bool {
	if href == "" {
		return false
	}
	u, err := url.Parse(href)
	if err != nil || u.Scheme == "" {
		return false // relative or malformed: not clickable in Telegram
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto", "tg":
		return true
	default:
		return false
	}
}

func isHTMLSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	}
	return false
}

// collapseSpaces folds every run of HTML whitespace into a single space.
func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f':
			space = true
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

var attrEscaper = strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")

// escapeText makes text safe inside a Telegram HTML message.
func escapeText(s string) string { return textEscaper.Replace(s) }

// escapeAttr makes a value safe inside a double-quoted HTML attribute.
func escapeAttr(s string) string { return attrEscaper.Replace(s) }
