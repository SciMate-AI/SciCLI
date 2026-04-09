package provider

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseFromCompletionErrorsOnEmptyChoices(t *testing.T) {
	t.Parallel()

	client := &openaiClient{
		providerOptions: providerClientOptions{
			model: models.Model{
				Provider: models.ProviderOpenAICompatible,
				APIModel: "gpt-5.4",
			},
		},
	}

	response, err := client.responseFromCompletion(openai.ChatCompletion{})
	require.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "no choices")
}

func TestResponseFromAccumulatedStreamFallsBackToContentWithoutChoices(t *testing.T) {
	t.Parallel()

	client := &openaiClient{
		providerOptions: providerClientOptions{
			model: models.Model{
				Provider: models.ProviderOpenAICompatible,
				APIModel: "gpt-5.4",
			},
		},
	}

	response, err := client.responseFromAccumulatedStream(openai.ChatCompletion{}, "partial answer")
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, "partial answer", response.Content)
	assert.Equal(t, message.FinishReasonUnknown, response.FinishReason)
}
