package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SciMate-AI/scicli/internal/llm/models"
)

type OnboardingSelection struct {
	Provider      models.ModelProvider
	APIKey        string
	PersistAPIKey bool
	ModelID       models.ModelID
	BaseURL       string
	CustomModel   string
}

var curatedOnboardingModels = map[models.ModelProvider][]models.ModelID{
	models.ProviderCopilot: {
		models.CopilotGPT54,
		models.CopilotGPT5Mini,
		models.CopilotClaudeOpus41,
		models.CopilotClaudeSonnet45,
		models.CopilotGemini31Pro,
		models.CopilotGrokCodeFast1,
	},
	models.ProviderAnthropic: {
		models.Claude4Sonnet,
		models.Claude4Opus,
		models.Claude37Sonnet,
		models.Claude35Sonnet,
		models.Claude35Haiku,
		models.Claude3Haiku,
	},
	models.ProviderOpenAI: {
		models.GPT54,
		models.GPT54Pro,
		models.GPT52,
		models.GPT52Pro,
		models.GPT5Mini,
		models.GPT5Nano,
	},
	models.ProviderGemini: {
		models.Gemini31ProPreview,
		models.Gemini31FlashLitePreview,
		models.Gemini3ProPreview,
		models.Gemini3FlashPreview,
		models.Gemini25,
		models.Gemini25Flash,
	},
	models.ProviderOpenRouter: {
		models.OpenRouterGPT54,
		models.OpenRouterGPT54Pro,
		models.OpenRouterGPT5Mini,
		models.OpenRouterGemini31Pro,
		models.OpenRouterClaudeSonnet45,
		models.OpenRouterGrokCodeFast1,
	},
	models.ProviderGROQ: {
		models.GPTOSS120B,
		models.GPTOSS20B,
		models.Qwen3_32B,
		models.Llama4Maverick,
		models.Llama4Scout,
		models.DeepseekR1DistillLlama70b,
	},
	models.ProviderXAI: {
		models.XAIGrok4,
		models.XAIGrok41FastReasoning,
		models.XAIGrok41FastChat,
		models.XAIGrok4FastReasoning,
		models.XAIGrok4FastChat,
		models.XAIGrokCodeFast1,
	},
	models.ProviderBedrock: {
		models.BedrockClaude37Sonnet,
	},
	models.ProviderVertexAI: {
		models.VertexAIGemini31ProPreview,
		models.VertexAIGemini31FlashLitePreview,
		models.VertexAIGemini3ProPreview,
		models.VertexAIGemini3FlashPreview,
		models.VertexAIGemini25,
		models.VertexAIGemini25Flash,
	},
	models.ProviderOpenAICompatible: {
		models.OpenAICompatibleCustom,
	},
}

func OnboardingModels(provider models.ModelProvider) []models.Model {
	curatedIDs, hasCurated := curatedOnboardingModels[provider]
	if !hasCurated {
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

	curated := make([]models.Model, 0, len(curatedIDs))
	for _, id := range curatedIDs {
		model, ok := models.SupportedModels[id]
		if ok && model.Provider == provider {
			curated = append(curated, model)
		}
	}
	return curated
}

func DefaultOnboardingModel(provider models.ModelProvider) models.ModelID {
	switch provider {
	case models.ProviderCopilot:
		return models.CopilotGPT54
	case models.ProviderAnthropic:
		return models.Claude4Sonnet
	case models.ProviderOpenAI:
		return models.GPT54
	case models.ProviderGemini:
		return models.Gemini31ProPreview
	case models.ProviderGROQ:
		return models.GPTOSS120B
	case models.ProviderOpenRouter:
		return models.OpenRouterGPT54
	case models.ProviderXAI:
		return models.XAIGrok4
	case models.ProviderBedrock:
		return models.BedrockClaude37Sonnet
	case models.ProviderVertexAI:
		return models.VertexAIGemini31ProPreview
	case models.ProviderOpenAICompatible:
		return models.OpenAICompatibleCustom
	default:
		modelsForProvider := OnboardingModels(provider)
		if len(modelsForProvider) == 0 {
			return ""
		}
		return modelsForProvider[0].ID
	}
}

func SaveOnboardingSelection(selection OnboardingSelection) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	if _, err := validateOnboardingSelection(selection); err != nil {
		return err
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[models.ModelProvider]Provider)
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[AgentName]Agent)
	}

	providerCfg := providerConfigFromSelection(selection)
	cfg.Providers[selection.Provider] = providerCfg

	agents, err := buildOnboardingAgents(selection.Provider, selection.ModelID)
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

		fileCfg.Providers[selection.Provider] = providerCfg
		for name, agent := range agents {
			fileCfg.Agents[name] = agent
		}
	})
}

