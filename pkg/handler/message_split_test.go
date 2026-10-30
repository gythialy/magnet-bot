package handler

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return htmlTagPattern.ReplaceAllString(s, "") }

// tagProblems lists unclosed and stray tags in msg. It uses its own scanner so
// the splitter's parser is not trusted for the check.
func tagProblems(msg string) []string {
	var (
		stack    []string
		problems []string
	)
	for _, tag := range htmlTagPattern.FindAllString(msg, -1) {
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(tag, "<"), ">"))
		switch {
		case body == "", strings.HasPrefix(body, "!"), strings.HasPrefix(body, "?"):
		case strings.HasPrefix(body, "/"):
			name := strings.ToLower(strings.TrimSpace(body[1:]))
			n := len(stack)
			if n == 0 || stack[n-1] != name {
				problems = append(problems, "stray </"+name+">")
				continue
			}
			stack = stack[:n-1]
		case strings.HasSuffix(body, "/"):
		default:
			stack = append(stack, strings.ToLower(strings.Fields(body)[0]))
		}
	}
	for _, name := range stack {
		problems = append(problems, "unclosed <"+name+">")
	}
	return problems
}

func squash(s string) string {
	return strings.Join(strings.Fields(stripTags(s)), "")
}

// TestSplitMessageKeepsTagsBalanced is the regression guard for cutting a
// message in the middle of an <a>/<b> element, which Telegram rejects.
// TestTruncateHTMLClosesOpenTags keeps a cut fragment parseable: the element
// that was open at the cut has to be closed.
func TestTruncateHTMLClosesOpenTags(t *testing.T) {
	got := truncateHTML("<b>一二三四</b>", 2)
	if got != "<b>一二</b>" {
		t.Errorf("truncateHTML() = %q, want %q", got, "<b>一二</b>")
	}
	if problems := tagProblems(got); len(problems) > 0 {
		t.Errorf("truncated markup is unbalanced: %v", problems)
	}
}

// TestTruncateHTMLCountsUTF16Units guards emoji: a surrogate pair must never be
// cut in half.
func TestTruncateHTMLCountsUTF16Units(t *testing.T) {
	if got := truncateHTML("😀😀😀", 5); got != "😀😀" {
		t.Errorf("truncateHTML() = %q, want two emoji", got)
	}
	if got := truncateHTML("abc", 10); got != "abc" {
		t.Errorf("truncateHTML() = %q, want the body unchanged", got)
	}
}

// TestVisibleUnitsIgnoresMarkup keeps the caption budget aligned with what
// Telegram charges for a message.
func TestVisibleUnitsIgnoresMarkup(t *testing.T) {
	if got := visibleUnits(`<b>ab</b><a href="https://example.com">c</a>`); got != 3 {
		t.Errorf("visibleUnits() = %d, want 3", got)
	}
}

func TestSplitMessageKeepsTagsBalanced(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<a href="https://example.com/very/long/path">公告标题</a> @ 2024-01-01`)
	sb.WriteString("\n<b>[+关键词]</b>\n\n")
	// Tags inside the body make sure a cut that lands between two tags still
	// closes exactly the elements of its own chunk.
	for i := range 1500 {
		sb.WriteString("<b>第")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString("行</b>内容\n")
	}
	message := sb.String()

	chunks := splitMessage(message)
	if len(chunks) < 2 {
		t.Fatalf("expected the message to be split, got %d chunk(s)", len(chunks))
	}
	for i, chunk := range chunks {
		if units := utf16Len(stripTags(chunk)); units > maxMessageUnits {
			t.Errorf("chunk %d is %d UTF-16 units, limit is %d", i, units, maxMessageUnits)
		}
		if problems := tagProblems(chunk); len(problems) > 0 {
			t.Errorf("chunk %d has broken tags: %v", i, problems)
		}
	}

	if got, want := squash(strings.Join(chunks, "")), squash(message); got != want {
		t.Errorf("split changed the text: got %d runes, want %d", len(got), len(want))
	}
}

// TestSplitMessageCountsUTF16Units guards the unit confusion that let emoji
// messages exceed Telegram's limit.
func TestSplitMessageCountsUTF16Units(t *testing.T) {
	if got := utf16Len("😀"); got != 2 {
		t.Fatalf("utf16Len(emoji) = %d, want 2", got)
	}

	message := strings.Repeat("😀", 3000) // 6000 UTF-16 units
	chunks := splitMessage(message)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	for i, chunk := range chunks {
		if units := utf16Len(chunk); units > maxMessageUnits {
			t.Errorf("chunk %d is %d units, limit is %d", i, units, maxMessageUnits)
		}
	}
	if got := strings.Join(chunks, ""); got != message {
		t.Error("split lost or duplicated emoji")
	}
}

func TestSplitMessageSingleChunk(t *testing.T) {
	chunks := splitMessage("hello")
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("splitMessage(hello) = %v, want one chunk", chunks)
	}
	if chunks := splitMessage("   "); chunks != nil {
		t.Fatalf("splitMessage(blank) = %v, want nil", chunks)
	}
}
