package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/SciMate-AI/scicli/internal/config"
)

const skillFileName = "SKILL.md"

type Scope string

const (
	ScopeWorkspace Scope = "workspace"
	ScopeUser      Scope = "user"
	ScopeExtension Scope = "extension"
	ScopeCustom    Scope = "custom"
)

type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Dir         string `json:"dir"`
	Scope       Scope  `json:"scope"`
	Source      string `json:"source,omitempty"`
	Content     string `json:"-"`
}

type Service interface {
	List(context.Context) ([]Skill, error)
	Recommend(context.Context, string, int) ([]Skill, error)
	Activate(context.Context, string, string) (Skill, error)
	Install(context.Context, string) (Skill, error)
	Uninstall(context.Context, string) error
	Active(string) []Skill
}

type service struct {
	mu      sync.RWMutex
	active  map[string]map[string]Skill
	workDir string
}

func NewService() Service {
	return &service{
		active:  make(map[string]map[string]Skill),
		workDir: config.WorkingDirectory(),
	}
}

func (s *service) List(_ context.Context) ([]Skill, error) {
	return discoverSkills(s.workDir)
}

func (s *service) Activate(ctx context.Context, sessionID, name string) (Skill, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Skill{}, fmt.Errorf("session ID is required")
	}
	skills, err := s.List(ctx)
	if err != nil {
		return Skill{}, err
	}
	skill, err := resolveSkill(skills, name)
	if err != nil {
		return Skill{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active[sessionID] == nil {
		s.active[sessionID] = make(map[string]Skill)
	}
	s.active[sessionID][skill.ID] = skill
	return skill, nil
}

func (s *service) Recommend(ctx context.Context, query string, limit int) ([]Skill, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	return recommendSkills(items, query, limit), nil
}

func (s *service) Install(_ context.Context, source string) (Skill, error) {
	return installSkill(source)
}

func (s *service) Uninstall(ctx context.Context, name string) error {
	items, err := s.List(ctx)
	if err != nil {
		return err
	}
	skill, err := resolveSkill(items, name)
	if err != nil {
		return err
	}
	if skill.Source != userInstalledExtensionName {
		return fmt.Errorf("only skills installed under %q can be removed", userInstalledExtensionName)
	}
	return os.RemoveAll(skill.Dir)
}

func (s *service) Active(sessionID string) []Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionSkills := s.active[sessionID]
	if len(sessionSkills) == 0 {
		return nil
	}

	ids := make([]string, 0, len(sessionSkills))
	for id := range sessionSkills {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	out := make([]Skill, 0, len(ids))
	for _, id := range ids {
		out = append(out, sessionSkills[id])
	}
	return out
}

func discoverSkills(workDir string) ([]Skill, error) {
	cfg := config.Get()
	disabled := map[string]struct{}{}
	if cfg != nil {
		for _, id := range cfg.Skills.Disabled {
			disabled[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
		}
	}

	roots := defaultRoots(workDir)
	if cfg != nil {
		for _, path := range cfg.Skills.Paths {
			roots = append(roots, skillRoot{
				Path:  path,
				Scope: ScopeCustom,
			})
		}
	}

	seenRoots := map[string]struct{}{}
	seenIDs := map[string]struct{}{}
	skills := make([]Skill, 0)

	for _, root := range roots {
		expandedRoots := expandRoot(workDir, root)
		for _, expanded := range expandedRoots {
			key := strings.ToLower(filepath.Clean(expanded.Path))
			if _, ok := seenRoots[key]; ok {
				continue
			}
			seenRoots[key] = struct{}{}

			items, err := loadSkillsFromRoot(expanded)
			if err != nil {
				continue
			}
			for _, skill := range items {
				if _, skip := disabled[strings.ToLower(skill.ID)]; skip {
					continue
				}
				if _, skip := disabled[strings.ToLower(skill.Name)]; skip {
					continue
				}
				if _, exists := seenIDs[strings.ToLower(skill.ID)]; exists {
					continue
				}
				seenIDs[strings.ToLower(skill.ID)] = struct{}{}
				skills = append(skills, skill)
			}
		}
	}

	slices.SortFunc(skills, func(a, b Skill) int {
		return strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
	})
	return skills, nil
}

type skillRoot struct {
	Path          string
	Scope         Scope
	ExtensionName string
}

func defaultRoots(workDir string) []skillRoot {
	families := []string{".scicli", ".gemini", ".claude"}
	roots := make([]skillRoot, 0, len(families)*4)

	for _, family := range families {
		roots = append(roots, skillRoot{
			Path:  filepath.Join(workDir, family, "skills"),
			Scope: ScopeWorkspace,
		})
	}

	if homeDir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		for _, family := range families {
			roots = append(roots,
				skillRoot{Path: filepath.Join(homeDir, family, "skills"), Scope: ScopeUser},
				skillRoot{Path: filepath.Join(homeDir, family, "extensions"), Scope: ScopeExtension},
			)
		}
	}

	for _, family := range families {
		roots = append(roots, skillRoot{
			Path:  filepath.Join(workDir, family, "extensions"),
			Scope: ScopeExtension,
		})
	}
	return roots
}

func expandRoot(workDir string, root skillRoot) []skillRoot {
	path := root.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	root.Path = filepath.Clean(path)

	if root.Scope != ScopeExtension {
		return []skillRoot{root}
	}

	entries, err := os.ReadDir(root.Path)
	if err != nil {
		return nil
	}

	out := make([]skillRoot, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		out = append(out, skillRoot{
			Path:          filepath.Join(root.Path, entry.Name(), "skills"),
			Scope:         ScopeExtension,
			ExtensionName: entry.Name(),
		})
	}
	return out
}

func loadSkillsFromRoot(root skillRoot) ([]Skill, error) {
	entries, err := os.ReadDir(root.Path)
	if err != nil {
		return nil, err
	}

	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root.Path, entry.Name())
		skillPath := filepath.Join(dir, skillFileName)
		content, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}

		name := entry.Name()
		id := name
		if root.Scope == ScopeExtension && root.ExtensionName != "" {
			id = filepath.ToSlash(filepath.Join(root.ExtensionName, name))
		}

		skills = append(skills, Skill{
			ID:          id,
			Name:        name,
			Description: describeSkill(string(content), name),
			Path:        skillPath,
			Dir:         dir,
			Scope:       root.Scope,
			Source:      root.ExtensionName,
			Content:     string(content),
		})
	}
	return skills, nil
}

