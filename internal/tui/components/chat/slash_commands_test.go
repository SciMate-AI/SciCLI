package chat

import "testing"

func TestFilterSlashCommandsShowsCommandsForRootSlash(t *testing.T) {
	items := filterSlashCommands("/")
	if len(items) == 0 {
		t.Fatal("expected slash suggestions for root slash")
	}
	if items[0].Command == "" {
		t.Fatal("expected first suggestion to include a command")
	}
	if items[0].Command != "/research set" {
		t.Fatalf("expected top suggestion to be prioritized research command, got %s", items[0].Command)
	}
}

func TestSlashCommandHasArguments(t *testing.T) {
	cmd := slashCommand{Command: "/research set", InsertText: "/research set ", RequiresArgs: true}
	if slashCommandHasArguments("/research set", cmd) {
		t.Fatal("did not expect bare command to count as arguments")
	}
	if !slashCommandHasArguments("/research set lattice relaxation", cmd) {
		t.Fatal("expected trailing text to count as command arguments")
	}
}

func TestDisplayCommandIncludesArgs(t *testing.T) {
	cmd := slashCommand{Command: "/artifact show", Args: "<artifact-id>"}
	if got := cmd.DisplayCommand(); got != "/artifact show <artifact-id>" {
		t.Fatalf("unexpected display command: %s", got)
	}
}

func TestFilterSlashCommandsIncludesScientistBenchCommands(t *testing.T) {
	items := filterSlashCommands("/sb")
	if len(items) == 0 {
		t.Fatal("expected scientist bench slash suggestions")
	}
	if items[0].Command != "/sb new" {
		t.Fatalf("expected /sb new to be the top scientist bench suggestion, got %s", items[0].Command)
	}
}
