package pkg

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/gythialy/magnet/pkg/utiles"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/go-resty/resty/v2"
	m "github.com/gythialy/magnet/pkg/entities"
)

const (
	ContextType = "application/json"
	UserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:98.0) Gecko/20100101 Firefox/98.0"
	siteId      = "404bb030-5be9-4070-85bd-c94b1473e8de"
	channelId   = "c5bff13f-21ca-4dac-b158-cb40accd3035"
	pageSize    = "20"
	crawlDays   = 2
)

type Crawler struct {
	ctx       *BotContext
	client    *resty.Client
	converter *md.Converter
}

func NewCrawler(ctx *BotContext) *Crawler {
	client := resty.New().EnableTrace()
	if _, exists := os.LookupEnv(constant.RestyTrace); exists {
		client.SetDebug(true)
	} else {
		client.SetDebug(false)
	}
	return &Crawler{
		ctx:       ctx,
		client:    client,
		converter: md.NewConverter("", true, nil),
	}
}

func (c *Crawler) FetchProjects() []*Project {
	now := time.Now()
	days := c.crawlDays()
	url := fmt.Sprintf("https://%s/freecms/rest/v1/notice/selectInfoMoreChannel.do?operationStartTime=%s&operationEndTime=%s", c.ctx.ServerUrl,
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
			SetHeader("Content-Type", ContextType).
			SetHeader("User-Agent", UserAgent).
			SetQueryParams(params).SetResult(&m.ProjectResult{}).Get(url); err == nil {
			r := resp.Result().(*m.ProjectResult)
			size := len(r.Data)
			if size > 0 {
				for _, v := range r.Data {
					content, _ := c.converter.ConvertString(v.Content)
					result = append(result, &Project{
						NoticeTime:     v.NoticeTime,
						OpenTenderCode: v.OpenTenderCode,
						ShortTitle:     v.Title,
						Title:          utiles.Escape(v.Title),
						Content:        utiles.Escape(content),
						Pageurl:        fmt.Sprintf("%s%s", c.ctx.ServerUrl, v.Pageurl),
					})
				}
				idx++
			} else {
				break
			}
		} else {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	logger.Msgf("total: %d", len(result))

	return result
}

func (c *Crawler) fetch(keywords []string, type_ string) []*m.Alarm {
	result := make([]*m.Alarm, 0)
	for _, keyword := range keywords {
		params := map[string]string{
			"publishType": type_,
			"creditName":  keyword,
		}

		url := fmt.Sprintf("https://%s/gateway/gpc-gpcms/rest/v2/punish/public?&pageNumber=1&pageSize=10&handleUnit=&startDate=&endDate=", c.ctx.ServerUrl)
		if resp, err := c.client.R().
			SetHeader("Content-Type", ContextType).
			SetHeader("User-Agent", UserAgent).
			SetQueryParams(params).
			SetResult(&m.AlarmResult{}).Get(url); err == nil {
			r := resp.Result().(*m.AlarmResult)
			for _, row := range r.Data.Rows {
				endDate := c.parseTime(row.EndDate)
				if endDate.IsZero() || time.Now().Before(endDate) {
					result = append(result, &m.Alarm{
						CreditName:       row.CreditName,
						CreditCode:       row.CreditCode,
						StartDate:        c.parseTime(row.StartDate),
						EndDate:          endDate,
						DetailReason:     row.DetailReason,
						HandleDepartment: row.HandleDepartment,
						HandleUnit:       row.HandleUnit,
						HandleResult:     row.HandleResult,
					})
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return result
}

func (c *Crawler) Fetch(keywords []string, userId int64) []*m.Alarm {
	result := make([]*m.Alarm, 0)
	r1 := c.fetch(keywords, "breakFaith")
	cache := make(map[string]interface{})
	for _, alarm := range r1 {
		if _, ok := cache[alarm.CreditCode]; !ok {
			alarm.UserId = userId
			cache[alarm.CreditCode] = alarm
			result = append(result, alarm)
		}
	}
	r2 := c.fetch(keywords, "suspend")
	for _, alarm := range r2 {
		if _, ok := cache[alarm.CreditCode]; !ok {
			alarm.UserId = userId
			cache[alarm.CreditCode] = alarm
			result = append(result, alarm)
		}
	}
	return result
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
