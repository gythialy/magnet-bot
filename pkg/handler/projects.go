package handler

import (
	"bytes"
	"html/template"
	"strings"
	"sync"
	"unicode"

	"github.com/gythialy/magnet/pkg/utils"

	"github.com/gythialy/magnet/pkg/dal"

	"github.com/gythialy/magnet/pkg/rule"
)

const (
	keywordTemplate = `{{if .HasTenderCode}}🔥{{end}}<a href="{{.Pageurl}}">{{.Title}}</a> @ {{.NoticeTime}}
<b>[{{.Keyword}}]</b>

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
	HasTenderCode  bool   `json:"-"`
}

func (p *Project) ToMessage() string {
	var buf bytes.Buffer
	p.HasTenderCode = utils.TenderCodeRegex.MatchString(p.Keyword)
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

		// Only add a chunk if it contains visible characters
		if chunk != "" || strings.IndexFunc(chunk, func(r rune) bool {
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
				if cr.Rule != nil && cr.Rule.ID != nil {
					key := *cr.Rule.ID
					if val, ok := r.counters.Load(key); ok {
						counter := val.(int32)
						r.counters.Store(key, counter+1)
					} else {
						r.counters.Store(key, int32(1))
					}
				}
			}
		}
		if len(matched) > 0 {
			v.Keyword = strings.Join(matched, "| ")
			r.keywordProjects = append(r.keywordProjects, v)
			logger.Debug().Msgf("matched by (%s)", v.Keyword)
		}
	}

	// Update counters in database
	r.counters.Range(func(key, value interface{}) bool {
		keyId, ok := key.(int32)
		if !ok {
			logger.Error().
				Interface("key", key).
				Msg("invalid key type in counter map")
			return true
		}

		counter, ok := value.(int32)
		if !ok {
			logger.Error().
				Interface("value", value).
				Int32("keyId", keyId).
				Msg("invalid counter type in map")
			return true
		}

		if err := dal.Keyword.UpdateCounter(keyId, counter); err != nil {
			logger.Error().
				Err(err).
				Int32("keyId", keyId).
				Int32("counter", counter).
				Msg("failed to update counter")
		} else {
			logger.Debug().
				Int32("keyId", keyId).
				Int32("counter", counter).
				Msg("counter updated successfully")
		}
		return true
	})

	return r.keywordProjects
}
