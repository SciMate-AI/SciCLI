package models

const (
	ProviderGemini ModelProvider = "gemini"

	// Models
	Gemini31ProPreview       ModelID = "gemini-3.1-pro-preview"
	Gemini31FlashLitePreview ModelID = "gemini-3.1-flash-lite-preview"
	Gemini3ProPreview        ModelID = "gemini-3-pro-preview"
	Gemini3FlashPreview      ModelID = "gemini-3-flash-preview"
	Gemini25Flash            ModelID = "gemini-2.5-flash"
	Gemini25FlashLite        ModelID = "gemini-2.5-flash-lite"
	Gemini25                 ModelID = "gemini-2.5"
	Gemini20Flash            ModelID = "gemini-2.0-flash"
	Gemini20FlashLite        ModelID = "gemini-2.0-flash-lite"
)

var GeminiModels = map[ModelID]Model{
	Gemini31ProPreview: {
		ID:                  Gemini31ProPreview,
		Name:                "Gemini 3.1 Pro Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3.1-pro-preview",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
	Gemini31FlashLitePreview: {
		ID:                  Gemini31FlashLitePreview,
		Name:                "Gemini 3.1 Flash Lite Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3.1-flash-lite-preview",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
	Gemini3ProPreview: {
		ID:                  Gemini3ProPreview,
		Name:                "Gemini 3 Pro Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3-pro-preview",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
	Gemini3FlashPreview: {
		ID:                  Gemini3FlashPreview,
		Name:                "Gemini 3 Flash Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3-flash-preview",
		CostPer1MIn:         0,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
	Gemini25Flash: {
		ID:                  Gemini25Flash,
		Name:                "Gemini 2.5 Flash",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-flash",
		CostPer1MIn:         0.15,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0.60,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
	Gemini25: {
		ID:                  Gemini25,
		Name:                "Gemini 2.5 Pro",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-pro",
		CostPer1MIn:         1.25,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        10,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
	Gemini25FlashLite: {
		ID:                  Gemini25FlashLite,
		Name:                "Gemini 2.5 Flash Lite",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-flash-lite",
		CostPer1MIn:         0.10,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0.40,
		ContextWindow:       1000000,
		DefaultMaxTokens:    6000,
		SupportsAttachments: true,
	},

	Gemini20Flash: {
		ID:                  Gemini20Flash,
		Name:                "Gemini 2.0 Flash",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.0-flash",
		CostPer1MIn:         0.10,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0.40,
		ContextWindow:       1000000,
		DefaultMaxTokens:    6000,
		SupportsAttachments: true,
	},
	Gemini20FlashLite: {
		ID:                  Gemini20FlashLite,
		Name:                "Gemini 2.0 Flash Lite",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.0-flash-lite",
		CostPer1MIn:         0.05,
		CostPer1MInCached:   0,
		CostPer1MOutCached:  0,
		CostPer1MOut:        0.30,
		ContextWindow:       1000000,
		DefaultMaxTokens:    6000,
		SupportsAttachments: true,
	},
}
