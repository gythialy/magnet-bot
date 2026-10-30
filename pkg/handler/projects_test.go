package handler

import (
	"sync"
	"testing"

	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/rule"
)

func testRules(t *testing.T, keywords ...string) []*rule.ComplexRule {
	t.Helper()

	rules := make([]*rule.ComplexRule, 0, len(keywords))
	for _, keyword := range keywords {
		rules = append(rules, rule.NewComplexRule(&model.Keyword{Keyword: keyword}))
	}
	return rules
}

// TestFilterReturnsCopies guards the regression where Filter wrote the match
// result back into the shared []*Project that every user of one run shares.
func TestFilterReturnsCopies(t *testing.T) {
	shared := []*Project{
		{Title: "t1", ShortTitle: "t1", Pageurl: "u1"},
		{Title: "t2", ShortTitle: "t2", Pageurl: "u2"},
	}

	matched := NewProjects(testBotContext("example.com"), shared, testRules(t, "t1")).Filter()
	if len(matched) != 1 {
		t.Fatalf("got %d matches, want 1", len(matched))
	}
	if matched[0].Pageurl != "u1" || matched[0].Keyword != "+t1" {
		t.Fatalf("unexpected match: %+v", matched[0])
	}
	if matched[0] == shared[0] {
		t.Fatal("Filter must return a copy, not the shared project")
	}
	if shared[0].Keyword != "" {
		t.Fatalf("Filter wrote the keyword back into the shared project: %q", shared[0].Keyword)
	}
}

// TestFilterConcurrentUsers reproduces the production fan-out: one shared
// project slice handed to several users processed concurrently. Run with
// -race, it fails if Filter writes into the shared structs again.
func TestFilterConcurrentUsers(t *testing.T) {
	shared := []*Project{
		{Title: "t1", ShortTitle: "t1", Pageurl: "u1"},
		{Title: "t2", ShortTitle: "t2", Pageurl: "u2"},
	}
	ctx := testBotContext("example.com")

	var (
		userA, userB []*Project
		wg           sync.WaitGroup
	)
	wg.Go(func() { userA = NewProjects(ctx, shared, testRules(t, "t1")).Filter() })
	wg.Go(func() { userB = NewProjects(ctx, shared, testRules(t, "t1 -zz")).Filter() })
	wg.Wait()

	if len(userA) != 1 || userA[0].Keyword != "+t1" {
		t.Fatalf("user A got %+v, want keyword %q", userA, "+t1")
	}
	if len(userB) != 1 || userB[0].Keyword != "+t1 -zz" {
		t.Fatalf("user B got %+v, want keyword %q", userB, "+t1 -zz")
	}
	for _, project := range shared {
		if project.Keyword != "" {
			t.Fatalf("shared project %s carries a user keyword: %q", project.Pageurl, project.Keyword)
		}
	}
}

// TestSplitMessageRendersAndSplits checks the project template and the splitter
// agree on the chunked output.
func TestSplitMessageRendersAndSplits(t *testing.T) {
	project := &Project{
		Title:      "t",
		ShortTitle: "t",
		Pageurl:    "https://example.com/x",
		Keyword:    "+k",
		Content:    "content",
	}
	chunks, total := project.SplitMessage()
	if total != 1 || len(chunks) != 1 {
		t.Fatalf("got %d chunks (total=%d), want 1", len(chunks), total)
	}
	if project.HasTenderCode {
		t.Fatal("HasTenderCode should stay false for a keyword without a tender code")
	}
	if chunks[0] == "" {
		t.Fatal("rendered chunk is empty")
	}
}
