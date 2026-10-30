package handler

import (
	"strings"
	"testing"
)

// TestAttachmentCaptionKeepsShortBody covers the common case: the whole body is
// the caption of the single attachment message.
func TestAttachmentCaptionKeepsShortBody(t *testing.T) {
	body := "正文第一段\n<b>加粗</b>"
	if got := attachmentCaption([]string{body}); got != body {
		t.Errorf("attachmentCaption() = %q, want the body unchanged", got)
	}
	if got := attachmentCaption(nil); got != "" {
		t.Errorf("attachmentCaption(nil) = %q, want an empty caption", got)
	}
}

// TestAttachmentCaptionUsesFirstChunkOnly keeps a multi-chunk body from being
// folded into the caption.
func TestAttachmentCaptionUsesFirstChunkOnly(t *testing.T) {
	if got := attachmentCaption([]string{"第一段", "第二段"}); got != "第一段" {
		t.Errorf("attachmentCaption() = %q, want only the first chunk", got)
	}
}

// TestAttachmentCaptionTruncatesLongBody keeps the caption inside Telegram's
// limit and balanced at the cut.
func TestAttachmentCaptionTruncatesLongBody(t *testing.T) {
	body := "<b>" + strings.Repeat("长", maxCaptionUnits*2) + "</b>"
	got := attachmentCaption([]string{body})

	if !strings.HasSuffix(got, "…") {
		t.Errorf("caption does not end with an ellipsis (len %d)", len(got))
	}
	if units := visibleUnits(got); units > maxCaptionUnits+1 {
		t.Errorf("caption charges %d units, want at most %d", units, maxCaptionUnits+1)
	}
	if problems := tagProblems(got); len(problems) > 0 {
		t.Errorf("caption markup is unbalanced: %v", problems)
	}
}
