package pkg

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
	ctx             *BotContext
}

func NewProjects(ctx *BotContext, projects []*Project, rules []*rule.ComplexRule) *Projects {
	return &Projects{
		Projects:        projects,
		rules:           rules,
		keywordProjects: make([]*Project, 0),
		ctx:             ctx,
	}
}

func (r *Projects) filter() {
	logger := r.ctx.Logger
	for _, v := range r.Projects {
		logger.Debug().Msgf("process: %s,%s[%s]", v.ShortTitle, v.OpenTenderCode, v.NoticeTime)
		matched := make([]string, 0, len(r.rules))
		for _, cr := range r.rules {
			if cr.IsMatch(v.ShortTitle) || cr.IsMatch(v.OpenTenderCode) {
				matched = append(matched, cr.ToString())
			}
		}
		if len(matched) > 0 {
			v.Keyword = strings.Join(matched, "| ")
			r.keywordProjects = append(r.keywordProjects, v)
			logger.Debug().Msgf("matched by (%s)", v.Keyword)
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
