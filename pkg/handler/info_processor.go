package handler

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/utils"
	"golang.org/x/time/rate"

	"github.com/google/generative-ai-go/genai"
	"github.com/gythialy/magnet/pkg/config"
	"google.golang.org/api/option"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/panjf2000/ants/v2"
)

const (
	poolSize = 10

	modelName         = "gemini-1.5-flash"
	requestsPerDay    = 1500
	requestsPerMinute = 15
	systemPrompt      = `将下列 HTML 转换为纯文本:
- 使用纯文本显示，不能包含任何 html 标签
- "申领时间"和"申领地址"之间应该去除多余的换行和空格转为一行，如: "2024年11月07日 至 2024年11月12日，每天上午 08:30 至 11:30，下午13:00至16:30(北京时间,工作日)"
- 对于复杂的表格使用csv格式显示，每个单元格的值删除多余的换行符和空白字符，如果处理后该行所有单元格的内容都为空，则删除，正常数据最终格式显示为"1;cell1value;cell2value"\n%s`
)

var (
	htmlTagsRegex = regexp.MustCompile(`<[^>]*>|</[^>]*>|<[^/][^>]*/>|<\s*[a-zA-Z][^>]*>`)
	htmlAttrRegex = regexp.MustCompile(`\s+\w+\s*=\s*("[^"]*"|'[^']*')`)
)

type InfoProcessor struct {
	ctx            *BotContext
	pool           *ants.PoolWithFunc
	gemini         *genai.Client
	crawler        *Crawler
	minuteLimiter  *rate.Limiter
	dailyCount     int64
	dailyResetTime time.Time
}

func NewInfoProcessor(ctx *BotContext) (*InfoProcessor, error) {
	client, err := genai.NewClient(context.Background(), option.WithAPIKey(config.GeminiAPIKey()))
	if err != nil {
		return nil, err
	}

	processor := &InfoProcessor{
		ctx:            ctx,
		minuteLimiter:  rate.NewLimiter(rate.Every(time.Minute/requestsPerMinute), 1),
		dailyResetTime: nextMidnight(),
		gemini:         client,
		crawler:        NewCrawler(ctx),
	}

	if pool, err := ants.NewPoolWithFunc(poolSize, processor.Handler); err != nil {
		return nil, err
	} else {
		processor.pool = pool
	}
	return processor, nil
}

func (r *InfoProcessor) Process() {
	projects := r.crawler.Projects()
	conf := r.config()
	for _, data := range conf {
		data.Projects = projects
		data.Alarms = r.crawler.Alarms(data.AlarmKeyword, data.UserId)
		data.IsForced = false
		if err := r.pool.Invoke(data); err != nil {
			r.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}
}

func (r *InfoProcessor) Get(userId int64) {
	results := r.crawler.Projects()
	if len(results) > 0 {
		data := r.get(userId)
		data.Projects = results
		data.IsForced = true
		if err := r.pool.Invoke(data); err != nil {
			r.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}
}

func (r *InfoProcessor) Release() {
	r.pool.Release()
	if err := r.gemini.Close(); err != nil {
		r.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
}

func (r *InfoProcessor) config() map[int64]ProcessData {
	ids := dal.Keyword.Ids()
	m := make(map[int64]ProcessData)
	for _, id := range ids {
		if _, ok := m[id]; !ok {
			m[id] = r.get(id)
		}
	}

	return m
}

func (r *InfoProcessor) get(id int64) ProcessData {
	keywords := dal.Keyword.GetByUserIdAndType(id, model.PROJECT)
	var rules []*rule.ComplexRule
	for _, kw := range keywords {
		r := rule.NewComplexRule(kw)
		if r != nil {
			rules = append(rules, r)
		}
	}
	return ProcessData{
		UserId:       id,
		ProjectRules: rules,
		AlarmKeyword: dal.Keyword.GetKeywords(id, model.ALARM),
	}
}

func (r *InfoProcessor) Handler(i interface{}) {
	switch pd := i.(type) {
	case ProcessData:
		// process projects
		historyDao := dal.History
		alarmDao := dal.Alarm
		projects := NewProjects(r.ctx, pd.Projects, pd.ProjectRules).Filter()
		failed := []string{"failed:"}
		filterFailed := make(map[string]*Project)
		userId := pd.UserId
		var processedURL []*model.History
		now := time.Now()

		logger := r.ctx.Logger
	projectLoop:
		for _, project := range projects {
			title := project.Title
			pageURL := project.Pageurl
			shortTitle := project.ShortTitle
			if historyDao.IsUrlExist(userId, pageURL) && !pd.IsForced {
				logger.Debug().Msgf("%s already processed", shortTitle)
				continue
			}

			chunks, total := r.ToMessage(project)
			logger.Debug().Msgf("split content to %d parts", total)

			// only save all parts failed to failed list
			isSuccessful := false
			for idx, chunk := range chunks {
				if _, err := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:    userId,
					Text:      chunk,
					ParseMode: models.ParseModeHTML,
				}); err != nil {
					if !isSuccessful {
						if _, ok := filterFailed[pageURL]; !ok {
							filterFailed[pageURL] = project
							failed = append(failed, fmt.Sprintf("%d <b>【关键字: %s</b> <a href=\"%s\">%s</a>",
								len(failed), project.Keyword, pageURL, title))
						}
					}
					logger.Error().Stack().Err(err).Msgf("content: %s", chunk)
					if idx == 0 {
						continue projectLoop
					}
				} else {
					isSuccessful = true
					logger.Info().Msgf("notify: %s[%s]-%d", shortTitle, project.OpenTenderCode, idx)
				}
				time.Sleep(500 * time.Millisecond)
			}

			if total > 0 {
				processedURL = append(processedURL, &model.History{
					UserID:    userId,
					URL:       pageURL,
					Title:     shortTitle,
					UpdatedAt: now,
				})
			}
		}

		if len(failed) > 1 {
			if _, err := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
				ChatID:    userId,
				Text:      strings.Join(failed, "\n"),
				ParseMode: models.ParseModeHTML,
			}); err != nil {
				logger.Error().Stack().Err(err).Msg("")
			} else {
				for _, v := range filterFailed {
					processedURL = append(processedURL, &model.History{
						UserID:    userId,
						URL:       v.Pageurl,
						Title:     v.ShortTitle,
						UpdatedAt: now,
					})
				}
			}
		}

		if len(processedURL) > 0 {
			if err := historyDao.Insert(processedURL); err != nil {
				logger.Error().Stack().Err(err).Msg("")
			}
		}

		// process alarms
		alarmCache := alarmDao.Cache(userId)
		var newAlarms []*model.Alarm
		for _, alarm := range pd.Alarms {
			if _, ok := alarmCache[alarm.CreditCode]; ok {
				continue
			}
			msg, _ := alarm.ToMessage()
			if _, err := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
				ChatID:    userId,
				Text:      msg,
				ParseMode: models.ParseModeHTML,
			}); err != nil {
				logger.Error().Stack().Err(err).Msg("send alarm")
			} else {
				newAlarms = append(newAlarms, alarm)
			}
		}

		if len(newAlarms) > 0 {
			if err := alarmDao.Insert(newAlarms); err != nil {
				logger.Error().Stack().Err(err).Msg("")
			}
		}
	}
}

