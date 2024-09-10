package entities

import (
	"bytes"
	"html/template"
	"strings"

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
	keywords        []string
}

func NewProjects(projects []*Project, keywords []string) *Projects {
	return &Projects{
		Projects:        projects,
		keywords:        keywords,
		keywordProjects: make([]*Project, 0),
	}
}

func (r *Projects) filter() {
	// patterns := regexp.MustCompile(strings.Join(keywords, "|"))
	for _, v := range r.Projects {
		for _, keyword := range r.keywords {
			var matched []string
			k := strings.TrimSpace(keyword)
			if strings.Contains(v.ShortTitle, k) || v.OpenTenderCode == k {
				matched = append(matched, k)
			}
			if len(matched) > 0 {
				v.Keyword = strings.Join(matched, ", ")
				r.keywordProjects = append(r.keywordProjects, v)
			}
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
