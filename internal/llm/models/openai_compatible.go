package models

const (
	ProviderOpenAICompatible ModelProvider = "openai-compatible"

	OpenAICompatibleCustom ModelID = "openai-compatible.custom"
)

var OpenAICompatibleModels = map[ModelID]Model{
	OpenAICompatibleCustom: {
		ID:                  OpenAICompatibleCustom,
		Name:                "OpenAI-compatible custom model",
		Provider:            ProviderOpenAICompatible,
		APIModel:            "custom",
		ContextWindow:       128_000,
		DefaultMaxTokens:    16_384,
		SupportsAttachments: true,
	},
}