func describeSkill(content, fallback string) string {
	lines := strings.Split(content, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		return truncate(line, 160)
	}
	return fallback
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max-3]) + "..."
}

func resolveSkill(skills []Skill, name string) (Skill, error) {
	query := strings.TrimSpace(name)
	if query == "" {
		return Skill{}, fmt.Errorf("skill_name is required")
	}

	lowerQuery := strings.ToLower(query)
	for _, skill := range skills {
		if strings.ToLower(skill.ID) == lowerQuery {
			return skill, nil
		}
	}

	matches := make([]Skill, 0)
	for _, skill := range skills {
		if strings.ToLower(skill.Name) == lowerQuery {
			matches = append(matches, skill)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		ids := make([]string, 0, len(matches))
		for _, skill := range matches {
			ids = append(ids, skill.ID)
		}
		return Skill{}, fmt.Errorf("skill name %q is ambiguous; use one of: %s", query, strings.Join(ids, ", "))
	}
	return Skill{}, fmt.Errorf("skill %q not found", query)
}

func recommendSkills(skills []Skill, query string, limit int) []Skill {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || len(skills) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}

	type scoredSkill struct {
		skill Skill
		score int
	}

	tokens := tokenizeSkillQuery(query)
	scored := make([]scoredSkill, 0, len(skills))
	for _, skill := range skills {
		score := scoreSkill(skill, query, tokens)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredSkill{skill: skill, score: score})
	}

	slices.SortFunc(scored, func(a, b scoredSkill) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(strings.ToLower(a.skill.ID), strings.ToLower(b.skill.ID))
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}

	out := make([]Skill, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.skill)
	}
	return out
}

func tokenizeSkillQuery(query string) []string {
	query = strings.NewReplacer("/", " ", "-", " ", "_", " ", ":", " ").Replace(query)
	fields := strings.Fields(query)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		out = append(out, field)
	}
	return out
}

func scoreSkill(skill Skill, query string, tokens []string) int {
	name := strings.ToLower(skill.Name)
	id := strings.ToLower(skill.ID)
	description := strings.ToLower(skill.Description)

	score := 0
	if strings.Contains(query, id) {
		score += 120
	}
	if strings.Contains(query, name) {
		score += 100
	}
	for _, token := range tokens {
		if token == name || token == id {
			score += 70
		}
		if strings.Contains(name, token) {
			score += 30
		}
		if strings.Contains(id, token) {
			score += 24
		}
		if strings.Contains(description, token) {
			score += 10
		}
	}
	return score
}
