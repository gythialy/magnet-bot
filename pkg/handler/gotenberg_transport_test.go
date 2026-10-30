package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGotenbergFetchPDFReportsTransportCause guards the diagnosis path: the
// Gotenberg client library replaces every transport failure with a fixed
// "request sending failed", so FetchPDF has to recover the real cause itself.
// A container that refuses connections is exactly what produced that opaque
// message in production.
func TestGotenbergFetchPDFReportsTransportCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server does not support hijacking")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close() // drop the connection without answering
	}))
	defer server.Close()

	client, err := NewGotenbergClient(server.URL, "http://127.0.0.1:1/pdf/", "")
	if err != nil {
		t.Fatal(err)
	}

	const noticeURL = "https://example.com/notice/1"
	if _, err := client.FetchPDF(context.Background(), noticeURL); err == nil {
		t.Fatal("FetchPDF() = nil, want a transport error")
	} else {
		message := err.Error()
		if strings.Contains(message, "request sending failed") {
			t.Fatalf("FetchPDF() kept the opaque library error: %v", err)
		}
		for _, want := range []string{noticeURL, "failed after"} {
			if !strings.Contains(message, want) {
				t.Errorf("error %q does not mention %q", message, want)
			}
		}
	}
}

// TestGotenbergFetchPDFReportsStatusAndBody keeps the server side diagnostic:
// a rejected conversion must say which status and why.
func TestGotenbergFetchPDFReportsStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Chromium conversion timed out", http.StatusGatewayTimeout)
	}))
	defer server.Close()

	client, err := NewGotenbergClient(server.URL, "http://127.0.0.1:1/pdf/", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.FetchPDF(context.Background(), "https://example.com/notice/1")
	if err == nil {
		t.Fatal("FetchPDF() = nil, want a status error")
	}
	message := err.Error()
	for _, want := range []string{"status 504", "Chromium conversion timed out"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not mention %q", message, want)
		}
	}
}

// TestGotenbergFetchPDFReturnsBytes is the success path of the same client.
func TestGotenbergFetchPDFReturnsBytes(t *testing.T) {
	const pdf = "%PDF-1.7 synthetic notice"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte(pdf))
	}))
	defer server.Close()

	client, err := NewGotenbergClient(server.URL, "http://127.0.0.1:1/pdf/", "")
	if err != nil {
		t.Fatal(err)
	}

	data, err := client.FetchPDF(context.Background(), "https://example.com/notice/1")
	if err != nil {
		t.Fatalf("FetchPDF() error = %v", err)
	}
	if string(data) != pdf {
		t.Fatalf("FetchPDF() = %q, want %q", data, pdf)
	}
}
