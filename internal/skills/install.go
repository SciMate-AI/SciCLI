package skills

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
)

const userInstalledExtensionName = "user-installed"
const UserInstalledExtensionName = userInstalledExtensionName

func installSkill(source string) (Skill, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return Skill{}, fmt.Errorf("skill source is required")
	}

	var (
		skillDir string
		cleanup  func()
	)

	if localPath, err := resolveLocalSkillPath(source); err == nil {
		skillDir = localPath
	} else {
		githubDir, githubCleanup, githubErr := fetchGitHubSkill(source)
		if githubErr != nil {
			return Skill{}, githubErr
		}
		skillDir = githubDir
		cleanup = githubCleanup
	}
	if cleanup != nil {
		defer cleanup()
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Skill{}, fmt.Errorf("resolve home directory: %w", err)
	}
	destRoot := filepath.Join(homeDir, ".scicli", "extensions", userInstalledExtensionName, "skills")
	name := filepath.Base(skillDir)
	destDir := filepath.Join(destRoot, name)
	if _, err := os.Stat(destDir); err == nil {
		return Skill{}, fmt.Errorf("skill %q is already installed", name)
	} else if !os.IsNotExist(err) {
		return Skill{}, err
	}

	if err := copySkillDir(skillDir, destDir); err != nil {
		return Skill{}, err
	}

	content, err := os.ReadFile(filepath.Join(destDir, skillFileName))
	if err != nil {
		return Skill{}, err
	}
	return Skill{
		ID:          filepath.ToSlash(filepath.Join(userInstalledExtensionName, name)),
		Name:        name,
		Description: describeSkill(string(content), name),
		Path:        filepath.Join(destDir, skillFileName),
		Dir:         destDir,
		Scope:       ScopeExtension,
		Source:      userInstalledExtensionName,
		Content:     string(content),
	}, nil
}

func resolveLocalSkillPath(source string) (string, error) {
	path := source
	if !filepath.IsAbs(path) {
		path = filepath.Join(config.WorkingDirectory(), path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("local skill source must be a directory")
	}
	if _, err := os.Stat(filepath.Join(path, skillFileName)); err != nil {
		return "", fmt.Errorf("directory %q does not contain %s", path, skillFileName)
	}
	return path, nil
}

func fetchGitHubSkill(source string) (string, func(), error) {
	u, err := url.Parse(source)
	if err != nil {
		return "", nil, fmt.Errorf("parse skill source: %w", err)
	}
	if !strings.EqualFold(u.Host, "github.com") {
		return "", nil, fmt.Errorf("unsupported skill source %q: use a local directory or a GitHub tree URL", source)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "tree" {
		return "", nil, fmt.Errorf("GitHub skill source must look like https://github.com/<owner>/<repo>/tree/<ref>/<path>")
	}

	owner := parts[0]
	repo := parts[1]
	ref := parts[3]
	skillPath := filepath.FromSlash(strings.Join(parts[4:], "/"))
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	tempDir, err := os.MkdirTemp("", "scicli-skill-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", ref, repoURL, tempDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("clone skill source: %w: %s", err, strings.TrimSpace(string(output)))
	}

	dir := filepath.Join(tempDir, skillPath)
	if _, err := os.Stat(filepath.Join(dir, skillFileName)); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("skill path %q does not contain %s", skillPath, skillFileName)
	}
	return dir, cleanup, nil
}

func copySkillDir(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(target)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return err
		}
		return dstFile.Chmod(0o644)
	})
}
