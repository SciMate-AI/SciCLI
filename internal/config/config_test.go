package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeLSPAliasesMigratesLegacyGoKey(t *testing.T) {
	resetConfigTestState()
	cfg = &Config{
		LSP: map[string]LSPConfig{
			"gopls": {Command: "gopls"},
		},
	}

	normalizeLSPAliases()

	_, hasLegacy := cfg.LSP["gopls"]
	goConfig, hasGo := cfg.LSP["go"]
	assert.False(t, hasLegacy)
	assert.True(t, hasGo)
	assert.Equal(t, "gopls", goConfig.Command)
}

func TestNeedsOnboardingWithoutCoderAgent(t *testing.T) {
	resetConfigTestState()
	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{},
		Agents:    map[AgentName]Agent{},
	}

	assert.True(t, NeedsOnboarding())
}

func TestSaveOnboardingSelectionPersistsGlobalConfig(t *testing.T) {
	resetConfigTestState()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, ".scicli.json")
	require.NoError(t, os.WriteFile(configFile, []byte("{}"), 0o644))

	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	cfg = &Config{
		Providers:  make(map[models.ModelProvider]Provider),
		Agents:     make(map[AgentName]Agent),
		LSP:        make(map[string]LSPConfig),
		MCPServers: make(map[string]MCPServer),
	}

	err := SaveOnboardingSelection(models.ProviderOpenAI, "test-key", true, models.GPT41)
	require.NoError(t, err)

	assert.False(t, NeedsOnboarding())
	assert.Equal(t, "test-key", cfg.Providers[models.ProviderOpenAI].APIKey)
	assert.Equal(t, models.GPT41, cfg.Agents[AgentCoder].Model)
	assert.Equal(t, models.GPT41, cfg.Agents[AgentSummarizer].Model)
	assert.Equal(t, models.GPT41Mini, cfg.Agents[AgentTask].Model)
	assert.Equal(t, models.GPT41Mini, cfg.Agents[AgentTitle].Model)

	savedConfig, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Contains(t, string(savedConfig), `"openai"`)
	assert.Contains(t, string(savedConfig), `"test-key"`)
	assert.Contains(t, string(savedConfig), `"gpt-4.1"`)
}

func resetConfigTestState() {
	cfg = nil
	viper.Reset()
}
