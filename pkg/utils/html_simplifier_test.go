package utils

import (
	"fmt"
	"strings"
	"testing"
)

// sampleNotice is a synthetic notice that exercises the markup the real API
// returns: nested spans with inline styles, a table with merged cells, inline
// formatting, <br>, entities, a relative link and script/style noise. It
// deliberately carries no data from any real notice.
const sampleNotice = `<h2 style="text-align: center; font-family: 宋体; font-size: 18pt;"><span
    style="font-family: 宋体;">测试采购项目</span><span style="font-family: 宋体;">谈判公告</span></h2>
<p style="text-indent: 28pt;"><span style="font-family: 宋体; font-size: 14pt;"><span
    style="font-family: 宋体;">我部就以下项目进行国内</span><strong>竞争性谈判</strong><span
    style="font-family: 宋体;">采购，欢迎符合条件的供应商参加。</span></span></p>
<p style="margin: 0pt;">一、项目名称：<u><span style="font-family: 宋体;">车场信息化改造</span></u></p>
<p style="margin: 0pt;">二、项目编号：<u>2024-TEST-W0001</u></p>
<p style="margin: 0pt;">三、项目概况：</p>
<table border="1" style="border-collapse: collapse; width: 100%;">
  <tr style="height: 26pt;">
    <th style="text-align: center;">序号</th>
    <th colspan="2" style="text-align: center;">物资名称</th>
    <th style="text-align: center;">数量</th>
    <th style="text-align: center;">备注</th>
  </tr>
  <tr>
    <td rowspan="2">1</td>
    <td>车牌识别</td>
    <td>套</td>
    <td>2</td>
    <td>1.规格：18.9升<br />2.含安装调试</td>
  </tr>
  <tr>
    <td>管理服务器</td>
    <td>台</td>
    <td>1</td>
    <td>详见附件</td>
  </tr>
</table>
<p>四、说明：<em>详见采购文件</em>，<strong>不接受联合体投标</strong>。</p>
<ul>
  <li>申领时间：2026年01月01日至2026年01月07日</li>
  <li>申领方式：<a href="/site/notice/1.html">线上申领</a></li>
</ul>
<p>五、联系方式：联系人：张助理，电话：010-00000000</p>
<script>alert('x')</script>
<style>p { margin: 0; }</style>
<!-- 注释 -->
<p>符号测试：a &lt; b &amp; c &gt; d</p>
`

// sanitize is SimplifyHTML for the cases that do not care about link
// resolution or the complex-table flag.
func sanitize(content string) string {
	html, _ := SimplifyHTML(content, "")
	return html
}

