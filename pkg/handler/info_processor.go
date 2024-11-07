package handler

import (
	"context"
	"fmt"
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
	poolSize          = 10
	maxMessageLength  = 4090
	modelName         = "gemini-1.5-flash"
	requestsPerDay    = 1500
	requestsPerMinute = 15
	systemPrompt      = `将下列 HTML 转换为纯文本:
- 尽可能使用文本显示
- "申领时间"和"申领地址"之间应该去除多余的换行和空格转为一行，如: "2024年11月07日 至 2024年11月12日，每天上午 08:30 至 11:30，下午13:00至16:30(北京时间,工作日)"
- 对于复杂的表格使用csv格式显示，每个单元格的值删除多余的换行符和空白字符，如果处理后该行所有单元格的内容都为空，则删除，正常数据最终格式显示为"1;cell1value;cell2value"\n%s`
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
		minuteLimiter:  rate.NewLimiter(rate.Every(time.Minute/requestsPerMinute), 1), // 15 requests per minute
		dailyResetTime: time.Now().Add(24 * time.Hour),
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

			for idx, chunk := range chunks {
				if _, err := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:    userId,
					Text:      chunk,
					ParseMode: models.ParseModeHTML,
				}); err != nil {
					if _, ok := filterFailed[pageURL]; !ok {
						filterFailed[pageURL] = project
						failed = append(failed, fmt.Sprintf("<a href=\"%s\">%s</a>", pageURL, title))
					}
					logger.Error().Stack().Err(err).Msg("")
					if idx == 0 {
						continue projectLoop
					}
				} else {
					logger.Info().Msgf("notify: %s[%s]-%d", shortTitle, project.OpenTenderCode, idx)
				}
				time.Sleep(500 * time.Millisecond)
			}

			processedURL = append(processedURL, &model.History{
				UserID:    userId,
				URL:       pageURL,
				Title:     shortTitle,
				UpdatedAt: now,
			})
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
				logger.Error().Stack().Err(err).Msg("")
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
	now := time.Now()

	// Reset daily counter if we've passed the reset time
	if now.After(r.dailyResetTime) {
		r.dailyCount = 0
		r.dailyResetTime = now.Add(24 * time.Hour)
	}

	// Check daily limit
	if r.dailyCount >= requestsPerDay {
		project.Content = utils.SimplifyContent(project.Content)
		r.ctx.Logger.Error().Msgf("daily API limit (%d) reached", requestsPerDay)
	} else {
		ctx := context.Background()
		if err := r.minuteLimiter.Wait(ctx); err != nil {
			r.ctx.Logger.Error().Err(err).Msg("minute rate limiter error")
		} else {
			m := r.gemini.GenerativeModel(modelName)
			content := project.Content
			prompt := genai.Text(fmt.Sprintf(systemPrompt, content))

			if resp, err := m.GenerateContent(context.Background(), prompt); err == nil {
				for _, can := range resp.Candidates {
					if can.Content != nil {
						for _, part := range can.Content.Parts {
							if part != nil {
								if data, ok := part.(genai.Text); ok {
									project.Content = strings.ReplaceAll(string(data), "**", "")
									break
								}
							}
						}
					}
				}
			} else {
				r.ctx.Logger.Error().Stack().Err(err).Msg("")
			}
		}
	}

	message := project.ToMessage()

	var chunks []string
	for len(message) > 0 {
		if len(message) <= maxMessageLength {
			chunks = append(chunks, message)
			break
		}

		chunk := message[:maxMessageLength]
		lastNewline := strings.LastIndex(chunk, "\n")
		if lastNewline > 0 {
			chunk = chunk[:lastNewline]
		}

		chunks = append(chunks, chunk)
		message = message[len(chunk):]
	}
	return chunks, len(chunks)
}

type ProcessData struct {
	UserId       int64
	ProjectRules []*rule.ComplexRule
	AlarmKeyword []string
	Projects     []*Project
	Alarms       []*model.Alarm
	IsForced     bool
}
