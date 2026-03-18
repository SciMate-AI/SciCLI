package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverSkillsFindsWorkspaceUserAndExtensionSkills(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	workDir := t.TempDir()
	writeSkill(t, filepath.Join(workDir, ".scicli", "skills", "local"), "# Local\nLocal workflow")
	writeSkill(t, filepath.Join(homeDir, ".gemini", "skills", "home-skill"), "# Home\nHome workflow")
	writeSkill(t, filepath.Join(workDir, ".claude", "skills", "lab-notes"), "# Lab Notes\nClaude workspace workflow")
	writeSkill(t, filepath.Join(workDir, ".claude", "extensions", "chem", "skills", "analyze"), "# Analyze\nExtension workflow")

	items, err := discoverSkills(workDir)
	require.NoError(t, err)
	require.Len(t, items, 4)

	assert.Equal(t, "chem/analyze", items[0].ID)
	assert.Equal(t, ScopeExtension, items[0].Scope)
	assert.Equal(t, "home-skill", items[1].ID)
	assert.Equal(t, ScopeUser, items[1].Scope)
	assert.Equal(t, "lab-notes", items[2].ID)
	assert.Equal(t, ScopeWorkspace, items[2].Scope)
	assert.Equal(t, "local", items[3].ID)
	assert.Equal(t, ScopeWorkspace, items[3].Scope)
}

func TestServiceActivateResolvesUniqueNameAndTracksSessionState(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	workDir := t.TempDir()
	writeSkill(t, filepath.Join(workDir, ".scicli", "skills", "build-go"), "# Build Go\nRun Go checks")
	writeSkill(t, filepath.Join(workDir, ".claude", "extensions", "go", "skills", "lint"), "# Lint\nRun lint flow")

	svc := &service{
		active:  make(map[string]map[string]Skill),
		workDir: workDir,
	}

	skill, err := svc.Activate(context.Background(), "session-1", "build-go")
	require.NoError(t, err)
	assert.Equal(t, "build-go", skill.ID)

	active := svc.Active("session-1")
	require.Len(t, active, 1)
	assert.Equal(t, "build-go", active[0].ID)

	skill, err = svc.Activate(context.Background(), "session-1", "go/lint")
	require.NoError(t, err)
	assert.Equal(t, "go/lint", skill.ID)

	active = svc.Active("session-1")
	require.Len(t, active, 2)
	assert.Equal(t, "build-go", active[0].ID)
	assert.Equal(t, "go/lint", active[1].ID)
}

func TestResolveSkillRejectsAmbiguousBareName(t *testing.T) {
	items := []Skill{
		{ID: "chem/analyze", Name: "analyze"},
		{ID: "bio/analyze", Name: "analyze"},
	}

	_, err := resolveSkill(items, "analyze")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

func TestRecommendSkillsRanksMatchingSkills(t *testing.T) {
	items := []Skill{
		{ID: "hpc-vasp", Name: "hpc-vasp", Description: "VASP HPC workflows"},
		{ID: "rdkit", Name: "rdkit", Description: "Cheminformatics workflows"},
	}

	results := recommendSkills(items, "help with vasp convergence", 5)
	require.Len(t, results, 1)
	assert.Equal(t, "hpc-vasp", results[0].ID)
}

func TestInstallSkillFromLocalPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	workDir := t.TempDir()
	srcDir := filepath.Join(workDir, "local-skill")
	writeSkill(t, srcDir, "# Local\nRun local workflow")

	skill, err := installSkill(srcDir)
	require.NoError(t, err)

	assert.Equal(t, "user-installed/local-skill", skill.ID)
	assert.FileExists(t, filepath.Join(homeDir, ".scicli", "extensions", userInstalledExtensionName, "skills", "local-skill", skillFileName))
}

func writeSkill(t *testing.T, dir string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, skillFileName), []byte(content), 0o644))
}
