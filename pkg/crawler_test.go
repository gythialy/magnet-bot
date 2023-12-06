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

func TestCrawler_escape(t *testing.T) {
	crawler := &Crawler{}

	// Testing escape of special characters
	t.Run("Escape special characters", func(t *testing.T) {
		input := "*#_[]()~`>+-=|{}.!@"
		expected := "\\_\\[\\]\\(\\)\\~\\`\\>\\+\\-\\=\\|\\{\\}\\.\\!@"
		result := crawler.escape(input)
		if result != expected {
			t.Errorf("Expected %s, but got %s", expected, result)
		}
	})

	// Testing escape of non-special characters
	t.Run("Escape non-special characters", func(t *testing.T) {
		input := "Hello World"
		expected := "Hello World"
		result := crawler.escape(input)
		if result != expected {
			t.Errorf("Expected %s, but got %s", expected, result)
		}
	})
}
