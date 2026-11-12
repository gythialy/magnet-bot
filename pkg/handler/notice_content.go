package handler

import (
	"errors"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/utils"
)

// warmNoticeContent caches the notice body the crawl API already returned, so
// the Mini App's first open of a freshly delivered notice costs no page fetch.
//
// The crawl API's content is a fragment, not a page, so it goes through the
// fragment sanitizer; the page path (utils.ExtractNoticeBody) is what the API
// uses when it has to fetch. Both end in the same whitelist, which is why a
// cached body is safe to insert into the app document either way.
//
// The write is best effort and never overwrites an existing row: a reader who
// forced a refresh must not have it rolled back by the next crawl cycle.
func (c *BotContext) warmNoticeContent(project *Project) {
	if c == nil || project == nil || project.Pageurl == "" || project.RawContent == "" {
		return
	}

	body, err := utils.SanitizeNoticeFragment(project.RawContent, c.baseURL())
	if err != nil {
		// A body without prose is not worth caching; the app will fetch the
		// page on demand and the page may still render.
		if !errors.Is(err, utils.ErrNoticeBodyMissing) {
			c.Logger.Debug().Err(err).Msg("skip notice content warm-up")
		}
		return
	}

	if _, err := dal.SaveNoticeContentIfAbsent(project.Pageurl, body, time.Now()); err != nil {
		c.Logger.Error().Stack().Err(err).Msg("cache notice content")
	}
}

// warmDeliveredContent caches the bodies of the notices that actually reached
// the user.
//
// Only delivered notices are warmed on purpose: a cache entry is readable only
// by someone who owns the history row, so warming a filtered-out notice would
// store a body nobody can open. A failure here is logged per notice and never
// affects delivery, which has already happened.
func (r *InfoProcessor) warmDeliveredContent(delivered []*Project) {
	for _, project := range delivered {
		r.ctx.warmNoticeContent(project)
	}
}

// baseURL is the site root both the crawler and this warm-up resolve relative
// links against. It is defined on Crawler; this keeps the shared form close to
// its second caller instead of duplicating the string concatenation.
func (c *BotContext) baseURL() string {
	if c == nil || c.Config == nil {
		return ""
	}
	return "https://" + c.Config.MessageServerUrl
}
