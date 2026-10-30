package handler

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"

	"github.com/go-resty/resty/v2"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	contextType = "application/json"
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:98.0) Gecko/20100101 Firefox/98.0"
	siteId      = "404bb030-5be9-4070-85bd-c94b1473e8de"
	channelId   = "c5bff13f-21ca-4dac-b158-cb40accd3035"
	pageSize    = "20"
	crawlDays   = 1

	// pageFetchInterval rate-limits the paginated notice list.
	pageFetchInterval = 200 * time.Millisecond
	// alarmFetchConcurrency bounds how many keyword queries run at once.
	alarmFetchConcurrency = 8
	// crawlerRequestTimeout bounds every API request. The scheduler runs in
	// SingletonMode, so one hung connection would otherwise stall the whole
	// crawl (and every user's notifications) until the OS gives up.
	crawlerRequestTimeout = 30 * time.Second
)

type Crawler struct {
	ctx    *BotContext
	client *resty.Client
}

func NewCrawler(ctx *BotContext) *Crawler {
	client := resty.New().SetTimeout(crawlerRequestTimeout)
	// RESTY tracing is expensive and very noisy, so it follows LOG_LEVEL
	// instead of a dedicated switch: only trace turns it on.
	if ctx.Config != nil && ctx.Config.LogLevel <= zerolog.TraceLevel {
		client.EnableTrace().SetDebug(true)
	}
	return &Crawler{
		ctx:    ctx,
		client: client,
	}
}

// baseURL is the site root the API paths are relative to.
func (c *Crawler) baseURL() string {
	return "https://" + c.ctx.Config.MessageServerUrl
}

// absoluteURL turns a notice path returned by the API into a clickable
// absolute URL. Values that already carry a scheme are returned unchanged.
func (c *Crawler) absoluteURL(link string) string {
	return utils.ResolveURL(c.baseURL(), link)
}

func (c *Crawler) Projects() []*Project {
	now := time.Now()
	days := c.crawlDays()
	url := fmt.Sprintf("https://%s/freecms/rest/v1/notice/selectInfoMoreChannel.do?operationStartTime=%s&operationEndTime=%s",
		c.ctx.Config.MessageServerUrl, c.format(now.AddDate(0, 0, -days)), c.format(now))
	params := map[string]string{
		"siteId":   siteId,
		"channel":  channelId,
		"pageSize": pageSize,
	}
	logger := c.ctx.Logger.Info()
	var result []*Project
	for idx := 1; ; idx++ {
		params["currPage"] = strconv.Itoa(idx)
		resp, err := c.client.R().
			SetHeader("Content-Type", contextType).
			SetHeader("User-Agent", userAgent).
			SetQueryParams(params).
			SetResult(&model.ProjectResult{}).
			Get(url)
		if err != nil {
			logger.Err(err).Msgf("fetch %s failed", url)
			break
		}

		r, ok := resp.Result().(*model.ProjectResult)
		if !ok || len(r.Data) == 0 {
			break
		}
		for _, v := range r.Data {
			// The sanitizer needs the site root to keep relative links in the
			// body clickable, and reports tables too complex to flatten.
			content, complexTable := utils.SimplifyHTML(v.Content, c.baseURL())
			result = append(result, &Project{
				NoticeTime:      v.NoticeTime,
				OpenTenderCode:  v.OpenTenderCode,
				ShortTitle:      v.Title,
				Title:           v.Title,
				Content:         content,
				Pageurl:         c.absoluteURL(v.Pageurl),
				HasComplexTable: complexTable,
			})
		}
		time.Sleep(pageFetchInterval)
	}

	logger.Msgf("total: %d", len(result))
	return result
}

// alarmListByKeywords queries one credit type for every keyword with a bounded
// fan-out. A failing keyword is logged and skipped so it cannot drop the rest.
func (c *Crawler) alarmListByKeywords(keywords []string, alarmType constant.CreditType) []*model.Alarm {
	if len(keywords) == 0 {
		return nil
	}

	lists := make([][]*model.Alarm, len(keywords))
	var g errgroup.Group
	g.SetLimit(alarmFetchConcurrency)
	for i, keyword := range keywords {
		g.Go(func() error {
			params := map[string]string{
				"creditName": keyword,
				"channel":    alarmType.String(),
				"siteId":     siteId,
			}
			list, err := c.alarmList(params)
			if err != nil {
				c.ctx.Logger.Debug().Err(err).Msgf("fetch alarm list for %q failed", keyword)
				return nil
			}
			lists[i] = list
			return nil
		})
	}
	_ = g.Wait()

	result := make([]*model.Alarm, 0, len(keywords)*10)
	for _, list := range lists {
		result = append(result, list...)
	}
	return result
}

