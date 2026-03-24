package dialog

import "testing"

func TestFilterCommandsRanksBestMatchFirst(t *testing.T) {
	commands := []Command{
		{ID: "compact", Title: "Compact Session", Description: "Summarize the current session"},
		{ID: "ultrawork", Title: "Enable Ultrawork", Description: "Switch runtime into ultrawork mode"},
		{ID: "skills", Title: "Skills", Description: "Browse and activate skills"},
	}

	filtered := filterCommands(commands, "ultra")
	if len(filtered) == 0 {
		t.Fatalf("expected at least one filtered command")
	}
	if filtered[0].ID != "ultrawork" {
		t.Fatalf("expected ultrawork to rank first, got %q", filtered[0].ID)
	}
}

func TestFilterCommandsRequiresTokenMatch(t *testing.T) {
	commands := []Command{
		{ID: "skills", Title: "Skills", Description: "Browse and activate skills"},
	}

	filtered := filterCommands(commands, "nonexistent")
	if len(filtered) != 0 {
		t.Fatalf("expected no matches, got %d", len(filtered))
	}
}

func TestFilterCommandsPrefersBoostedEnabledMatch(t *testing.T) {
	commands := []Command{
		{ID: "promote-a", Title: "Promote Active Experiment", Description: "Promote candidate", Boost: 120},
		{ID: "promote-b", Title: "Promote Active Experiment", Description: "Promote candidate", Boost: 0, Disabled: true},
	}

	filtered := filterCommands(commands, "promote")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(filtered))
	}
	if filtered[0].ID != "promote-a" {
		t.Fatalf("expected boosted enabled command to rank first, got %q", filtered[0].ID)
	}
}
