package utils

import (
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
)

type TablePlugin struct{}

func NewTablePlugin() converter.Plugin {
	return &TablePlugin{}
}

func (p *TablePlugin) Name() string {
	return "table"
}

func (p *TablePlugin) Init(conv *converter.Converter) error {
	conv.Register.RendererFor("table", converter.TagTypeBlock, func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		var rows []string

		// Find tbody element
		var tbody *html.Node
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "tbody" {
				tbody = child
				break
			}
		}

		if tbody == nil {
			return converter.RenderTryNext
		}

		// Process table rows within tbody
		for tr := tbody.FirstChild; tr != nil; tr = tr.NextSibling {
			if tr.Type == html.ElementNode && tr.Data == "tr" {
				var cells []string

				// Process cells in the row
				for td := tr.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.Data == "td" || td.Data == "th") {
						text := getTextContent(td)
						// Clean cell content using the same logic as html_simplifier.go
						text = whitespaceReplacer.Replace(text)
						text = multiSpaceRegex.ReplaceAllString(text, " ")
						text = strings.TrimSpace(text)

						if text != "" {
							cells = append(cells, text)
						}
					}
				}

				if len(cells) > 0 {
					rows = append(rows, strings.Join(cells, ";"))
				}
			}
		}

		if len(rows) > 0 {
			_, _ = w.WriteString(strings.Join(rows, "\n"))
		}

		return converter.RenderSuccess
	}, converter.PriorityStandard)

	return nil
}

// getTextContent extracts text content from an HTML node
func getTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var result string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		result += getTextContent(c)
	}
	return result
}
