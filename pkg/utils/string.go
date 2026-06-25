package utils

import (
	"encoding/json"
	"regexp"
	"strings"
)

var TenderCodeRegex = regexp.MustCompile(`\d{4}-[A-Z0-9]+-[A-Z0-9]+`)

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

// headerValue := fmt.Sprintf("%s%s$$%d", baseURL, endpoint, time.Now().UnixMilli())
// encryptedHeader, err := encryptByRSA(headerValue)
// if err != nil {
// return fmt.Errorf("encryption error: %w", err)
// }
//
// // Create new request
// url := baseURL + endpoint + data
// req, err := http.NewRequest("GET", url, nil)
// if err != nil {
// return fmt.Errorf("failed to create request: %w", err)
// }
//
// // Set headers
// req.Header.Set("nsssjss", encryptedHeader)
// req.Header.Set("Content-Type", "application/json; charset=utf-8")
