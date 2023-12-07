package pkg

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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
)

type Crawler struct {
	ctx       *BotContext
	client    *resty.Client
	converter *md.Converter
}

func NewCrawler(ctx *BotContext) *Crawler {
	return &Crawler{
		ctx:       ctx,
		client:    resty.New().EnableTrace(),
		converter: md.NewConverter("", true, nil),
	}
}

func (c *Crawler) Get() []*m.Result {
	now := time.Now()
	url := fmt.Sprintf("https://%s/freecms/rest/v1/notice/selectInfoMoreChannel.do?operationStartTime=%s&operationEndTime=%s", c.ctx.ServerUrl,
		c.format(now.AddDate(0, 0, -1)), c.format(now))
	idx := 1
	result := make([]*m.Result, 0)
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
			SetQueryParams(params).SetResult(&m.QueryResult{}).Get(url); err == nil {
			r := resp.Result().(*m.QueryResult)
			size := len(r.Data)
			if size > 0 {
				for _, v := range r.Data {
					content, _ := c.converter.ConvertString(v.Content)
					result = append(result, &m.Result{
						NoticeTime:     v.NoticeTime,
						OpenTenderCode: v.OpenTenderCode,
						Title:          c.escape(v.Title),
						Content:        c.escape(content),
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

func (c *Crawler) format(time time.Time) string {
	return fmt.Sprintf("%d-%d-%d%%20%d:%d:%d", time.Year(), time.Month(), time.Day(),
		time.Hour(), time.Minute(), time.Second())
}

// In all other places characters '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'
// must be escaped with the preceding character '\'.
// In all other places characters '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'
// must be escaped with the preceding character '\'.
func (c *Crawler) escape(s string) string {
	return strings.NewReplacer(
		"*", "",
		"#", "",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	).Replace(s)
}