func SaveProviderSelection(selection OnboardingSelection) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	if _, err := validateOnboardingSelection(selection); err != nil {
		return err
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[models.ModelProvider]Provider)
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[AgentName]Agent)
	}

	providerCfg := providerConfigFromSelection(selection)
	cfg.Providers[selection.Provider] = providerCfg
	updatedAgents := map[AgentName]Agent{
		AgentCoder: newDefaultAgentConfig(AgentCoder, selection.ModelID),
	}
	if cfg.TUI.LinkAllAgentModels {
		updatedAgents = make(map[AgentName]Agent, len(linkedAgentNames()))
		for _, agentName := range linkedAgentNames() {
			updatedAgents[agentName] = newDefaultAgentConfig(agentName, selection.ModelID)
		}
	}
	for agentName, agentCfg := range updatedAgents {
		cfg.Agents[agentName] = agentCfg
	}

	if !providerConfigReady(selection.Provider, providerCfg) {
		return fmt.Errorf("provider %s configuration is incomplete", selection.Provider)
	}

	return updateCfgFile(func(fileCfg *Config) {
		if fileCfg.Providers == nil {
			fileCfg.Providers = make(map[models.ModelProvider]Provider)
		}
		if fileCfg.Agents == nil {
			fileCfg.Agents = make(map[AgentName]Agent)
		}

		fileCfg.Providers[selection.Provider] = providerCfg
		for agentName, agentCfg := range updatedAgents {
			fileCfg.Agents[agentName] = agentCfg
		}
	})
}

func validateOnboardingSelection(selection OnboardingSelection) (models.Model, error) {
	model, ok := models.SupportedModels[selection.ModelID]
	if !ok {
		return models.Model{}, fmt.Errorf("model %s not supported", selection.ModelID)
	}
	if model.Provider != selection.Provider {
		return models.Model{}, fmt.Errorf("model %s does not belong to provider %s", selection.ModelID, selection.Provider)
	}

	if selection.Provider == models.ProviderOpenAICompatible {
		if strings.TrimSpace(selection.BaseURL) == "" {
			return models.Model{}, fmt.Errorf("base url is required for %s", selection.Provider)
		}
		if strings.TrimSpace(selection.CustomModel) == "" {
			return models.Model{}, fmt.Errorf("model is required for %s", selection.Provider)
		}
	}

	return model, nil
}

func providerConfigFromSelection(selection OnboardingSelection) Provider {
	storedAPIKey := ""
	if selection.PersistAPIKey {
		storedAPIKey = strings.TrimSpace(selection.APIKey)
	}

	return Provider{
		APIKey:   storedAPIKey,
		BaseURL:  strings.TrimSpace(selection.BaseURL),
		Model:    strings.TrimSpace(selection.CustomModel),
		Disabled: false,
	}
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
	if model.CanReason && (model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal || model.Provider == models.ProviderOpenAICompatible) {
		agent.ReasoningEffort = "medium"
	}
	return agent
}

func defaultTaskModel(provider models.ModelProvider) models.ModelID {
	switch provider {
	case models.ProviderOpenAI:
		return models.GPT5Mini
	case models.ProviderGemini:
		return models.Gemini3FlashPreview
	case models.ProviderOpenRouter:
		return models.OpenRouterGPT5Mini
	case models.ProviderXAI:
		return models.XAIGrokCodeFast1
	case models.ProviderVertexAI:
		return models.VertexAIGemini31FlashLitePreview
	default:
		return DefaultOnboardingModel(provider)
	}
}

func defaultTitleModel(provider models.ModelProvider) models.ModelID {
	switch provider {
	case models.ProviderOpenAI:
		return models.GPT5Mini
	case models.ProviderAnthropic:
		return models.Claude4Sonnet
	case models.ProviderGemini:
		return models.Gemini25Flash
	case models.ProviderOpenRouter:
		return models.OpenRouterGPT5Mini
	case models.ProviderXAI:
		return models.XAIGrok41FastChat
	case models.ProviderVertexAI:
		return models.VertexAIGemini31FlashLitePreview
	default:
		return DefaultOnboardingModel(provider)
	}
}
