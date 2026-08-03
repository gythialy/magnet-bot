package handler

import (
	"strings"
	"testing"
)

func TestHelpTextGeneratedFromRegistry(t *testing.T) {
	help := HelpText()

	if !strings.Contains(help, "/add_keywords") {
		t.Error("help missing /add_keywords")
	}
	if !strings.Contains(help, "/clean") {
		t.Error("help missing /clean")
	}
	if !strings.Contains(help, "admin only") {
		t.Error("help should mark admin-only commands")
	}
	// usage placeholders are rendered
	if !strings.Contains(help, "/convertpdf <url>") {
		t.Error("help should render usage placeholder for /convertpdf")
	}
}

func TestTelegramCommandsMatchesRegistry(t *testing.T) {
	cmds := TelegramCommands()
	if len(cmds) != len(commandSpecs) {
		t.Fatalf("command count mismatch: registry=%d telegram=%d", len(commandSpecs), len(cmds))
	}

	seen := make(map[string]bool)
	for _, spec := range commandSpecs {
		seen[spec.Command] = true
	}
	for _, cmd := range cmds {
		if !seen[cmd.Command] {
			t.Errorf("unexpected command in TelegramCommands: %s", cmd.Command)
		}
		if cmd.Description == "" {
			t.Errorf("command %s has empty description", cmd.Command)
		}
	}
}
