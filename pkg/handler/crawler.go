package handler

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gythialy/magnet/pkg/model"

	"github.com/gythialy/magnet/pkg/utils"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-resty/resty/v2"
)

const (
	contextType = "application/json"
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:98.0) Gecko/20100101 Firefox/98.0"
	siteId      = "404bb030-5be9-4070-85bd-c94b1473e8de"
	channelId   = "c5bff13f-21ca-4dac-b158-cb40accd3035"
	pageSize    = "20"
	crawlDays   = 1
)

type Crawler struct {
	ctx    *BotContext
	client *resty.Client
}

func NewCrawler(ctx *BotContext) *Crawler {
	client := resty.New().EnableTrace()
	if _, exists := os.LookupEnv(constant.RestyTrace); exists {
		client.SetDebug(true)
	} else {
		client.SetDebug(false)
	}
	return &Crawler{
		ctx:    ctx,
		client: client,
	}
}

func (c *Crawler) Projects() []*Project {
	now := time.Now()
	days := c.crawlDays()
	url := fmt.Sprintf("https://%s/freecms/rest/v1/notice/selectInfoMoreChannel.do?operationStartTime=%s&operationEndTime=%s", c.ctx.Config.MessageServerUrl,
		c.format(now.AddDate(0, 0, -days)), c.format(now))
	idx := 1
	result := make([]*Project, 0)
	params := map[string]string{
		"siteId":  siteId,
		"channel": channelId,
		//"currPage":       string(idx),
		"pageSize": pageSize,
		//"noticeType":     "",
		//"regionCode":     "",
		//"purchaseManner": "",
		//"title":          "",
		//"openTenderCode": "",
		//"selectTimeName": "",
		//"cityOrArea":     "",
		//"purchaseNature": "",
		//"punishType":     "",
	}
	logger := c.ctx.Logger.Info()
	for {
		params["currPage"] = strconv.Itoa(idx)
		if resp, err := c.client.R().
			SetHeader("Content-Type", contextType).
			SetHeader("User-Agent", userAgent).
			SetQueryParams(params).SetResult(&model.ProjectResult{}).Get(url); err == nil {
			r := resp.Result().(*model.ProjectResult)
			size := len(r.Data)
			if size > 0 {
				for _, v := range r.Data {
					content := utils.CleanContent(v.Content)
					result = append(result, &Project{
						NoticeTime:     v.NoticeTime,
						OpenTenderCode: v.OpenTenderCode,
						ShortTitle:     v.Title,
						Title:          v.Title,
						Content:        content,
						Pageurl:        fmt.Sprintf("%s%s", c.ctx.Config.MessageServerUrl, v.Pageurl),
					})
				}
				idx++
			} else {
				break
			}
		} else {
			logger.Err(err).Msgf("fetch %s failed", url)
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	logger.Msgf("total: %d", len(result))

	return result
}

func (c *Crawler) alarmListByKeywords(keywords []string, alarmType constant.CreditType) []*model.Alarm {
	var result []*model.Alarm
	for _, keyword := range keywords {
		params := map[string]string{
			"creditName": keyword,
			"channel":    alarmType.String(),
			"siteId":     siteId,
		}
		if list, err := c.alarmList(params); err == nil {
			result = append(result, list...)
		}
	}
	return result
}

func (c *Crawler) alarmList(params map[string]string) ([]*model.Alarm, error) {
	result := make([]*model.Alarm, 0)
	url := fmt.Sprintf("https://%s/freecms/rest/v1/punish/queryPunishList.do",
		c.ctx.Config.MessageServerUrl)
	if resp, err := c.client.R().
		SetHeader("Content-Type", contextType).
		SetHeader("User-Agent", userAgent).
		SetQueryParams(params).
		SetResult(&model.AlarmList{}).Get(url); err == nil {
		r := resp.Result().(*model.AlarmList)
		for _, row := range r.Data.Rows {
			endDate := c.parseTime(row.EndDate)
			if endDate.IsZero() || time.Now().Before(endDate) {
				result = append(result, &model.Alarm{
					CreditName:       row.CreditName,
					CreditCode:       row.CreditCode,
					BusinessID:       row.ID,
					StartDate:        c.parseTime(row.StartDate),
					EndDate:          &endDate,
					DetailReason:     &row.DetailReason,
					HandleDepartment: &row.HandleDepartment,
					HandleUnit:       &row.HandleUnit,
					HandleResult:     &row.HandleResult,
					NoticeID:         row.NoticeID,
					OriginNoticeID:   &row.OriginNoticeID,
				})
			}
		}
		return result, nil
	} else {
		return nil, err
	}
}

func (c *Crawler) alarm(noticeId string) (*model.AlarmDetail, error) {
	params := map[string]string{
		"punishNoticeId": noticeId,
		"env":            "1",
	}
	url := fmt.Sprintf("https://%s/freecms/rest/v1/punish/selectByNoticeId.do", c.ctx.Config.MessageServerUrl)
	if resp, err := c.client.R().
		SetHeader("Content-Type", contextType).
		SetHeader("User-Agent", userAgent).
		SetQueryParams(params).
		SetResult(&model.AlarmDetail{}).Get(url); err == nil {
		r := resp.Result().(*model.AlarmDetail)
		return r, nil
	} else {
		return nil, err
	}
}

func (c *Crawler) Alarms(keywords []string, userId int64) []*model.Alarm {
	result := make([]*model.Alarm, 0)
	r1 := c.alarmListByKeywords(keywords, constant.CreditTypeBreakFaith)
	cache := make(map[string]interface{})
	for _, alarm := range r1 {
		if _, ok := cache[alarm.CreditCode]; !ok {
			alarm.UserID = userId
			c.alarmTitle(alarm)
			cache[alarm.CreditCode] = alarm
			result = append(result, alarm)
		}
	}
	r2 := c.alarmListByKeywords(keywords, constant.CreditTypeSuspend)
	for _, alarm := range r2 {
		if _, ok := cache[alarm.CreditCode]; !ok {
			alarm.UserID = userId
			c.alarmTitle(alarm)
			cache[alarm.CreditCode] = alarm
			result = append(result, alarm)
		}
	}
	return result
}

func (c *Crawler) alarmTitle(alarm *model.Alarm) {
	if detail, err := c.alarm(alarm.NoticeID); err == nil {
		alarm.Title = &detail.Data.Title
		u := fmt.Sprintf("https://%s%s", c.ctx.Config.MessageServerUrl, detail.Data.Pageurl)
		alarm.PageUrl1 = u
	} else {
		c.ctx.Logger.Error().Msg(err.Error())
	}
	if alarm.OriginNoticeID != nil && *alarm.OriginNoticeID != "" {
		if detail, err := c.alarm(*alarm.OriginNoticeID); err == nil {
			u := fmt.Sprintf("https://%s%s", c.ctx.Config.MessageServerUrl, detail.Data.Pageurl)
			alarm.PageUrl2 = &u
		} else {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}
}

func (c *Crawler) format(time time.Time) string {
	return fmt.Sprintf("%d-%d-%d%%20%d:%d:%d", time.Year(), time.Month(), time.Day(),
		time.Hour(), time.Minute(), time.Second())
}

func (c *Crawler) parseTime(date string) time.Time {
	if date == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.DateOnly, date)
	return t
}

func (c *Crawler) crawlDays() int {
	days := os.Getenv(constant.CrawlDays)
	if days != "" {
		if i, err := strconv.Atoi(days); err == nil {
			return i
		}
	}
	return crawlDays
}
