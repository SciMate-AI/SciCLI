package models

const (
	ProviderXAI ModelProvider = "xai"

	XAIGrok4               ModelID = "grok-4"
	XAIGrok41FastReasoning ModelID = "grok-4-1-fast-reasoning"
	XAIGrok41FastChat      ModelID = "grok-4-1-fast-non-reasoning"
	XAIGrok4FastReasoning  ModelID = "grok-4-fast-reasoning"
	XAIGrok4FastChat       ModelID = "grok-4-fast-non-reasoning"
	XAIGrokCodeFast1       ModelID = "grok-code-fast-1"
	XAIGrok3Beta           ModelID = "grok-3-beta"
	XAIGrok3MiniBeta       ModelID = "grok-3-mini-beta"
	XAIGrok3FastBeta       ModelID = "grok-3-fast-beta"
	XAiGrok3MiniFastBeta   ModelID = "grok-3-mini-fast-beta"
)

var XAIModels = map[ModelID]Model{
	XAIGrok4: {
		ID:                  XAIGrok4,
		Name:                "Grok 4",
		Provider:            ProviderXAI,
		APIModel:            "grok-4",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOut:        0,
		CostPer1MOutCached:  0,
		ContextWindow:       256_000,
		DefaultMaxTokens:    20_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	XAIGrok41FastReasoning: {
		ID:                  XAIGrok41FastReasoning,
		Name:                "Grok 4.1 Fast Reasoning",
		Provider:            ProviderXAI,
		APIModel:            "grok-4-1-fast-reasoning",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOut:        0,
		CostPer1MOutCached:  0,
		ContextWindow:       256_000,
		DefaultMaxTokens:    20_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	XAIGrok41FastChat: {
		ID:                  XAIGrok41FastChat,
		Name:                "Grok 4.1 Fast",
		Provider:            ProviderXAI,
		APIModel:            "grok-4-1-fast-non-reasoning",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOut:        0,
		CostPer1MOutCached:  0,
		ContextWindow:       256_000,
		DefaultMaxTokens:    20_000,
		SupportsAttachments: true,
	},
	XAIGrok4FastReasoning: {
		ID:                  XAIGrok4FastReasoning,
		Name:                "Grok 4 Fast Reasoning",
		Provider:            ProviderXAI,
		APIModel:            "grok-4-fast-reasoning",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOut:        0,
		CostPer1MOutCached:  0,
		ContextWindow:       256_000,
		DefaultMaxTokens:    20_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	XAIGrok4FastChat: {
		ID:                  XAIGrok4FastChat,
		Name:                "Grok 4 Fast",
		Provider:            ProviderXAI,
		APIModel:            "grok-4-fast-non-reasoning",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOut:        0,
		CostPer1MOutCached:  0,
		ContextWindow:       256_000,
		DefaultMaxTokens:    20_000,
		SupportsAttachments: true,
	},
	XAIGrokCodeFast1: {
		ID:                  XAIGrokCodeFast1,
		Name:                "Grok Code Fast 1",
		Provider:            ProviderXAI,
		APIModel:            "grok-code-fast-1",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOut:        0,
		CostPer1MOutCached:  0,
		ContextWindow:       256_000,
		DefaultMaxTokens:    20_000,
		SupportsAttachments: true,
	},
	XAIGrok3Beta: {
		ID:                 XAIGrok3Beta,
		Name:               "Grok3 Beta",
		Provider:           ProviderXAI,
		APIModel:           "grok-3-beta",
		CostPer1MIn:        3.0,
		CostPer1MInCached:  0,
		CostPer1MOut:       15,
		CostPer1MOutCached: 0,
		ContextWindow:      131_072,
		DefaultMaxTokens:   20_000,
	},
	XAIGrok3MiniBeta: {
		ID:                 XAIGrok3MiniBeta,
		Name:               "Grok3 Mini Beta",
		Provider:           ProviderXAI,
		APIModel:           "grok-3-mini-beta",
		CostPer1MIn:        0.3,
		CostPer1MInCached:  0,
		CostPer1MOut:       0.5,
		CostPer1MOutCached: 0,
		ContextWindow:      131_072,
		DefaultMaxTokens:   20_000,
	},
	XAIGrok3FastBeta: {
		ID:                 XAIGrok3FastBeta,
		Name:               "Grok3 Fast Beta",
		Provider:           ProviderXAI,
		APIModel:           "grok-3-fast-beta",
		CostPer1MIn:        5,
		CostPer1MInCached:  0,
		CostPer1MOut:       25,
		CostPer1MOutCached: 0,
		ContextWindow:      131_072,
		DefaultMaxTokens:   20_000,
	},
	XAiGrok3MiniFastBeta: {
		ID:                 XAiGrok3MiniFastBeta,
		Name:               "Grok3 Mini Fast Beta",
		Provider:           ProviderXAI,
		APIModel:           "grok-3-mini-fast-beta",
		CostPer1MIn:        0.6,
		CostPer1MInCached:  0,
		CostPer1MOut:       4.0,
		CostPer1MOutCached: 0,
		ContextWindow:      131_072,
		DefaultMaxTokens:   20_000,
	},
}
