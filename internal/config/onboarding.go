package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SciMate-AI/scicli/internal/llm/models"
)

func OnboardingModels(provider models.ModelProvider) []models.Model {
	var providerModels []models.Model
	for _, model := range models.SupportedModels {
		if model.Provider == provider {
			providerModels = append(providerModels, model)
		}
	}

	slices.SortFunc(providerModels, func(a, b models.Model) int {
		if a.Name == b.Name {
			return 0
		}
		if a.Name > b.Name {
			return -1
		}
		return 1
	})

	return providerModels
}

func DefaultOnboardingModel(provider models.ModelProvider) models.ModelID {
	switch provider {
	case models.ProviderCopilot:
		return models.CopilotGPT4o
	case models.ProviderAnthropic:
		return models.Claude4Sonnet
	case models.ProviderOpenAI:
		return models.GPT41
	case models.ProviderGemini:
		return models.Gemini25
	case models.ProviderGROQ:
		return models.QWENQwq
	case models.ProviderOpenRouter:
		return models.OpenRouterClaude37Sonnet
	case models.ProviderXAI:
		return models.XAIGrok3Beta
	case models.ProviderBedrock:
		return models.BedrockClaude37Sonnet
	case models.ProviderVertexAI:
		return models.VertexAIGemini25
	default:
		modelsForProvider := OnboardingModels(provider)
		if len(modelsForProvider) == 0 {
			return ""
		}
		return modelsForProvider[0].ID
	}
}

func SaveOnboardingSelection(provider models.ModelProvider, apiKey string, persistAPIKey bool, modelID models.ModelID) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	model, ok := models.SupportedModels[modelID]
	if !ok {
		return fmt.Errorf("model %s not supported", modelID)
	}
	if model.Provider != provider {
		return fmt.Errorf("model %s does not belong to provider %s", modelID, provider)
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[models.ModelProvider]Provider)
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[AgentName]Agent)
	}

	storedAPIKey := ""
	if persistAPIKey {
		storedAPIKey = strings.TrimSpace(apiKey)
	}

	cfg.Providers[provider] = Provider{
		APIKey:   storedAPIKey,
		Disabled: false,
	}

	agents, err := buildOnboardingAgents(provider, modelID)
	if err != nil {
		return err
	}

	for name, agent := range agents {
		cfg.Agents[name] = agent
	}

	if err := Validate(); err != nil {
		return err
	}

	return updateCfgFile(func(fileCfg *Config) {
		if fileCfg.Providers == nil {
			fileCfg.Providers = make(map[models.ModelProvider]Provider)
		}
		if fileCfg.Agents == nil {
			fileCfg.Agents = make(map[AgentName]Agent)
		}

		fileCfg.Providers[provider] = Provider{
			APIKey:   storedAPIKey,
			Disabled: false,
		}
		for name, agent := range agents {
			fileCfg.Agents[name] = agent
		}
	})
}

func buildOnboardingAgents(provider models.ModelProvider, coderModel models.ModelID) (map[AgentName]Agent, error) {
	if _, ok := models.SupportedModels[coderModel]; !ok {
		return nil, fmt.Errorf("model %s not supported", coderModel)
	}

	taskModel := defaultTaskModel(provider)
	if taskModel == "" {
		taskModel = coderModel
	}

	titleModel := defaultTitleModel(provider)
	if titleModel == "" {
		titleModel = coderModel
	}

	agents := map[AgentName]Agent{
		AgentCoder:      newDefaultAgentConfig(AgentCoder, coderModel),
		AgentSummarizer: newDefaultAgentConfig(AgentSummarizer, coderModel),
		AgentTask:       newDefaultAgentConfig(AgentTask, taskModel),
		AgentTitle:      newDefaultAgentConfig(AgentTitle, titleModel),
	}

	return agents, nil
}

func newDefaultAgentConfig(agentName AgentName, modelID models.ModelID) Agent {
	model := models.SupportedModels[modelID]
	maxTokens := model.DefaultMaxTokens
	if maxTokens <= 0 {
		maxTokens = MaxTokensFallbackDefault
	}
	if agentName == AgentTitle {
		maxTokens = 80
	}

	agent := Agent{
		Model:     modelID,
		MaxTokens: maxTokens,
	}
	if model.CanReason && (model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal) {
		agent.ReasoningEffort = "medium"
	}
	return agent
}

func defaultTaskModel(provider models.ModelProvider) models.ModelID {
	switch provider {
	case models.ProviderOpenAI:
		return models.GPT41Mini
	case models.ProviderGemini:
		return models.Gemini25Flash
	case models.ProviderOpenRouter:
		return models.OpenRouterClaude37Sonnet
	case models.ProviderXAI:
		return models.XAIGrok3Beta
	case models.ProviderVertexAI:
		return models.VertexAIGemini25Flash
	default:
		return DefaultOnboardingModel(provider)
	}
}

func defaultTitleModel(provider models.ModelProvider) models.ModelID {
	switch provider {
	case models.ProviderOpenAI:
		return models.GPT41Mini
	case models.ProviderAnthropic:
		return models.Claude4Sonnet
	case models.ProviderGemini:
		return models.Gemini25Flash
	case models.ProviderOpenRouter:
		return models.OpenRouterClaude35Haiku
	case models.ProviderXAI:
		return models.XAiGrok3MiniFastBeta
	case models.ProviderVertexAI:
		return models.VertexAIGemini25Flash
	default:
		return DefaultOnboardingModel(provider)
	}
}
