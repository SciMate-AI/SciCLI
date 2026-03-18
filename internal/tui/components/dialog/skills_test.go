package dialog

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/skills"
)

func TestFilterSkillsRanksScientificMatchFirst(t *testing.T) {
	items := []skills.Skill{
		{ID: "hpc-skills/hpc-vasp", Name: "hpc-vasp", Description: "Build and debug VASP workflows", Scope: skills.ScopeExtension},
		{ID: "user-installed/latex", Name: "latex", Description: "Work with LaTeX documents", Scope: skills.ScopeExtension, Source: skills.UserInstalledExtensionName},
	}

	filtered := filterSkills(items, "vasp workflows")
	if len(filtered) == 0 {
		t.Fatalf("expected at least one filtered skill")
	}
	if filtered[0].ID != "hpc-skills/hpc-vasp" {
		t.Fatalf("expected hpc-vasp to rank first, got %q", filtered[0].ID)
	}
}

func TestFilterSkillsAllowsScopeAndSourceSearch(t *testing.T) {
	items := []skills.Skill{
		{ID: "user-installed/latex", Name: "latex", Description: "Work with LaTeX documents", Scope: skills.ScopeExtension, Source: skills.UserInstalledExtensionName},
	}

	filtered := filterSkills(items, "user-installed")
	if len(filtered) != 1 {
		t.Fatalf("expected one match, got %d", len(filtered))
	}
}
