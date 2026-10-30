package handler

import (
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// maxMessageUnits is the per-message budget in UTF-16 code units. Telegram
// rejects messages longer than 4096 units after entity parsing, so the limit
// applies to the visible text: HTML tags do not count, which is why the
// splitter only charges text atoms against the budget.
const maxMessageUnits = 4090

// chunkSendInterval is the pause between two chunks of the same project, kept
// to stay well inside Telegram's per-chat rate limit.
const chunkSendInterval = 500 * time.Millisecond

// htmlAtom is one unit of the message: either a single rune of text (units > 0)
// or an HTML tag (units == 0).
type htmlAtom struct {
	text        string
	units       int
	name        string
	open        bool
	selfClosing bool
}

// splitMessage splits an HTML message into chunks that each fit in one
// Telegram message. Tags are never cut in half: a chunk closes every element
// still open at its end, and the next chunk reopens them, so both halves stay
// parseable.
func splitMessage(msg string) []string {
	atoms := parseHTMLAtoms(msg)
	if len(atoms) == 0 {
		return nil
	}

	st := splitState{}
	var chunks []string
	start := 0

	for i, a := range atoms {
		if a.units > 0 && st.units+a.units > maxMessageUnits {
			cut, cutStack := st.chooseCut(i, start)
			if chunk := renderChunk(atoms[start:cut], cutStack); chunk != "" {
				chunks = append(chunks, chunk)
			}
			st.reset(cutStack)
			// Replay the atoms the preferred cut skipped over.
			for j := cut; j < i; j++ {
				st.apply(atoms[j], j)
			}
			start = cut
		}
		st.apply(a, i)
	}

	if start < len(atoms) {
		if chunk := renderChunk(atoms[start:], st.stack); chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

// splitState tracks the open elements and the length of the chunk being built.
type splitState struct {
	stack []htmlAtom
	units int
	marks []splitMark
}

// splitMark records a preferred cut point (a newline) together with the open
// elements at that position.
type splitMark struct {
	atom  int
	stack []htmlAtom
}

func (s *splitState) reset(stack []htmlAtom) {
	s.stack = stack
	s.units = 0
	s.marks = s.marks[:0]
}

// apply folds one atom into the current chunk state.
func (s *splitState) apply(a htmlAtom, index int) {
	switch {
	case a.units > 0:
		s.units += a.units
		if a.text == "\n" {
			s.marks = append(s.marks, splitMark{atom: index + 1, stack: slices.Clone(s.stack)})
		}
	case a.open && !a.selfClosing:
		s.stack = append(s.stack, a)
	case !a.open:
		if n := len(s.stack); n > 0 && s.stack[n-1].name == a.name {
			s.stack = s.stack[:n-1]
		}
	}
}

// chooseCut picks where to break before atom i: the last newline when it is in
// the second half of the chunk, otherwise the atom itself.
func (s *splitState) chooseCut(i, start int) (int, []htmlAtom) {
	if n := len(s.marks); n > 0 {
		m := s.marks[n-1]
		if m.atom > start && m.atom-start > (i-start)/2 {
			return m.atom, m.stack
		}
	}
	return i, slices.Clone(s.stack)
}

// renderChunk joins the atoms into a chunk, closing the elements that are
// still open at that position so the chunk on its own is well-formed.
func renderChunk(atoms, stack []htmlAtom) string {
	var sb strings.Builder
	for _, a := range atoms {
		sb.WriteString(a.text)
	}
	for _, a := range slices.Backward(stack) {
		sb.WriteString("</")
		sb.WriteString(a.name)
		sb.WriteString(">")
	}
	return strings.TrimSpace(sb.String())
}

// parseHTMLAtoms tokenises the message into runes and tags.
func parseHTMLAtoms(msg string) []htmlAtom {
	atoms := make([]htmlAtom, 0, len(msg))
	for i := 0; i < len(msg); {
		if msg[i] == '<' {
			if end := strings.IndexByte(msg[i:], '>'); end > 0 {
				raw := msg[i : i+end+1]
				if name, open, selfClosing, ok := parseTag(raw); ok {
					atoms = append(atoms, htmlAtom{text: raw, name: name, open: open, selfClosing: selfClosing})
					i += len(raw)
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(msg[i:])
		atoms = append(atoms, htmlAtom{text: msg[i : i+size], units: utf16.RuneLen(r)})
		i += size
	}
	return atoms
}

// parseTag reports the name and direction of an HTML tag. Comments and
// unknown markup report ok == false and are then treated as text.
func parseTag(raw string) (name string, open, selfClosing, ok bool) {
	body := strings.TrimSuffix(strings.TrimPrefix(raw, "<"), ">")
	body = strings.TrimSpace(body)
	if body == "" || strings.HasPrefix(body, "!") || strings.HasPrefix(body, "?") {
		return "", false, false, false
	}
	if rest, found := strings.CutPrefix(body, "/"); found {
		name = strings.ToLower(strings.TrimSpace(rest))
		return name, false, false, name != ""
	}
	body, selfClosing = strings.CutSuffix(body, "/")
	body = strings.TrimSpace(body)
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return "", false, false, false
	}
	name = strings.ToLower(fields[0])
	return name, true, selfClosing, name != ""
}

// maxCaptionUnits is the visible-text budget of one media caption. Telegram
// allows 1024 UTF-16 units; the margin absorbs entity parsing and the ellipsis
// appended when the body is cut.
const maxCaptionUnits = 1000

// visibleUnits returns the UTF-16 units Telegram charges for msg, i.e. the text
// without the markup.
func visibleUnits(msg string) int {
	n := 0
	for _, a := range parseHTMLAtoms(msg) {
		n += a.units
	}
	return n
}

// truncateHTML cuts msg to at most budget visible units and closes every element
// still open at the cut, so the fragment stays parseable on its own.
func truncateHTML(msg string, budget int) string {
	atoms := parseHTMLAtoms(msg)
	st := splitState{}

	cut := len(atoms)
	for i, a := range atoms {
		if a.units > 0 && st.units+a.units > budget {
			cut = i
			break
		}
		st.apply(a, i)
	}
	return strings.TrimSpace(renderChunk(atoms[:cut], st.stack))
}

// utf16Len returns the length of s in UTF-16 code units.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
