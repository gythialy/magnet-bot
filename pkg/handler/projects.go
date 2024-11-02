package handler

import (
	"bytes"
	"html/template"
	"strings"
	"sync"

	"github.com/gythialy/magnet/pkg/dal"

	"github.com/gythialy/magnet/pkg/rule"
)

const (
	keywordTemplate = `<b>【关键字: {{.Keyword}}】</b><a href="{{.Pageurl}}">{{.Title}}</a> @ {{.NoticeTime}}
{{ .Content }} `
)

var keywordRender = template.Must(template.New("keyword_template").
	//	Funcs(template.FuncMap{
	//	"cleanContent": cleanContent,
	//}).
	Parse(keywordTemplate))

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
	counters        *sync.Map
}

func NewProjects(ctx *BotContext, projects []*Project, rules []*rule.ComplexRule) *Projects {
	return &Projects{
		Projects:        projects,
		rules:           rules,
		keywordProjects: make([]*Project, 0),
		ctx:             ctx,
		counters:        &sync.Map{},
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
				key := cr.Rule.ID
				if val, ok := r.counters.LoadOrStore(cr.Rule.ID, int32(0)); ok {
					r.counters.Store(key, val.(int32)+1)
				}
			}
		}
		if len(matched) > 0 {
			v.Keyword = strings.Join(matched, "| ")
			r.keywordProjects = append(r.keywordProjects, v)
			logger.Debug().Msgf("matched by (%s)", v.Keyword)
		}
	}

	r.counters.Range(func(key, value interface{}) bool {
		keyId, _ := key.(int32)
		counter := value.(int32)
		if err := dal.Keyword.UpdateCounter(keyId, counter); err != nil {
			r.ctx.Logger.Error().Msgf("update counter error: %s", err.Error())
		} else {
			r.ctx.Logger.Debug().Msgf("update counter: %d=>%d", keyId, counter)
		}
		return true
	})
}

func (r *Projects) ToMessage() map[string]TelegramMessage {
	r.filter()
	result := make(map[string]TelegramMessage)

	for _, project := range r.keywordProjects {
		var buf bytes.Buffer
		// Remove '**' from the content
		project.Content = strings.ReplaceAll(project.Content, "**", "")

		if err := keywordRender.Execute(&buf, project); err == nil {
			result[project.Title] = TelegramMessage{Content: buf.String(), Project: project}
		} else {
			r.ctx.Logger.Error().Msg(err.Error())
		}
	}

	return result
}

func cleanContent(content string) string {
	lines := strings.Split(content, "\n")
	var merged []string
	inMergeBlock := false
	var currentBlock strings.Builder

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "(一)申领时间") {
			inMergeBlock = true
			currentBlock.WriteString(trimmedLine)
		} else if inMergeBlock && strings.HasPrefix(trimmedLine, "(二)") {
			merged = append(merged, strings.TrimRight(currentBlock.String(), " "))
			merged = append(merged, "") // Add an empty line before (二)
			merged = append(merged, line)
			inMergeBlock = false
			currentBlock.Reset()
		} else if inMergeBlock {
			currentBlock.WriteString(" " + trimmedLine)
		} else {
			merged = append(merged, line)
		}
	}

	if inMergeBlock {
		merged = append(merged, strings.TrimRight(currentBlock.String(), " "))
	}

	return strings.Join(merged, "\n")
}

type TelegramMessage struct {
	Content string
	Project *Project
}
