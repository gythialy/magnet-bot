package handler

import (
	"testing"

	"github.com/gythialy/magnet/pkg/constant"
)

// TestParseCallbackData guards the panic that blind indexing of the split
// callback data used to cause.
func TestParseCallbackData(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		prefix  string
		page    int
		term    string
		wantErr bool
	}{
		{name: "search", data: "search:2:foo", prefix: "search", page: 2, term: "foo"},
		{name: "empty term", data: "today:1:", prefix: "today", page: 1},
		{name: "term with colon", data: "search:3:a:b", prefix: "search", page: 3, term: "a:b"},
		{name: "missing term", data: "search:1", wantErr: true},
		{name: "no colon", data: "search", wantErr: true},
		{name: "empty", data: "", wantErr: true},
		{name: "non numeric page", data: "search:x:foo", wantErr: true},
		{name: "zero page", data: "search:0:foo", wantErr: true},
		{name: "negative page", data: "search:-1:foo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, page, term, err := parseCallbackData(tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseCallbackData(%q) error = %v, wantErr %v", tt.data, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if prefix != tt.prefix || page != tt.page || term != tt.term {
				t.Fatalf("parseCallbackData(%q) = (%q, %d, %q), want (%q, %d, %q)",
					tt.data, prefix, page, term, tt.prefix, tt.page, tt.term)
			}
		})
	}
}

func TestPageKeyboard(t *testing.T) {
	if kb := pageKeyboard(page{number: 1, totalPages: 1}, constant.SearchCallback, "x"); kb != nil {
		t.Fatalf("expected no keyboard for a single page, got %+v", kb)
	}

	kb := pageKeyboard(page{number: 2, totalPages: 3}, constant.SearchCallback, "foo")
	if kb == nil || len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected one keyboard row, got %+v", kb)
	}
	row := kb.InlineKeyboard[0]
	if len(row) != 2 {
		t.Fatalf("expected previous and next buttons, got %d", len(row))
	}
	if row[0].CallbackData != "search:1:foo" || row[1].CallbackData != "search:3:foo" {
		t.Fatalf("unexpected callback data: %q, %q", row[0].CallbackData, row[1].CallbackData)
	}
}

func TestRenderPage(t *testing.T) {
	got := renderPage([]string{"a", "b"}, 2, 2, func(s string) string { return s })
	if want := "3. a\n4. b\n"; got != want {
		t.Fatalf("renderPage() = %q, want %q", got, want)
	}
}
