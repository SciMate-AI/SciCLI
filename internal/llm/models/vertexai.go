package models

const (
	ProviderVertexAI ModelProvider = "vertexai"

	// Models
	VertexAIGemini31ProPreview       ModelID = "vertexai.gemini-3.1-pro-preview"
	VertexAIGemini31FlashLitePreview ModelID = "vertexai.gemini-3.1-flash-lite-preview"
	VertexAIGemini3ProPreview        ModelID = "vertexai.gemini-3-pro-preview"
	VertexAIGemini3FlashPreview      ModelID = "vertexai.gemini-3-flash-preview"
	VertexAIGemini25Flash            ModelID = "vertexai.gemini-2.5-flash"
	VertexAIGemini25                 ModelID = "vertexai.gemini-2.5"
)

var VertexAIGeminiModels = map[ModelID]Model{
	VertexAIGemini31ProPreview: {
		ID:                  VertexAIGemini31ProPreview,
		Name:                "VertexAI: Gemini 3.1 Pro Preview",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-3.1-pro-preview",
		CostPer1MIn:         GeminiModels[Gemini31ProPreview].CostPer1MIn,
		CostPer1MInCached:   GeminiModels[Gemini31ProPreview].CostPer1MInCached,
		CostPer1MOut:        GeminiModels[Gemini31ProPreview].CostPer1MOut,
		CostPer1MOutCached:  GeminiModels[Gemini31ProPreview].CostPer1MOutCached,
		ContextWindow:       GeminiModels[Gemini31ProPreview].ContextWindow,
		DefaultMaxTokens:    GeminiModels[Gemini31ProPreview].DefaultMaxTokens,
		SupportsAttachments: true,
	},
	VertexAIGemini31FlashLitePreview: {
		ID:                  VertexAIGemini31FlashLitePreview,
		Name:                "VertexAI: Gemini 3.1 Flash Lite Preview",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-3.1-flash-lite-preview",
		CostPer1MIn:         GeminiModels[Gemini31FlashLitePreview].CostPer1MIn,
		CostPer1MInCached:   GeminiModels[Gemini31FlashLitePreview].CostPer1MInCached,
		CostPer1MOut:        GeminiModels[Gemini31FlashLitePreview].CostPer1MOut,
		CostPer1MOutCached:  GeminiModels[Gemini31FlashLitePreview].CostPer1MOutCached,
		ContextWindow:       GeminiModels[Gemini31FlashLitePreview].ContextWindow,
		DefaultMaxTokens:    GeminiModels[Gemini31FlashLitePreview].DefaultMaxTokens,
		SupportsAttachments: true,
	},
	VertexAIGemini3ProPreview: {
		ID:                  VertexAIGemini3ProPreview,
		Name:                "VertexAI: Gemini 3 Pro Preview",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-3-pro-preview",
		CostPer1MIn:         GeminiModels[Gemini3ProPreview].CostPer1MIn,
		CostPer1MInCached:   GeminiModels[Gemini3ProPreview].CostPer1MInCached,
		CostPer1MOut:        GeminiModels[Gemini3ProPreview].CostPer1MOut,
		CostPer1MOutCached:  GeminiModels[Gemini3ProPreview].CostPer1MOutCached,
		ContextWindow:       GeminiModels[Gemini3ProPreview].ContextWindow,
		DefaultMaxTokens:    GeminiModels[Gemini3ProPreview].DefaultMaxTokens,
		SupportsAttachments: true,
	},
	VertexAIGemini3FlashPreview: {
		ID:                  VertexAIGemini3FlashPreview,
		Name:                "VertexAI: Gemini 3 Flash Preview",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-3-flash-preview",
		CostPer1MIn:         GeminiModels[Gemini3FlashPreview].CostPer1MIn,
		CostPer1MInCached:   GeminiModels[Gemini3FlashPreview].CostPer1MInCached,
		CostPer1MOut:        GeminiModels[Gemini3FlashPreview].CostPer1MOut,
		CostPer1MOutCached:  GeminiModels[Gemini3FlashPreview].CostPer1MOutCached,
		ContextWindow:       GeminiModels[Gemini3FlashPreview].ContextWindow,
		DefaultMaxTokens:    GeminiModels[Gemini3FlashPreview].DefaultMaxTokens,
		SupportsAttachments: true,
	},
	VertexAIGemini25Flash: {
		ID:                  VertexAIGemini25Flash,
		Name:                "VertexAI: Gemini 2.5 Flash",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-2.5-flash",
		CostPer1MIn:         GeminiModels[Gemini25Flash].CostPer1MIn,
		CostPer1MInCached:   GeminiModels[Gemini25Flash].CostPer1MInCached,
		CostPer1MOut:        GeminiModels[Gemini25Flash].CostPer1MOut,
		CostPer1MOutCached:  GeminiModels[Gemini25Flash].CostPer1MOutCached,
		ContextWindow:       GeminiModels[Gemini25Flash].ContextWindow,
		DefaultMaxTokens:    GeminiModels[Gemini25Flash].DefaultMaxTokens,
		SupportsAttachments: true,
	},
	VertexAIGemini25: {
		ID:                  VertexAIGemini25,
		Name:                "VertexAI: Gemini 2.5 Pro",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-2.5-pro",
		CostPer1MIn:         GeminiModels[Gemini25].CostPer1MIn,
		CostPer1MInCached:   GeminiModels[Gemini25].CostPer1MInCached,
		CostPer1MOut:        GeminiModels[Gemini25].CostPer1MOut,
		CostPer1MOutCached:  GeminiModels[Gemini25].CostPer1MOutCached,
		ContextWindow:       GeminiModels[Gemini25].ContextWindow,
		DefaultMaxTokens:    GeminiModels[Gemini25].DefaultMaxTokens,
		SupportsAttachments: true,
	},
}
