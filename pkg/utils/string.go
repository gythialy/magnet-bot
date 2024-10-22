package utils

import (
	"encoding/json"
	"strings"
)

func ToString(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return ""
	}
	return string(b)
}

// EscapeMarkdown
// In all other places characters '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'
// must be escaped with the preceding character '\'.
// In all other places characters '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'
// must be escaped with the preceding character '\'.
func EscapeMarkdown(s string) string {
	return strings.NewReplacer(
		"*", "",
		"#", "",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"（", "\\(",
		"）", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	).Replace(s)
}
