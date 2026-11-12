package utils

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// The Mini App renders a notice's full text inside its own document, so the
// markup has to be safe to insert there and independent of the notice site's
// stylesheets. This file is the only place that produces that markup: fetch and
// cache paths both pass through it.
//
// The policy is a whitelist, not a blacklist: an element that is not listed is
// unwrapped (its text kept) and an attribute that is not listed is dropped.
// script/style/iframe and friends are removed together with their content,
// because their text is not notice prose. That ordering matters — unwrapping a
// <script> would print its source into the page.
const (
	// maxNoticeHTMLBytes bounds one sanitized body. Notice pages are a few tens
	// of kilobytes; anything past this is a table dump that no phone should
	// render, and the client falls back to opening the original page.
	maxNoticeHTMLBytes = 1 << 20
)

var (
	// ErrNoticeBodyMissing reports a page that carries no recognisable body
	// container. It is a fetch-side outcome, not a render failure.
	ErrNoticeBodyMissing = errors.New("notice body not found")

	// ErrNoticeBodyTooLarge reports a sanitized body past maxNoticeHTMLBytes.
	ErrNoticeBodyTooLarge = errors.New("notice body is too large")
)

// noticeDroppedTags are removed with their content.
var noticeDroppedTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"iframe": true, "frame": true, "frameset": true, "object": true,
	"embed": true, "applet": true, "svg": true, "math": true, "canvas": true,
	"video": true, "audio": true, "source": true, "track": true,
	"form": true, "input": true, "button": true, "select": true,
	"option": true, "optgroup": true, "textarea": true, "label": true,
	"head": true, "meta": true, "link": true, "base": true, "title": true,
}

// noticeAllowedTags is the structural vocabulary the app's stylesheet targets:
// printed-column prose, tables, images and links.
var noticeAllowedTags = map[string]bool{
	"p": true, "div": true, "span": true, "br": true, "hr": true, "wbr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"blockquote": true, "pre": true, "code": true,
	"b": true, "strong": true, "i": true, "em": true, "u": true, "s": true,
	"strike": true, "del": true, "ins": true, "sub": true, "sup": true,
	"small": true, "big": true, "mark": true, "abbr": true, "cite": true,
	"table": true, "caption": true, "colgroup": true, "col": true,
	"thead": true, "tbody": true, "tfoot": true, "tr": true, "td": true,
	"th": true, "a": true, "img": true, "figure": true, "figcaption": true,
	"section": true, "article": true, "header": true, "footer": true,
	"main": true, "aside": true, "nav": true,
}

// noticeVoidTags never get a closing tag.
var noticeVoidTags = map[string]bool{
	"br": true, "hr": true, "wbr": true, "img": true, "col": true,
}

// noticeBodySelectors is the container search order for a full notice page.
// The publishing system wraps the body in div.pdfcontent; the rest cover
// neighbouring layouts of the same CMS. body is the last resort, and a page
// where even that is empty is reported as missing rather than served blank.
var noticeBodySelectors = []string{
	"div.pdfcontent",
	"div.info-content",
	"div.article-content",
	"div.TRS_Editor",
	"div#zoom",
	"div.content",
	"div#content",
	"article",
	"main",
	"body",
}

// SanitizeNoticeFragment sanitizes a body fragment that is not a whole page
// (the crawler API returns the notice content on its own). Relative links are
// resolved against baseURL; an empty baseURL drops them.
func SanitizeNoticeFragment(fragment, baseURL string) (string, error) {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		// net/html is lenient; a parse error here means the input is not HTML
		// at all. The caller can still show the text, so this is an error only
		// in the sense that no safe markup could be produced.
		return "", err
	}

	body := findFirst(doc, "body")
	if body == nil {
		return "", ErrNoticeBodyMissing
	}

	return renderNoticeHTML(body, baseURL)
}

// ExtractNoticeBody sanitizes the body of a complete notice page: it locates
// the content container first, so the page's navigation, banner and footer
// never reach the app.
func ExtractNoticeBody(documentHTML, pageURL string) (string, error) {
	doc, err := html.Parse(strings.NewReader(documentHTML))
	if err != nil {
		return "", err
	}

	container := findNoticeContainer(doc)
	if container == nil {
		return "", ErrNoticeBodyMissing
	}

	return renderNoticeHTML(container, pageURL)
}

// findNoticeContainer walks noticeBodySelectors in order and returns the first
// match. Selector matching goes through goquery because the patterns mix
// classes, ids and bare elements.
func findNoticeContainer(doc *html.Node) *html.Node {
	if doc == nil {
		return nil
	}

	root := goquery.NewDocumentFromNode(doc)
	for _, selector := range noticeBodySelectors {
		if match := root.Find(selector).First(); match.Length() > 0 {
			return match.Get(0)
		}
	}
	return nil
}

