package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SciMate-AI/scicli/internal/logging"
)

const bundledVersionFile = ".scicli-bundled-version"

type bundledExtension struct {
	Name      string
	EmbedDir  string
	SourceURL string
	Ref       string
	Date      string
}

var bundledExtensions = []bundledExtension{
	{
		Name:      "claude-scientific-skills",
		EmbedDir:  "bundled/claude-scientific-skills",
		SourceURL: "https://github.com/K-Dense-AI/claude-scientific-skills",
		Ref:       "575f1e586f6b484feb3cba040b6e29f2a56f3c74",
		Date:      "2026-03-11",
	},
	{
		Name:      "hpc-skills",
		EmbedDir:  "bundled/hpc-skills",
		SourceURL: "https://github.com/SciMate-AI/HPC-Skills",
		Ref:       "54307828d30ef4116017a9270ea26fad4f413d5b",
		Date:      "2026-03-17",
	},
}

//go:embed bundled
var bundledFS embed.FS

func EnsureBundledExtensions() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	if strings.TrimSpace(homeDir) == "" {
		return nil
	}

	destRoot := filepath.Join(homeDir, ".scicli", "extensions")
	if err := syncBundledExtensions(bundledFS, destRoot, bundledExtensions); err != nil {
		return err
	}
	return nil
}

func syncBundledExtensions(fsys fs.FS, destRoot string, bundles []bundledExtension) error {
	for _, bundle := range bundles {
		destDir := filepath.Join(destRoot, bundle.Name)
		versionPath := filepath.Join(destDir, bundledVersionFile)

		currentVersion, err := os.ReadFile(versionPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read bundle version for %s: %w", bundle.Name, err)
		}
		overwrite := strings.TrimSpace(string(currentVersion)) != bundle.versionString()

		if err := copyBundleFiles(fsys, bundle, destDir, overwrite); err != nil {
			return err
		}
	}

	return nil
}

func copyBundleFiles(fsys fs.FS, bundle bundledExtension, destDir string, overwrite bool) error {
	if err := fs.WalkDir(fsys, bundle.EmbedDir, func(entryPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath := strings.TrimPrefix(strings.TrimPrefix(entryPath, bundle.EmbedDir), "/")
		if relPath == "." {
			return os.MkdirAll(destDir, 0o755)
		}
		if relPath == "" {
			return os.MkdirAll(destDir, 0o755)
		}

		targetPath := filepath.Join(destDir, filepath.FromSlash(relPath))
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		if !overwrite {
			if _, err := os.Stat(targetPath); err == nil {
				return nil
			} else if !os.IsNotExist(err) {
				return err
			}
		}

		data, err := fs.ReadFile(fsys, entryPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("copy bundled extension %s: %w", bundle.Name, err)
	}

	versionPath := filepath.Join(destDir, bundledVersionFile)
	if err := os.WriteFile(versionPath, []byte(bundle.versionString()+"\n"), 0o644); err != nil {
		return fmt.Errorf("write bundle version for %s: %w", bundle.Name, err)
	}

	logging.Debug("synced bundled extension skills", "extension", bundle.Name, "ref", bundle.Ref)
	return nil
}

func (b bundledExtension) versionString() string {
	return fmt.Sprintf("%s %s %s", b.SourceURL, b.Ref, b.Date)
}
