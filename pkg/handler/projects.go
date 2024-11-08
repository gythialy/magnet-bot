package handler

import (
	"bytes"
	"html/template"
	"strings"
	"sync"
	"unicode"

	"github.com/gythialy/magnet/pkg/dal"

	"github.com/gythialy/magnet/pkg/rule"
)

const (
	keywordTemplate = `<b>【关键字: {{.Keyword}}】</b><a href="{{.Pageurl}}">{{.Title}}</a> @ {{.NoticeTime}}
{{ .Content | noescape }} `
	maxMessageLength = 4090
)

var keywordRender = template.Must(template.New("keyword_template").
	Funcs(template.FuncMap{
		"noescape": func(str string) template.HTML {
			return template.HTML(str)
		},
	}).
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

func (p *Project) ToMessage() string {
	var buf bytes.Buffer

	if err := keywordRender.Execute(&buf, p); err == nil {
		return buf.String()
	}
	return ""
}

func (p *Project) SplitMessage() ([]string, int) {
	message := p.ToMessage()
	var chunks []string

	for len(message) > 0 {
		if len(message) <= maxMessageLength {
			if len(message) > 0 {
				chunks = append(chunks, message)
			}
			break
		}

		runes := []rune(message)
		end := maxMessageLength
		if end > len(runes) {
			end = len(runes)
		}

		chunk := string(runes[:end])
		lastNewline := strings.LastIndex(chunk, "\n")
		if lastNewline > 0 {
			chunk = chunk[:lastNewline]
		}

		chunk = strings.TrimSpace(chunk)

		// Only add chunk if it contains visible characters
		if chunk != "" && strings.IndexFunc(chunk, func(r rune) bool {
			return !unicode.IsSpace(r) && unicode.IsPrint(r)
		}) >= 0 {
			chunks = append(chunks, chunk)
		}
		message = message[len(chunk):]
	}

	return chunks, len(chunks)
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

func (r *Projects) Filter() []*Project {
	logger := r.ctx.Logger
	for _, v := range r.Projects {
		logger.Debug().Msgf("process: %s,%s[%s]", v.ShortTitle, v.OpenTenderCode, v.NoticeTime)
		matched := make([]string, 0, len(r.rules))
		for _, cr := range r.rules {
			if cr.IsMatch(v.ShortTitle) || cr.IsMatch(v.OpenTenderCode) {
				matched = append(matched, cr.ToString())
				key := cr.Rule.ID
				if val, ok := r.counters.LoadOrStore(cr.Rule.ID, int32(1)); ok {
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
			r.ctx.Logger.Error().Stack().Err(err).Msg("update counter error")
		} else {
			r.ctx.Logger.Debug().Msgf("update counter: %d=>%d", keyId, counter)
		}
		return true
	})

	return r.keywordProjects
}
