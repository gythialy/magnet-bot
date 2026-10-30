package handler

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

// TestGotenbergFetchPDF runs against a real Gotenberg instance, e.g. the local
// OrbStack container started with:
//
//	docker run -d --name gotenberg -p 3000:3000 gotenberg/gotenberg:8
//	GOTENBERG_URL=http://localhost:3000 GOTENBERG_TEST_URL=<notice page> //	  go test ./pkg/handler -run Gotenberg
//
// The notice URL is supplied by the caller so no site-specific data lives in
// the repository.
func TestGotenbergFetchPDF(t *testing.T) {
	baseURL := os.Getenv("GOTENBERG_URL")
	if baseURL == "" {
		t.Skip("GOTENBERG_URL not set; skipping the Gotenberg integration test")
	}
	noticeURL := os.Getenv("GOTENBERG_TEST_URL")
	if noticeURL == "" {
		t.Skip("GOTENBERG_TEST_URL not set; skipping the Gotenberg integration test")
	}

	client, err := NewGotenbergClient(baseURL, "http://127.0.0.1:8080/pdf/", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pdf, err := client.FetchPDF(ctx, noticeURL)
	if err != nil {
		t.Fatalf("FetchPDF(%s): %v", noticeURL, err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("response is not a PDF: %q", pdf[:min(len(pdf), 16)])
	}
	if len(pdf) < 10<<10 {
		t.Fatalf("suspiciously small PDF: %d bytes", len(pdf))
	}
	t.Logf("converted %s into a %d bytes PDF", noticeURL, len(pdf))
}
