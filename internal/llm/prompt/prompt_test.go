package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/opencode-ai/opencode/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetContextFromPaths(t *testing.T) {
	tmpDir := t.TempDir()
	onceContext = sync.Once{}
	contextContent = ""

	_, err := config.Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	cfg := config.Get()
	cfg.WorkingDir = tmpDir
	cfg.ContextPaths = []string{
		"file.txt",
		"directory/",
	}
	testFiles := []string{
		"file.txt",
		"directory/file_a.txt",
		"directory/file_b.txt",
		"directory/file_c.txt",
	}

	createTestFiles(t, tmpDir, testFiles)

	context := getContextFromPaths()
	expectedContext := fmt.Sprintf(
		"# From:%s\nfile.txt: test content\n# From:%s\ndirectory/file_a.txt: test content\n# From:%s\ndirectory/file_b.txt: test content\n# From:%s\ndirectory/file_c.txt: test content",
		filepath.Join(tmpDir, "file.txt"),
		filepath.Join(tmpDir, "directory", "file_a.txt"),
		filepath.Join(tmpDir, "directory", "file_b.txt"),
		filepath.Join(tmpDir, "directory", "file_c.txt"),
	)
	assert.Equal(t, expectedContext, context)
}

func createTestFiles(t *testing.T, tmpDir string, testFiles []string) {
	t.Helper()
	for _, path := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if path[len(path)-1] == '/' {
			err := os.MkdirAll(fullPath, 0755)
			require.NoError(t, err)
		} else {
			dir := filepath.Dir(fullPath)
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
			err = os.WriteFile(fullPath, []byte(path+": test content"), 0644)
			require.NoError(t, err)
		}
	}
}
