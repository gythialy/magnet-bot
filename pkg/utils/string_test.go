package utils

import "testing"

func TestEscape(t *testing.T) {
	t.Run("EscapeMarkdown special characters", func(t *testing.T) {
		input := "*#_[]()~`>+-=|{}.!@"
		expected := "\\_\\[\\]\\(\\)\\~\\`\\>\\+\\-\\=\\|\\{\\}\\.\\!@"
		result := EscapeMarkdown(input)
		if result != expected {
			t.Errorf("Expected %s, but got %s", expected, result)
		}
	})

	// Testing escape of non-special characters
	t.Run("EscapeMarkdown non-special characters", func(t *testing.T) {
		input := "Hello World"
		expected := "Hello World"
		result := EscapeMarkdown(input)
		if result != expected {
			t.Errorf("Expected %s, but got %s", expected, result)
		}
	})
}
