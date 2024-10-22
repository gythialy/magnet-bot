package pkg

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/gythialy/magnet/pkg/rule"
)

const (
	keywordTemplate = `<b>【关键字: {{.Keyword}}】</b><a href="{{.Pageurl}}">{{.Title}}</a>
{{.Content}}`
)

var keywordRender = template.Must(template.New("keyword_template").Funcs(template.FuncMap{
	"removeEmptyLines": removeEmptyLines,
}).Parse(keywordTemplate))

func removeEmptyLines(s string) string {
	lines := strings.Split(s, "\n")
	var nonEmptyLines []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}
	return strings.Join(nonEmptyLines, "\n")
}

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

func (r *Projects) ToMarkdown() map[string]TelegramMessage {
	r.filter()
	result := make(map[string]TelegramMessage)

	for _, project := range r.keywordProjects {
		var buf bytes.Buffer
		if err := keywordRender.Execute(&buf, project); err == nil {
			c := buf.String()
			result[project.Title] = TelegramMessage{Content: c, Project: project}
		} else {
			r.ctx.Logger.Error().Msg(err.Error())
		}
	}

	return result
}

type TelegramMessage struct {
	Content string
	Project *Project
}
