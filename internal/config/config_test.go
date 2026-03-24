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

	err := SaveOnboardingSelection(OnboardingSelection{
		Provider:      models.ProviderOpenAI,
		APIKey:        "test-key",
		PersistAPIKey: true,
		ModelID:       models.GPT54,
	})
	require.NoError(t, err)

	assert.False(t, NeedsOnboarding())
	assert.Equal(t, "test-key", cfg.Providers[models.ProviderOpenAI].APIKey)
	assert.Equal(t, models.GPT54, cfg.Agents[AgentCoder].Model)
	assert.Equal(t, models.GPT54, cfg.Agents[AgentSummarizer].Model)
	assert.Equal(t, models.GPT5Mini, cfg.Agents[AgentTask].Model)
	assert.Equal(t, models.GPT5Mini, cfg.Agents[AgentTitle].Model)

	savedConfig, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Contains(t, string(savedConfig), `"openai"`)
	assert.Contains(t, string(savedConfig), `"test-key"`)
	assert.Contains(t, string(savedConfig), `"gpt-5.4"`)
}

func TestSaveOnboardingSelectionPersistsOpenAICompatibleProvider(t *testing.T) {
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

	err := SaveOnboardingSelection(OnboardingSelection{
		Provider:      models.ProviderOpenAICompatible,
		APIKey:        "compat-key",
		PersistAPIKey: true,
		ModelID:       models.OpenAICompatibleCustom,
		BaseURL:       "https://example.test/v1",
		CustomModel:   "gpt-5.4",
	})
	require.NoError(t, err)

	assert.False(t, NeedsOnboarding())
	assert.Equal(t, "compat-key", cfg.Providers[models.ProviderOpenAICompatible].APIKey)
	assert.Equal(t, "https://example.test/v1", cfg.Providers[models.ProviderOpenAICompatible].BaseURL)
	assert.Equal(t, "gpt-5.4", cfg.Providers[models.ProviderOpenAICompatible].Model)
	assert.Equal(t, models.OpenAICompatibleCustom, cfg.Agents[AgentCoder].Model)
}

func TestSaveProviderSelectionPersistsProviderAndCoderOnly(t *testing.T) {
	resetConfigTestState()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, ".scicli.json")
	require.NoError(t, os.WriteFile(configFile, []byte("{}"), 0o644))

	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	cfg = &Config{
		Providers: make(map[models.ModelProvider]Provider),
		Agents: map[AgentName]Agent{
			AgentCoder:      newDefaultAgentConfig(AgentCoder, models.Claude4Sonnet),
			AgentSummarizer: newDefaultAgentConfig(AgentSummarizer, models.Claude4Sonnet),
			AgentTask:       newDefaultAgentConfig(AgentTask, models.Claude4Sonnet),
			AgentTitle:      newDefaultAgentConfig(AgentTitle, models.Claude4Sonnet),
		},
		LSP:        make(map[string]LSPConfig),
		MCPServers: make(map[string]MCPServer),
	}

	err := SaveProviderSelection(OnboardingSelection{
		Provider:      models.ProviderOpenAI,
		APIKey:        "later-key",
		PersistAPIKey: true,
		ModelID:       models.GPT54,
	})
	require.NoError(t, err)

	assert.Equal(t, "later-key", cfg.Providers[models.ProviderOpenAI].APIKey)
	assert.Equal(t, models.GPT54, cfg.Agents[AgentCoder].Model)
	assert.Equal(t, models.Claude4Sonnet, cfg.Agents[AgentSummarizer].Model)
	assert.Equal(t, models.Claude4Sonnet, cfg.Agents[AgentTask].Model)
	assert.Equal(t, models.Claude4Sonnet, cfg.Agents[AgentTitle].Model)

	savedConfig, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Contains(t, string(savedConfig), `"openai"`)
	assert.Contains(t, string(savedConfig), `"later-key"`)
	assert.Contains(t, string(savedConfig), `"gpt-5.4"`)
}

func TestProviderReadyRecognizesConfiguredOpenAICompatible(t *testing.T) {
	resetConfigTestState()
	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderOpenAICompatible: {
				BaseURL: "https://example.test/v1",
				Model:   "custom-model",
			},
		},
	}

	assert.True(t, ProviderReady(models.ProviderOpenAICompatible))
	assert.False(t, ProviderReady(models.ProviderOpenAI))
}

func TestSetSkillDisabledPersistsConfig(t *testing.T) {
	resetConfigTestState()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, ".scicli.json")
	require.NoError(t, os.WriteFile(configFile, []byte("{}"), 0o644))

	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	cfg = &Config{}
	require.NoError(t, SetSkillDisabled("chem/analyze", true))
	assert.Equal(t, []string{"chem/analyze"}, cfg.Skills.Disabled)

	require.NoError(t, SetSkillDisabled("chem/analyze", false))
	assert.Empty(t, cfg.Skills.Disabled)
}

func TestApplyRuntimeOverridesUpdatesAutomationAndPermissions(t *testing.T) {
	resetConfigTestState()
	cfg = &Config{}

	err := ApplyRuntimeOverrides("ultrawork", true, []string{"git status", "go test"})
	require.NoError(t, err)

	assert.Equal(t, WorkModeUltrawork, cfg.Automation.WorkMode)
	assert.True(t, cfg.Permissions.AutoApprove)
	assert.Equal(t, []string{"git status", "go test"}, cfg.Permissions.AllowCommandPrefixes)
}

func TestSetWorkModeRuntimeOnly(t *testing.T) {
	resetConfigTestState()
	cfg = &Config{}

	require.NoError(t, SetWorkMode(WorkModeAuto, false))
	assert.Equal(t, WorkModeAuto, cfg.Automation.WorkMode)
}

func TestSetDefaultsDoesNotInjectBuiltInMCPServers(t *testing.T) {
	resetConfigTestState()

	setDefaults(false)

	assert.Empty(t, viper.GetStringMap("mcpServers"))
}

func resetConfigTestState() {
	cfg = nil
	viper.Reset()
}
