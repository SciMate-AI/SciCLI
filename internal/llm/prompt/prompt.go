package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/llm/models"
	"github.com/opencode-ai/opencode/internal/logging"
)

func GetAgentPrompt(agentName config.AgentName, provider models.ModelProvider) string {
	basePrompt := ""
	switch agentName {
	case config.AgentCoder:
		basePrompt = CoderPrompt(provider)
	case config.AgentTitle:
		basePrompt = TitlePrompt(provider)
	case config.AgentTask:
		basePrompt = TaskPrompt(provider)
	case config.AgentSummarizer:
		basePrompt = SummarizerPrompt(provider)
	default:
		basePrompt = "You are a helpful assistant"
	}

	if agentName == config.AgentCoder || agentName == config.AgentTask {
		// Add context from project-specific instruction files if they exist
		contextContent := getContextFromPaths()
		logging.Debug("Context content", "Context", contextContent)
		if contextContent != "" {
			return fmt.Sprintf("%s\n\n# Project-Specific Context\n Make sure to follow the instructions in the context below\n%s", basePrompt, contextContent)
		}
	}
	return basePrompt
}

var (
	onceContext    sync.Once
	contextContent string
)

func getContextFromPaths() string {
	onceContext.Do(func() {
		var (
			cfg          = config.Get()
			workDir      = cfg.WorkingDir
			contextPaths = cfg.ContextPaths
		)

		contextContent = processContextPaths(workDir, contextPaths)
	})

	return contextContent
}

func processContextPaths(workDir string, paths []string) string {
	// Track processed files to avoid duplicates
	processedFiles := make(map[string]bool)
	var processedMutex sync.Mutex
	results := make([]string, 0)

	for _, path := range paths {
		if strings.HasSuffix(path, "/") {
			dirResults := make([]string, 0)
			filepath.WalkDir(filepath.Join(workDir, path), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}

				processedMutex.Lock()
				lowerPath := strings.ToLower(path)
				if processedFiles[lowerPath] {
					processedMutex.Unlock()
					return nil
				}
				processedFiles[lowerPath] = true
				processedMutex.Unlock()

				if result := processFile(path); result != "" {
					dirResults = append(dirResults, result)
				}
				return nil
			})
			sort.Strings(dirResults)
			results = append(results, dirResults...)
			continue
		}

		fullPath := filepath.Join(workDir, path)
		processedMutex.Lock()
		lowerPath := strings.ToLower(fullPath)
		if processedFiles[lowerPath] {
			processedMutex.Unlock()
			continue
		}
		processedFiles[lowerPath] = true
		processedMutex.Unlock()

		if result := processFile(fullPath); result != "" {
			results = append(results, result)
		}
	}

	return strings.Join(results, "\n")
}

func processFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return "# From:" + filePath + "\n" + string(content)
}