// TestSimplifyHTMLResolvesRelativeLinks guards the base URL: notice bodies link
// to their own site with relative hrefs, which Telegram cannot open.
func TestSimplifyHTMLResolvesRelativeLinks(t *testing.T) {
	in := `<p><a href="/site/a.html">站内</a> <a href="b.html">相对</a> <a href="https://other.example.com/c">绝对</a></p>`

	got, _ := SimplifyHTML(in, "https://www.example.com")
	for _, want := range []string{
		`<a href="https://www.example.com/site/a.html">站内</a>`,
		`<a href="https://www.example.com/b.html">相对</a>`,
		`<a href="https://other.example.com/c">绝对</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q\n%s", want, got)
		}
	}

	// Without a base URL a relative link degrades to its text.
	got, _ = SimplifyHTML(in, "")
	if strings.Contains(got, "site/a.html") {
		t.Errorf("relative link survived without a base URL: %q", got)
	}
	if !strings.Contains(got, "站内") {
		t.Errorf("link text was lost: %q", got)
	}
}

// TestSimplifyHTMLComplexTablePlaceholder guards the hand-off to the PDF path:
// a table too wide or too tall to flatten is replaced by a placeholder and
// reported, instead of producing an unreadable text table.
func TestSimplifyHTMLComplexTablePlaceholder(t *testing.T) {
	var wide strings.Builder
	wide.WriteString("<table><tr>")
	for i := range complexTableColumns {
		fmt.Fprintf(&wide, "<th>h%d</th>", i)
	}
	wide.WriteString("</tr><tr>")
	for i := range complexTableColumns {
		fmt.Fprintf(&wide, "<td>c%d</td>", i)
	}
	wide.WriteString("</tr></table>")

	got, complex := SimplifyHTML(wide.String(), "")
	if !complex {
		t.Fatal("expected the wide table to be flagged as complex")
	}
	if !strings.Contains(got, complexTablePlaceholder) {
		t.Errorf("placeholder missing from %q", got)
	}
	if strings.Contains(got, "c0") {
		t.Errorf("complex table should not be flattened: %q", got)
	}

	// A small table still flattens.
	got, complex = SimplifyHTML("<table><tr><td>a</td><td>b</td></tr></table>", "")
	if complex {
		t.Error("small table must not be flagged as complex")
	}
	if got != "a | b" {
		t.Errorf("small table = %q, want %q", got, "a | b")
	}
}

func TestSimplifyHTMLProducesTelegramMarkup(t *testing.T) {
	out, complexTable := SimplifyHTML(sampleNotice, "https://www.example.com")
	if out == "" {
		t.Fatal("SimplifyHTML returned empty content")
	}
	if complexTable {
		t.Error("the sample table is small enough to flatten")
	}

	for _, tag := range tagsOf(out) {
		if !allowedTelegramTag(tag) {
			t.Errorf("output contains a tag Telegram cannot render: %q", tag)
		}
	}
	if problems := unbalancedTags(out); len(problems) > 0 {
		t.Errorf("output has unbalanced tags: %v", problems)
	}
	for _, unwanted := range []string{
		"style=", "<table", "<td", "<th", "<span", "<p>", "<div", "<h2", "<em>", "<strong", "<script", "<style", "alert(",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output still contains %q", unwanted)
		}
	}

	for _, want := range []string{
		"测试采购项目谈判公告",        // two adjacent spans join without a space
		"2024-TEST-W0001",   // <u> content survives
		"<u>", "<i>", "<b>", // formatting tags are mapped
		"<b>序号</b> | <b>物资名称</b> | <b>数量</b> | <b>备注</b>",   // header row, colspan cell once
		"1 | 车牌识别 | 套 | 2 | 1.规格：18.9升 2.含安装调试",             // <br> becomes a space
		"1 | 管理服务器 | 台 | 1 | 详见附件",                          // rowspan cell repeats
		`href="https://www.example.com/site/notice/1.html"`, // relative link resolved
		"a &lt; b &amp; c &gt; d",                           // entities decoded then re-escaped
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q\n---\n%s", want, out)
		}
	}

	// Telegram treats blank lines as paragraph breaks; the flattening must not
	// introduce them, otherwise every notice doubles in height.
	if strings.Contains(out, "\n\n") {
		t.Error("output contains blank lines")
	}
}

func TestSimplifyHTMLTable(t *testing.T) {
	got := sanitize("<table><tbody><tr><th>序号</th><th>名称</th></tr><tr><td>1</td><td>车场</td></tr></tbody></table>")
	// Header cells are bolded; every row stays on one line.
	if want := "<b>序号</b> | <b>名称</b>\n1 | 车场"; got != want {
		t.Fatalf("SimplifyHTML(table) = %q, want %q", got, want)
	}
}

// TestSimplifyHTMLTableMergedCells covers colspan/rowspan: the grid keeps the
// columns aligned, a colspan cell is written once, and a rowspan cell repeats
// on every row it covers so each line stands on its own.
func TestSimplifyHTMLTableMergedCells(t *testing.T) {
	in := "<table>" +
		`<tr><th>序号</th><th colspan="2">名称</th><th>数量</th></tr>` +
		`<tr><td rowspan="2">1</td><td>甲</td><td>乙</td><td>2</td></tr>` +
		"<tr><td>丙</td><td>丁</td><td>3</td></tr>" +
		"</table>"

	want := "<b>序号</b> | <b>名称</b> | <b>数量</b>\n" +
		"1 | 甲 | 乙 | 2\n" +
		"1 | 丙 | 丁 | 3"
	if got := sanitize(in); got != want {
		t.Fatalf("SimplifyHTML(merged table) = %q, want %q", got, want)
	}
}

// TestSimplifyHTMLTableSpanBounds guards absurd span values: they are clamped
// and a rowspan cannot reach past the last row.
func TestSimplifyHTMLTableSpanBounds(t *testing.T) {
	// A colspan wider than the row is reserved, not rendered as empty cells.
	in := `<table><tr><td colspan="3">x</td><td>y</td></tr></table>`
	if got, want := sanitize(in), "x | y"; got != want {
		t.Fatalf("sanitize() = %q, want %q", got, want)
	}

	// A rowspan that outlives the table must not invent extra lines.
	in = `<table><tr><td rowspan="3">A</td><td>1</td></tr><tr><td>2</td></tr></table>`
	if got, want := sanitize(in), "A | 1\nA | 2"; got != want {
		t.Fatalf("sanitize() = %q, want %q", got, want)
	}
}

func TestSimplifyHTMLDropsScriptAndStyle(t *testing.T) {
	in := "<p>保留</p><script>alert(1)</script><style>p{color:red}</style><!-- 注释 --><noscript>no</noscript>"
	if got := sanitize(in); got != "保留" {
		t.Fatalf("sanitize() = %q, want %q", got, "保留")
	}
}

func TestSimplifyHTMLEscapesText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"entities", "<p>a &lt; b &amp; c &gt; d</p>", "a &lt; b &amp; c &gt; d"},
		{"raw text", "5 < 6", "5 &lt; 6"},
		{"tag-like text", "<p>&lt;b&gt; 不是标签</p>", "&lt;b&gt; 不是标签"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitize(tt.in); got != tt.want {
				t.Fatalf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSimplifyHTMLLinks(t *testing.T) {
	in := `<p><a href="https://example.com/x?a=1&amp;b=2">链接</a> <a href="javascript:alert(1)">坏链接</a></p>`
	got := sanitize(in)

	if want := `<a href="https://example.com/x?a=1&amp;b=2">链接</a>`; !strings.Contains(got, want) {
		t.Errorf("safe link was not preserved: %q", got)
	}
	if strings.Contains(got, "javascript:") {
		t.Errorf("unsafe link target survived: %q", got)
	}
	if !strings.Contains(got, "坏链接") {
		t.Errorf("link text was lost: %q", got)
	}
}

func TestSimplifyHTMLBlocksAndLists(t *testing.T) {
	in := "<div><p>第一段</p><p>第二段<br>换行</p><ul><li>一</li><li>二</li></ul></div>"
	want := "第一段\n第二段\n换行\n• 一\n• 二"
	if got := sanitize(in); got != want {
		t.Fatalf("sanitize() = %q, want %q", got, want)
	}
}

func TestSimplifyHTMLInlineFormatting(t *testing.T) {
	in := "<p><strong>粗</strong><em>斜</em><u>下划线</u><del>删除</del><code>x=1</code></p>"
	want := "<b>粗</b><i>斜</i><u>下划线</u><s>删除</s><code>x=1</code>"
	if got := sanitize(in); got != want {
		t.Fatalf("sanitize() = %q, want %q", got, want)
	}
}

// tagsOf returns the tag names used in s.
func tagsOf(s string) []string {
	var tags []string
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			break
		}
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s[i:i+end+1], "<"), ">"))
		body = strings.TrimPrefix(body, "/")
		if body == "" {
			i += end
			continue
		}
		tags = append(tags, strings.ToLower(strings.Fields(body)[0]))
		i += end
	}
	return tags
}

func allowedTelegramTag(name string) bool {
	switch name {
	case "b", "i", "u", "s", "code", "pre", "blockquote", "a", "span", "tg-spoiler":
		return true
	}
	return false
}

// unbalancedTags lists elements left open or closed twice.
func unbalancedTags(s string) []string {
	var (
		stack    []string
		problems []string
	)
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			break
		}
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s[i:i+end+1], "<"), ">"))
		i += end
		if body == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(body, "/"); ok {
			name := strings.ToLower(strings.TrimSpace(rest))
			n := len(stack)
			if n == 0 || stack[n-1] != name {
				problems = append(problems, "stray </"+name+">")
				continue
			}
			stack = stack[:n-1]
			continue
		}
		if strings.HasSuffix(body, "/") {
			continue // self-closing
		}
		stack = append(stack, strings.ToLower(strings.Fields(body)[0]))
	}
	for _, name := range stack {
		problems = append(problems, "unclosed <"+name+">")
	}
	return problems
}