// findFirst returns the first element named tag, or nil.
func findFirst(n *html.Node, tag string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if found != nil {
			return
		}
		if cur.Type == html.ElementNode && strings.EqualFold(cur.Data, tag) {
			found = cur
			return
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

// renderNoticeHTML sanitizes the children of root and enforces the size bound.
//
// An empty result is reported as missing content: a container that sanitizes
// away to nothing (an empty shell, a page that is only scripts) has nothing the
// app can show, and answering "" would render a blank sheet instead.
func renderNoticeHTML(root *html.Node, baseURL string) (string, error) {
	r := &noticeRenderer{base: baseURL}
	r.children(root)

	out := strings.TrimSpace(r.b.String())
	if out == "" {
		return "", ErrNoticeBodyMissing
	}
	if len(out) > maxNoticeHTMLBytes {
		return "", ErrNoticeBodyTooLarge
	}
	return out, nil
}

// noticeRenderer accumulates the sanitized markup.
type noticeRenderer struct {
	b    strings.Builder
	base string
}

func (r *noticeRenderer) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.node(c)
	}
}

func (r *noticeRenderer) node(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		r.b.WriteString(escapeText(n.Data))
	case html.ElementNode:
		r.element(n)
	}
}

func (r *noticeRenderer) element(n *html.Node) {
	name := strings.ToLower(n.Data)

	if noticeDroppedTags[name] {
		return
	}

	if !noticeAllowedTags[name] {
		// Unknown inline or block element: keep the prose, drop the wrapper.
		r.children(n)
		return
	}

	if name == "a" {
		r.link(n)
		return
	}
	if name == "img" {
		r.image(n)
		return
	}

	r.b.WriteString("<" + name + r.attributes(n, name) + ">")
	if noticeVoidTags[name] {
		return
	}
	r.children(n)
	r.b.WriteString("</" + name + ">")
}

// attributes renders the whitelisted attributes of one element. Everything else
// — style, class, id, on* handlers, data-* — is dropped: the app supplies its
// own styling, and an id could collide with the shell's.
func (r *noticeRenderer) attributes(n *html.Node, name string) string {
	var b strings.Builder

	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)

		switch name {
		case "td", "th":
			if key != "colspan" && key != "rowspan" {
				continue
			}
			if span, ok := clampNoticeSpan(attr.Val); ok {
				b.WriteString(" " + key + `="` + span + `"`)
			}
		case "ol":
			if key != "start" {
				continue
			}
			if start, ok := clampNoticeStart(attr.Val); ok {
				b.WriteString(` start="` + start + `"`)
			}
		}
	}

	return b.String()
}

func (r *noticeRenderer) link(n *html.Node) {
	href := ResolveURL(r.base, attr(n, "href"))
	if !isWebLink(href) {
		// Unusable target: keep the text so nothing is lost.
		r.children(n)
		return
	}

	r.b.WriteString(`<a href="` + escapeAttr(href) + `">`)
	r.children(n)
	r.b.WriteString("</a>")
}

func (r *noticeRenderer) image(n *html.Node) {
	src := ResolveURL(r.base, attr(n, "src"))
	if !isWebLink(src) {
		return
	}

	r.b.WriteString(`<img src="` + escapeAttr(src) + `"`)
	if alt := strings.TrimSpace(attr(n, "alt")); alt != "" {
		r.b.WriteString(` alt="` + escapeAttr(alt) + `"`)
	}
	if title := strings.TrimSpace(attr(n, "title")); title != "" {
		r.b.WriteString(` title="` + escapeAttr(title) + `"`)
	}
	// The app never loads notice images until they scroll into view, and never
	// leaks the reader's referrer (or cookie-bearing origin) to the notice host.
	r.b.WriteString(` loading="lazy" referrerpolicy="no-referrer">`)
}

// isWebLink accepts only links a phone browser may open. javascript:, data: and
// protocol-relative leftovers are rejected.
func isWebLink(href string) bool {
	if href == "" {
		return false
	}
	u, err := url.Parse(href)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

// clampNoticeSpan parses a colspan/rowspan. Values outside 1..100 are dropped
// rather than clamped: an absurd span is a layout bug, and guessing would
// distort the table.
func clampNoticeSpan(raw string) (string, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 || value > 100 {
		return "", false
	}
	return strconv.Itoa(value), true
}

// clampNoticeStart validates an ordered list's start attribute.
func clampNoticeStart(raw string) (string, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < -9999 || value > 9999 {
		return "", false
	}
	return strconv.Itoa(value), true
}
