package chat

import (
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/models"
)

func truncateString(value string, width int) string {
	if width <= 0 || len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func activeProviderAndModel() (string, string) {
	cfg := config.Get()
	providerLine := "provider: unconfigured"
	modelLine := "model: unavailable"
	if cfg == nil {
		return providerLine, modelLine
	}

	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return providerLine, modelLine
	}
	model, ok := models.SupportedModels[agentCfg.Model]
	if !ok {
		return providerLine, modelLine
	}
	providerLine = "provider: " + string(model.Provider)
	modelLine = "model: " + model.Name
	if providerCfg, ok := cfg.Providers[model.Provider]; ok {
		if strings.TrimSpace(providerCfg.BaseURL) != "" {
			providerLine += " @ " + providerCfg.BaseURL
		}
		if model.Provider == models.ProviderOpenAICompatible && strings.TrimSpace(providerCfg.Model) != "" {
			modelLine = "model: " + providerCfg.Model
		}
	}
	return providerLine, modelLine
}

func activeModelShellLabel(width int) string {
	providerLine, modelLine := activeProviderAndModel()
	provider := strings.TrimPrefix(providerLine, "provider: ")
	model := strings.TrimPrefix(modelLine, "model: ")
	label := provider + " / " + model
	return truncateString(label, max(18, width))
}
