package utils

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	tableRegex        = regexp.MustCompile(`<table[^>]*>(.*?)</table>`)
	trRegex           = regexp.MustCompile(`<tr[^>]*>(.*?)</tr>`)
	tdRegex           = regexp.MustCompile(`<td[^>]*>(.*?)</td>|<th[^>]*>(.*?)</th>`)
	styleAttrRegex    = regexp.MustCompile(`\s+style="[^"]*"`)
	multiSpaceRegex   = regexp.MustCompile(`\s+`)
	multiNewlineRegex = regexp.MustCompile(`\n{2,}`) // 匹配3个或更多换行

	commentRegex  = regexp.MustCompile(`<!--[\s\S]*?-->`)
	styleTagRegex = regexp.MustCompile(`<style[^>]*>[\s\S]*?</style>`)
	// inlineStyleRegex = regexp.MustCompile(`<[^>]*style="[^"]*"[^>]*>`)
	h1OpenRegex  = regexp.MustCompile(`<h1[^>]*>`)
	h1CloseRegex = regexp.MustCompile(`</h1>`)
	brRegex      = regexp.MustCompile(`<br\s*/>`)
	htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
	// Add whitespace replacer as a package-level variable
	whitespaceReplacer = strings.NewReplacer(
		`\r\n`, "",
		"\r\n", "",
		`\t`, "",
		`\f`, "",
		`\n`, "",
		"\n", "",
	)

	// Add spaceRegex as a package-level variable
	// spaceRegex = regexp.MustCompile(`\s+`)
)

func UnescapeHTML(content string) string {
	content = html.UnescapeString(content)
	// 删除HTML注释
	content = commentRegex.ReplaceAllString(content, "")
	// 清理所有空白字符，包括换行符
	content = multiNewlineRegex.ReplaceAllString(content, "\n")

	return content
}

func SimplifyHTML(htmlContent string) string {
	content := UnescapeHTML(htmlContent)
	// 清理所有空白字符，包括换行符
	content = whitespaceReplacer.Replace(content)
	// 删除CSS和style
	content = styleTagRegex.ReplaceAllString(content, "")
	content = styleAttrRegex.ReplaceAllString(content, "")

	// 结构性元素转换为<br />
	// First replace existing <br /> patterns to avoid duplication
	content = strings.ReplaceAll(content, "</div><br />", "||DIV_BR||")
	content = strings.ReplaceAll(content, "</p><br />", "||P_BR||")
	// Add <br /> after block elements
	content = strings.ReplaceAll(content, "</div>", "</div><br />")
	content = strings.ReplaceAll(content, "</p>", "</p><br />")
	// Restore original patterns
	content = strings.ReplaceAll(content, "||DIV_BR||", "</div><br />")
	content = strings.ReplaceAll(content, "||P_BR||", "</p><br />")

	// Replace <h1> tags with <br />
	content = h1OpenRegex.ReplaceAllString(content, "<br />")
	content = h1CloseRegex.ReplaceAllString(content, "")

	// 处理表格（在删除其他标签之前）
	content = tableRegex.ReplaceAllStringFunc(content, func(tableContent string) string {
		var rows []string
		matches := trRegex.FindAllStringSubmatch(tableContent, -1)

		for _, match := range matches {
			rowContent := match[1]
			cells := tdRegex.FindAllStringSubmatch(rowContent, -1)

			var cellContents []string
			for _, cell := range cells {
				c := cell[1]
				if c == "" {
					c = cell[2]
				}

				// Clean cell content
				c = htmlTagRegex.ReplaceAllString(c, "")
				c = whitespaceReplacer.Replace(c)
				c = multiSpaceRegex.ReplaceAllString(c, "")
				c = strings.TrimSpace(c)

				if c != "" {
					cellContents = append(cellContents, c)
				}
			}

			if len(cellContents) > 0 {
				rows = append(rows, strings.Join(cellContents, ";"))
			}
		}

		return strings.Join(rows, "\n")
	})

	// Replace <br /> tags with newline
	content = brRegex.ReplaceAllString(content, "\n")

	// Remove all remaining HTML tags
	content = htmlTagRegex.ReplaceAllString(content, "")
	content = multiNewlineRegex.ReplaceAllString(content, "\n")

	// 最终清理
	return strings.TrimSpace(content)
}
