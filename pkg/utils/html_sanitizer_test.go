package utils

import (
	"strings"
	"testing"
)

// The fixtures here are synthetic: real notice pages are user data and must
// never be committed, so every case below describes the shape being guarded.

func TestSanitizeNoticeFragmentKeepsStructure(t *testing.T) {
	const fragment = `<div><p>第一段 <strong>加粗</strong></p>` +
		`<ul><li>甲</li><li>乙</li></ul></div>`

	got, err := SanitizeNoticeFragment(fragment, "https://notices.example.com")
	if err != nil {
		t.Fatalf("SanitizeNoticeFragment() error = %v", err)
	}

	for _, want := range []string{"<p>", "<strong>加粗</strong>", "<ul>", "<li>甲</li>"} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q: %s", want, got)
		}
	}
}

// TestSanitizeNoticeFragmentDropsActiveContent guards the whitelist: script,
// style and embedded frames must disappear together with their content, and
// inline handlers must not survive as attributes.
func TestSanitizeNoticeFragmentDropsActiveContent(t *testing.T) {
	const fragment = `<div>` +
		`<script>alert("x")</script>` +
		`<style>p{color:red}</style>` +
		`<iframe src="https://evil.example.com/"></iframe>` +
		`<p onclick="steal()" style="position:fixed" class="x" id="y">正文</p>` +
		`<a href="javascript:alert(1)">点我</a>` +
		`</div>`

	got, err := SanitizeNoticeFragment(fragment, "https://notices.example.com")
	if err != nil {
		t.Fatalf("SanitizeNoticeFragment() error = %v", err)
	}

	for _, unwanted := range []string{
		"script", "alert(", "style", "iframe", "onclick", "javascript:",
		"position:fixed", `class="x"`, `id="y"`,
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output still carries %q: %s", unwanted, got)
		}
	}
	if !strings.Contains(got, "正文") || !strings.Contains(got, "点我") {
		t.Errorf("prose was lost: %s", got)
	}
}

// TestSanitizeNoticeFragmentResolvesLinks pins the base URL handling: relative
// links become absolute, and without a base they degrade to their text.
func TestSanitizeNoticeFragmentResolvesLinks(t *testing.T) {
	const fragment = `<div><a href="/a.html">站内</a> <a href="https://other.example.com/b">外链</a>` +
		`<img src="pic.png" alt="示意" onerror="steal()"></div>`

	got, err := SanitizeNoticeFragment(fragment, "https://notices.example.com")
	if err != nil {
		t.Fatalf("SanitizeNoticeFragment() error = %v", err)
	}
	for _, want := range []string{
		`href="https://notices.example.com/a.html"`,
		`href="https://other.example.com/b"`,
		`src="https://notices.example.com/pic.png"`,
		`alt="示意"`,
		`loading="lazy"`,
		`referrerpolicy="no-referrer"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "onerror") {
		t.Errorf("event handler survived: %s", got)
	}

	got, err = SanitizeNoticeFragment(fragment, "")
	if err != nil {
		t.Fatalf("SanitizeNoticeFragment() error = %v", err)
	}
	if strings.Contains(got, "a.html") || strings.Contains(got, "pic.png") {
		t.Errorf("relative target survived without a base: %s", got)
	}
	if !strings.Contains(got, "站内") {
		t.Errorf("link text was lost: %s", got)
	}
}

// TestSanitizeNoticeFragmentKeepsTableSpans guards the table attributes the
// printed layout depends on, and drops absurd ones instead of clamping them.
func TestSanitizeNoticeFragmentKeepsTableSpans(t *testing.T) {
	const fragment = `<table><tr><td colspan="2">合并</td></tr>` +
		`<tr><td rowspan="0">零</td><td style="width:10px">普通</td></tr></table>`

	got, err := SanitizeNoticeFragment(fragment, "https://notices.example.com")
	if err != nil {
		t.Fatalf("SanitizeNoticeFragment() error = %v", err)
	}
	if !strings.Contains(got, `colspan="2"`) {
		t.Errorf("valid colspan was dropped: %s", got)
	}
	if strings.Contains(got, "rowspan") {
		t.Errorf("out-of-range rowspan survived: %s", got)
	}
	if strings.Contains(got, "width:10px") {
		t.Errorf("style attribute survived: %s", got)
	}
}

// TestExtractNoticeBodyKeepsOnlyTheContentContainer proves the whole-page path
// does not serve the site chrome: the banner and the footer are outside the
// container the CMS marks.
func TestExtractNoticeBodyKeepsOnlyTheContentContainer(t *testing.T) {
	const page = `<!DOCTYPE html><html><body>` +
		`<header><a href="/">站点首页</a></header>` +
		`<div class="pdfcontent"><h1>公告标题</h1><p>公告正文</p>` +
		`<table><caption>明细</caption><tr><th>项目</th></tr><tr><td>一</td></tr></table></div>` +
		`<footer>版权所有</footer></body></html>`

	got, err := ExtractNoticeBody(page, "https://notices.example.com")
	if err != nil {
		t.Fatalf("ExtractNoticeBody() error = %v", err)
	}

	for _, want := range []string{"公告标题", "公告正文", "<table>", "<caption>明细</caption>"} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q: %s", want, got)
		}
	}
	for _, unwanted := range []string{"站点首页", "版权所有"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("page chrome leaked into the body: %q in %s", unwanted, got)
		}
	}
}

// TestExtractNoticeBodyFallsBackToBody covers pages whose CMS wrapper is
// missing but whose body still carries prose.
func TestExtractNoticeBodyFallsBackToBody(t *testing.T) {
	got, err := ExtractNoticeBody(`<html><body><p>只有正文</p></body></html>`, "")
	if err != nil {
		t.Fatalf("ExtractNoticeBody() error = %v", err)
	}
	if !strings.Contains(got, "只有正文") {
		t.Errorf("body fallback lost the prose: %s", got)
	}
}

func TestExtractNoticeBodyReportsMissingContent(t *testing.T) {
	for name, page := range map[string]string{
		"empty html": "<html></html>",
		"only head":  "<html><head><title>t</title></head></html>",
		"only drop":  `<html><body><script>var x=1</script></body></html>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ExtractNoticeBody(page, ""); err == nil {
				t.Fatal("ExtractNoticeBody() error = nil, want ErrNoticeBodyMissing")
			}
		})
	}
}

// TestRenderNoticeHTMLBoundsSize keeps one pathological table from becoming a
// multi-megabyte response the phone has to parse.
func TestRenderNoticeHTMLBoundsSize(t *testing.T) {
	huge := "<div><p>" + strings.Repeat("长", maxNoticeHTMLBytes/3+16) + "</p></div>"

	if _, err := SanitizeNoticeFragment(huge, ""); err != ErrNoticeBodyTooLarge {
		t.Fatalf("SanitizeNoticeFragment() error = %v, want ErrNoticeBodyTooLarge", err)
	}
}
