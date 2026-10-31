package utils

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateNoticeURL(t *testing.T) {
	const allowed = "notices.example.com"

	tests := []struct {
		name    string
		raw     string
		host    string
		wantErr error
	}{
		{name: "https notice", raw: "https://notices.example.com/notice/1", host: allowed},
		{name: "http notice", raw: "http://notices.example.com/notice/1", host: allowed},
		{name: "uppercase host", raw: "https://NOTICES.Example.COM/notice/1", host: allowed},
		{name: "surrounding spaces", raw: "  https://notices.example.com/notice/1  ", host: allowed},
		{name: "path and query kept", raw: "https://notices.example.com/notice/1.html?from=list", host: allowed},

		{name: "empty url", raw: "", host: allowed, wantErr: ErrNoticeURLMalformed},
		{name: "relative url", raw: "notices.example.com/notice/1", host: allowed, wantErr: ErrNoticeURLMalformed},
		{name: "non http scheme", raw: "ftp://notices.example.com/notice/1", host: allowed, wantErr: ErrNoticeURLMalformed},
		{name: "unparseable url", raw: "://not-a-url", host: allowed, wantErr: ErrNoticeURLMalformed},
		{
			name:    "credentials in url",
			raw:     "https://user:pass@notices.example.com/notice/1",
			host:    allowed,
			wantErr: ErrNoticeURLMalformed,
		},

		{name: "other host", raw: "https://evil.example.com/notice/1", host: allowed, wantErr: ErrNoticeURLHost},
		{
			name:    "host as suffix",
			raw:     "https://notices.example.com.evil.example.com/notice/1",
			host:    allowed,
			wantErr: ErrNoticeURLHost,
		},
		{
			name:    "host as prefix",
			raw:     "https://evilnotices.example.com/notice/1",
			host:    allowed,
			wantErr: ErrNoticeURLHost,
		},
		{
			name:    "unexpected port",
			raw:     "https://notices.example.com:8443/notice/1",
			host:    allowed,
			wantErr: ErrNoticeURLHost,
		},
		{
			name:    "empty allow-list",
			raw:     "https://notices.example.com/notice/1",
			host:    "",
			wantErr: ErrNoticeURLHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ValidateNoticeURL(tt.raw, tt.host)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ValidateNoticeURL(%q) error = %v, want %v", tt.raw, err, tt.wantErr)
				}
				if parsed != nil {
					t.Fatalf("ValidateNoticeURL(%q) = %v, want nil on error", tt.raw, parsed)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateNoticeURL(%q) error = %v, want nil", tt.raw, err)
			}
			if got := parsed.Host; !strings.EqualFold(got, tt.host) {
				t.Fatalf("parsed host = %q, want %q", got, tt.host)
			}
		})
	}
}
