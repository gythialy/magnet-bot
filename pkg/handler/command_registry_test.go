package handler

import (
	"strings"
	"testing"

	"github.com/gythialy/magnet/pkg/constant"
)

func TestHelpTextGeneratedFromRegistry(t *testing.T) {
	// Without the Mini App every command is still offered in the chat.
	help := HelpText(false)

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
	if strings.Contains(help, miniAppHelpHint) {
		t.Error("help should not point at the Mini App while it is disabled")
	}
}

func TestHelpTextHidesRetiredCommands(t *testing.T) {
	help := HelpText(true)

	for command := range retiredCommands {
		if strings.Contains(help, command) {
			t.Errorf("help still advertises retired command %s", command)
		}
	}
	// The commands that stay in the chat are still documented, including the
	// argument placeholder that tells the user how to call them.
	for _, want := range []string{
		constant.Me,
		constant.Alarm + " <id>",
		constant.Retry,
		constant.Clean,
		"(admin only)",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help is missing %q", want)
		}
	}
	if !strings.Contains(help, miniAppHelpHint) {
		t.Errorf("help is missing the Mini App hint %q", miniAppHelpHint)
	}
}

func TestTelegramCommandsMatchesRegistry(t *testing.T) {
	cmds := TelegramCommands(false)
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

func TestTelegramCommandsHidesRetiredCommands(t *testing.T) {
	cmds := TelegramCommands(true)

	if want := len(commandSpecs) - len(retiredCommands); len(cmds) != want {
		t.Fatalf("command count = %d, want %d (retired commands must be hidden)", len(cmds), want)
	}

	seen := make(map[string]bool, len(cmds))
	for _, cmd := range cmds {
		seen[cmd.Command] = true
	}
	for command := range retiredCommands {
		if seen[command] {
			t.Errorf("command menu still advertises retired command %s", command)
		}
	}
	for _, command := range []string{constant.Me, constant.Alarm, constant.Retry, constant.Clean} {
		if !seen[command] {
			t.Errorf("command menu is missing retained command %s", command)
		}
	}
}
