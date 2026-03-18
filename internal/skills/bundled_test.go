package skills

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncBundledExtensionsCopiesFilesAndWritesVersion(t *testing.T) {
	destRoot := t.TempDir()
	fsys := fstest.MapFS{
		"bundled/demo/skills/example/SKILL.md":  {Data: []byte("# Demo\nExample skill")},
		"bundled/demo/skills/example/notes.txt": {Data: []byte("hello")},
	}

	err := syncBundledExtensions(fsys, destRoot, []bundledExtension{
		{
			Name:      "demo",
			EmbedDir:  "bundled/demo",
			SourceURL: "https://example.test/demo",
			Ref:       "abc123",
			Date:      "2026-03-18",
		},
	})
	require.NoError(t, err)

	skillPath := filepath.Join(destRoot, "demo", "skills", "example", "SKILL.md")
	versionPath := filepath.Join(destRoot, "demo", bundledVersionFile)

	skillData, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	assert.Equal(t, "# Demo\nExample skill", string(skillData))

	versionData, err := os.ReadFile(versionPath)
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/demo abc123 2026-03-18\n", string(versionData))
}

func TestSyncBundledExtensionsOverwritesWhenVersionChanges(t *testing.T) {
	destRoot := t.TempDir()
	destDir := filepath.Join(destRoot, "demo", "skills", "example")
	require.NoError(t, os.MkdirAll(destDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte("old"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(destRoot, "demo", bundledVersionFile),
		[]byte("https://example.test/demo oldref 2026-03-17\n"),
		0o644,
	))

	fsys := fstest.MapFS{
		"bundled/demo/skills/example/SKILL.md": {Data: []byte("new")},
	}

	err := syncBundledExtensions(fsys, destRoot, []bundledExtension{
		{
			Name:      "demo",
			EmbedDir:  "bundled/demo",
			SourceURL: "https://example.test/demo",
			Ref:       "newref",
			Date:      "2026-03-18",
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(destDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestSyncBundledExtensionsPreservesExistingFilesWhenVersionMatches(t *testing.T) {
	destRoot := t.TempDir()
	destDir := filepath.Join(destRoot, "demo", "skills", "example")
	require.NoError(t, os.MkdirAll(destDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte("custom"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(destRoot, "demo", bundledVersionFile),
		[]byte("https://example.test/demo stable 2026-03-18\n"),
		0o644,
	))

	fsys := fstest.MapFS{
		"bundled/demo/skills/example/SKILL.md": {Data: []byte("bundled")},
	}

	err := syncBundledExtensions(fsys, destRoot, []bundledExtension{
		{
			Name:      "demo",
			EmbedDir:  "bundled/demo",
			SourceURL: "https://example.test/demo",
			Ref:       "stable",
			Date:      "2026-03-18",
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(destDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "custom", string(data))
}
