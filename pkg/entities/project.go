package entities

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/rs/zerolog/log"
)

const (
	keywordTemplate = `【关键字: {{.Keyword}}】[{{.Title}}]({{.Pageurl}})   
	{{.Content}}`
)

var keywordRender = template.Must(template.New("keyword_template").Funcs(template.FuncMap{
	"html": func(s string) string {
		return s
	},
}).Parse(keywordTemplate))

type Project struct {
	NoticeTime     string `json:"noticeTime,omitempty"`
	OpenTenderCode string `json:"openTenderCode,omitempty"`
	Title          string `json:"title,omitempty"`
	ShortTitle     string `json:"-"`
	Content        string `json:"content,omitempty"`
	Pageurl        string `json:"pageurl,omitempty"`
	Keyword        string `json:"keyword,omitempty"`
}

type Projects struct {
	Projects        []*Project
	keywordProjects []*Project
	rules           []*rule.ComplexRule
}

func NewProjects(projects []*Project, rules []*rule.ComplexRule) *Projects {
	return &Projects{
		Projects:        projects,
		rules:           rules,
		keywordProjects: make([]*Project, 0),
	}
}

func (r *Projects) filter() {
	for _, v := range r.Projects {
		matched := make([]string, 0, len(r.rules))
		for _, rule := range r.rules {
			if rule.IsMatch(v.ShortTitle) || rule.IsMatch(v.OpenTenderCode) {
				matched = append(matched, rule.ToString())
			}
		}
		if len(matched) > 0 {
			v.Keyword = strings.Join(matched, "| ")
			r.keywordProjects = append(r.keywordProjects, v)
		}
	}
}

func (r *Projects) ToMarkdown() map[string]Markdown {
	r.filter()
	result := make(map[string]Markdown)

	for _, project := range r.keywordProjects {
		var buf bytes.Buffer
		if err := keywordRender.Execute(&buf, project); err == nil {
			result[project.Title] = Markdown{Content: buf.String(), Project: project}
		} else {
			log.Err(err)
		}
	}

	return result
}

type Markdown struct {
	Content string
	Project *Project
}
