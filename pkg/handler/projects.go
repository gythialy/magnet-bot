package handler

import (
	"bytes"
	"html/template"
	"slices"
	"strings"

	"github.com/gythialy/magnet/pkg/rule"
	"github.com/gythialy/magnet/pkg/utils"
)

const (
	keywordTemplate = `{{if .HasTenderCode}}🔥{{end}}<a href="{{.Pageurl}}">{{.Title}}</a> @ {{.NoticeTime}}
<b>[{{.Keyword}}]</b>

{{ .Content | noescape }} `
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
	// HasComplexTable marks a notice whose table cannot be flattened into the
	// message body; the pipeline attaches the original page as a PDF instead.
	HasComplexTable bool `json:"-"`

	// keywordIDs lists the keywords that matched this notice. The pipeline
	// records each (keyword, notice) pair once, so a keyword's counter is the
	// number of distinct related notices instead of the number of times the same
	// notice was re-crawled.
	keywordIDs []int32
}

func (p *Project) ToMessage() string {
	var buf bytes.Buffer
	p.HasTenderCode = utils.TenderCodeRegex.MatchString(p.Keyword)
	if err := keywordRender.Execute(&buf, p); err != nil {
		return ""
	}
	return buf.String()
}

// SplitMessage renders the project and splits it into Telegram-sized chunks.
func (p *Project) SplitMessage() ([]string, int) {
	message := p.ToMessage()
	if utf16Len(message) <= maxMessageUnits {
		if chunk := strings.TrimSpace(message); chunk != "" {
			return []string{chunk}, 1
		}
		return nil, 0
	}
	chunks := splitMessage(message)
	return chunks, len(chunks)
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
		keywordProjects: make([]*Project, 0, len(projects)),
		ctx:             ctx,
	}
}

// Filter returns the projects matched by at least one rule.
//
// Every matched project is a shallow copy carrying its own Keyword: the
// caller's slice is shared by every user processed in one run (InfoProcessor
// fans users out to the ants pool), so writing the match result back into the
// original structs would both race and leak one user's keywords into another
// user's message. The copies also own Content/HasTenderCode, which the render
// step rewrites, and the matched keyword ids, which the pipeline counts only
// after the notice has actually been delivered.
func (r *Projects) Filter() []*Project {
	logger := r.ctx.Logger
	for _, v := range r.Projects {
		logger.Debug().Msgf("process: %s,%s[%s]", v.ShortTitle, v.OpenTenderCode, v.NoticeTime)
		matched := make([]string, 0, len(r.rules))
		matchedIDs := make([]int32, 0, len(r.rules))
		for _, cr := range r.rules {
			if cr.IsMatch(v.ShortTitle) || cr.IsMatch(v.OpenTenderCode) {
				matched = append(matched, cr.ToString())
				if cr.Rule != nil && cr.Rule.ID != nil && !slices.Contains(matchedIDs, *cr.Rule.ID) {
					matchedIDs = append(matchedIDs, *cr.Rule.ID)
				}
			}
		}
		if len(matched) == 0 {
			continue
		}
		project := *v
		project.Keyword = strings.Join(matched, "| ")
		project.keywordIDs = matchedIDs
		r.keywordProjects = append(r.keywordProjects, &project)
		logger.Debug().Msgf("matched by (%s)", project.Keyword)
	}

	return r.keywordProjects
}
