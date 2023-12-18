package pkg

import (
	"os"
	"testing"
)

func TestCrawler_Get(t *testing.T) {
	crawler := NewCrawler(&BotContext{
		ServerUrl: os.Getenv("ServerUrl"),
	})

	results := crawler.Get()
	t.Log(len(results))
}
