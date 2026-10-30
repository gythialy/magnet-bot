package utils

import (
	"encoding/json"
	"regexp"
)

// TenderCodeRegex matches tender codes such as "2023-JQ01-W1295".
var TenderCodeRegex = regexp.MustCompile(`\d{4}-[A-Z0-9]+-[A-Z0-9]+`)

// ToString renders v as indented JSON. It returns an empty string when v
// cannot be encoded.
func ToString(v any) string {
	b, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return ""
	}
	return string(b)
}
