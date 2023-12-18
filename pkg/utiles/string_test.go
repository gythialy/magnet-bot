package utiles

import "testing"

func TestEscape(t *testing.T) {
	t.Run("Escape special characters", func(t *testing.T) {
		input := "*#_[]()~`>+-=|{}.!@"
		expected := "\\_\\[\\]\\(\\)\\~\\`\\>\\+\\-\\=\\|\\{\\}\\.\\!@"
		result := Escape(input)
		if result != expected {
			t.Errorf("Expected %s, but got %s", expected, result)
		}
	})

	// Testing escape of non-special characters
	t.Run("Escape non-special characters", func(t *testing.T) {
		input := "Hello World"
		expected := "Hello World"
		result := Escape(input)
		if result != expected {
			t.Errorf("Expected %s, but got %s", expected, result)
		}
	})
}