func (r *InfoProcessor) ToMessage(project *Project) ([]string, int) {
	// Reset daily counter if needed
	if time.Now().After(r.dailyResetTime) {
		r.dailyCount = 0
		r.dailyResetTime = nextMidnight()
	}

	// Use simplified content if we hit API limits or encounter errors
	if r.dailyCount >= requestsPerDay {
		r.ctx.Logger.Error().Msgf("daily API limit (%d) reached", requestsPerDay)
		project.Content = utils.SimplifyContent(project.Content)
		return project.SplitMessage()
	}

	// Try to use Gemini API
	if err := r.minuteLimiter.Wait(context.Background()); err != nil {
		r.ctx.Logger.Error().Err(err).Msg("minute rate limiter error")
		project.Content = utils.SimplifyContent(project.Content)
		return project.SplitMessage()
	}

	// Generate content using Gemini
	m := r.gemini.GenerativeModel(modelName)
	prompt := genai.Text(fmt.Sprintf(systemPrompt, project.Content))
	resp, err := m.GenerateContent(context.Background(), prompt)
	if err != nil {
		r.ctx.Logger.Error().Stack().Err(err).Msg("Gemini API error")
		project.Content = utils.SimplifyContent(project.Content)
		return project.SplitMessage()
	}

	if content := extractContent(resp); content != "" {
		project.Content = content
	} else {
		project.Content = utils.SimplifyContent(project.Content)
	}

	return project.SplitMessage()
}

func extractContent(resp *genai.GenerateContentResponse) string {
	if resp == nil {
		return ""
	}

	for _, can := range resp.Candidates {
		if can.Content == nil {
			continue
		}
		for _, part := range can.Content.Parts {
			if part == nil {
				continue
			}
			if data, ok := part.(genai.Text); ok {
				return cleanContent(string(data))
			}
		}
	}
	return ""
}

func cleanContent(content string) string {
	// Remove HTML attributes and tags
	content = htmlAttrRegex.ReplaceAllString(content, "")
	content = htmlTagsRegex.ReplaceAllString(content, "")

	// Remove markdown style bold and any remaining < or > characters
	content = strings.NewReplacer(
		"**", "",
	).Replace(content)

	return content
}

func nextMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0,
		now.Location()).Add(24 * time.Hour)
}

type ProcessData struct {
	UserId       int64
	ProjectRules []*rule.ComplexRule
	AlarmKeyword []string
	Projects     []*Project
	Alarms       []*model.Alarm
	IsForced     bool
}
