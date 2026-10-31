package utils

import (
	"errors"
	"net/url"
	"strings"
)

var (
	// ErrNoticeURLMalformed reports a notice URL that cannot be parsed or that
	// is not an absolute http(s) URL without credentials.
	ErrNoticeURLMalformed = errors.New("notice url is malformed")

	// ErrNoticeURLHost reports a notice URL that points away from the
	// configured notice host.
	ErrNoticeURLHost = errors.New("notice url host is not allowed")
)

// ValidateNoticeURL parses raw and decides whether it may be fetched or handed
// to the conversion service.
//
// It is the only place that answers that question: the chat commands and the
// Mini App both call it, so the allow-list cannot drift between them. An empty
// allowedHost rejects every URL on purpose: the checked value ends up as the
// address of a server-side request, so the check must never fail open.
func ValidateNoticeURL(raw, allowedHost string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, ErrNoticeURLMalformed
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrNoticeURLMalformed
	}
	// Credentials would be forwarded to the notice server and never belong in
	// a notice link.
	if parsed.User != nil {
		return nil, ErrNoticeURLMalformed
	}
	if parsed.Host == "" {
		return nil, ErrNoticeURLMalformed
	}
	if allowedHost == "" || !strings.EqualFold(parsed.Host, allowedHost) {
		return nil, ErrNoticeURLHost
	}
	return parsed, nil
}
