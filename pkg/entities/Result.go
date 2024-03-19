package entities

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	tenderTemplate = `【项目号】[{{.Title}}]({{.Pageurl}})  
	{{.Content}}
`
	keywordTemplate = `【关键字: {{.Keyword}}】[{{.Title}}]({{.Pageurl}})   
	{{.Content}}`
)

var (
	TenderRender = template.Must(template.New("tender_template").Funcs(template.FuncMap{
		"html": func(s string) string {
			return s
		},
	}).Parse(tenderTemplate))

	keywordRender = template.Must(template.New("keyword_template").Funcs(template.FuncMap{
		"html": func(s string) string {
			return s
		},
	}).Parse(keywordTemplate))
)

type Result struct {
	NoticeTime     string `json:"noticeTime,omitempty"`
	OpenTenderCode string `json:"openTenderCode,omitempty"`
	Title          string `json:"title,omitempty"`
	Content        string `json:"content,omitempty"`
	Pageurl        string `json:"pageurl,omitempty"`
	Keyword        string `json:"keyword,omitempty"`
}

type Results struct {
	Data           []*Result
	TenderResults  []*Result
	KeywordResults []*Result
}

func NewResults(data []*Result) *Results {
	return &Results{
		Data:           data,
		TenderResults:  make([]*Result, 0),
		KeywordResults: make([]*Result, 0),
	}
}

func (r *Results) Filter(keywords, tenderCodes []string) {
	// patterns := regexp.MustCompile(strings.Join(keywords, "|"))
	// convert tenderCodes to map
	m := make(map[string]int)
	for i, code := range tenderCodes {
		m[code] = i
	}
	for _, v := range r.Data {
		if _, ok := m[v.OpenTenderCode]; ok {
			r.TenderResults = append(r.TenderResults, v)
			break
		}
		for _, keyword := range keywords {
			var matched []string
			k := strings.TrimSpace(keyword)
			if strings.Contains(v.Title, k) {
				matched = append(matched, k)
			}
			if len(matched) > 0 {
				v.Keyword = strings.Join(matched, ", ")
				r.KeywordResults = append(r.KeywordResults, v)
			}
		}
	}
}

func (r *Results) ToMarkdown() map[string]Markdown {
	result := make(map[string]Markdown)

	for _, v := range r.TenderResults {
		var buf bytes.Buffer
		if err := TenderRender.Execute(&buf, v); err == nil {
			result[v.Title] = Markdown{Content: buf.String(), Result: v}
		} else {
			log.Err(err)
		}
	}

	for _, v := range r.KeywordResults {
		var buf bytes.Buffer
		if err := keywordRender.Execute(&buf, v); err == nil {
			result[v.Title] = Markdown{Content: buf.String(), Result: v}
		} else {
			log.Err(err)
		}
	}

	return result
}

type Markdown struct {
	Content string
	Result  *Result
}