func (c *Crawler) alarmList(params map[string]string) ([]*model.Alarm, error) {
	url := fmt.Sprintf("https://%s/freecms/rest/v1/punish/queryPunishList.do", c.ctx.Config.MessageServerUrl)
	resp, err := c.client.R().
		SetHeader("Content-Type", contextType).
		SetHeader("User-Agent", userAgent).
		SetQueryParams(params).
		SetResult(&model.AlarmList{}).
		Get(url)
	if err != nil {
		return nil, err
	}

	r, ok := resp.Result().(*model.AlarmList)
	if !ok {
		return nil, errors.New("unexpected alarm list response")
	}

	now := time.Now()
	result := make([]*model.Alarm, 0, len(r.Data.Rows))
	for _, row := range r.Data.Rows {
		endDate := c.parseTime(row.EndDate)
		if !endDate.IsZero() && now.After(endDate) {
			continue
		}
		result = append(result, &model.Alarm{
			CreditName:       row.CreditName,
			CreditCode:       row.CreditCode,
			BusinessID:       row.ID,
			StartDate:        c.parseTime(row.StartDate),
			EndDate:          new(endDate),
			DetailReason:     new(row.DetailReason),
			HandleDepartment: new(row.HandleDepartment),
			HandleUnit:       new(row.HandleUnit),
			HandleResult:     new(row.HandleResult),
			NoticeID:         row.NoticeID,
			OriginNoticeID:   new(row.OriginNoticeID),
		})
	}
	return result, nil
}

func (c *Crawler) alarm(noticeId string) (*model.AlarmDetail, error) {
	params := map[string]string{
		"punishNoticeId": noticeId,
		"env":            "1",
	}
	url := fmt.Sprintf("https://%s/freecms/rest/v1/punish/selectByNoticeId.do", c.ctx.Config.MessageServerUrl)
	resp, err := c.client.R().
		SetHeader("Content-Type", contextType).
		SetHeader("User-Agent", userAgent).
		SetQueryParams(params).
		SetResult(&model.AlarmDetail{}).
		Get(url)
	if err != nil {
		return nil, err
	}
	detail, ok := resp.Result().(*model.AlarmDetail)
	if !ok {
		return nil, errors.New("unexpected alarm detail response")
	}
	return detail, nil
}

func (c *Crawler) Alarms(keywords []string, userId int64) []*model.Alarm {
	// Cache alarm detail fetches by noticeId. Multiple alarms often share the
	// same notice (origin notice id), and breakFaith/suspend lists overlap, so
	// without a cache the same detail endpoint gets hit N+1 times.
	detailCache := make(map[string]*model.AlarmDetail)
	seen := make(map[string]struct{})

	var result []*model.Alarm
	for _, alarmType := range []constant.CreditType{constant.CreditTypeBreakFaith, constant.CreditTypeSuspend} {
		for _, alarm := range c.alarmListByKeywords(keywords, alarmType) {
			if _, ok := seen[alarm.CreditCode]; ok {
				continue
			}
			seen[alarm.CreditCode] = struct{}{}
			alarm.UserID = userId
			c.alarmTitle(alarm, detailCache)
			result = append(result, alarm)
		}
	}
	return result
}

func (c *Crawler) alarmTitle(alarm *model.Alarm, detailCache map[string]*model.AlarmDetail) {
	detail, err := c.alarmCached(alarm.NoticeID, detailCache)
	if err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	} else {
		alarm.Title = new(detail.Data.Title)
		alarm.PageUrl1 = c.absoluteURL(detail.Data.Pageurl)
	}

	if alarm.OriginNoticeID == nil || *alarm.OriginNoticeID == "" {
		return
	}
	origin, err := c.alarmCached(*alarm.OriginNoticeID, detailCache)
	if err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
		return
	}
	alarm.PageUrl2 = new(c.absoluteURL(origin.Data.Pageurl))
}

// alarmCached fetches an alarm detail once per Process() run and reuses it
// for every alarm that references the same noticeId.
func (c *Crawler) alarmCached(noticeId string, detailCache map[string]*model.AlarmDetail) (*model.AlarmDetail, error) {
	if detail, ok := detailCache[noticeId]; ok {
		return detail, nil
	}
	detail, err := c.alarm(noticeId)
	if err != nil {
		return nil, err
	}
	detailCache[noticeId] = detail
	return detail, nil
}

func (c *Crawler) format(t time.Time) string {
	return fmt.Sprintf("%d-%d-%d%%20%d:%d:%d", t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second())
}

func (c *Crawler) parseTime(date string) time.Time {
	if date == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.DateOnly, date)
	return t
}

// crawlDays returns how many days back a run looks. A value below one would
// produce an empty window, so it falls back to the default instead.
func (c *Crawler) crawlDays() int {
	if days := os.Getenv(constant.CrawlDays); days != "" {
		if i, err := strconv.Atoi(days); err == nil && i >= 1 {
			return i
		}
	}
	return crawlDays
}
