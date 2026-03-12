package agent

import (
	"encoding/json"
	"strings"
	"testing"

	toolsPkg "github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/message"
)

func TestCompactToolResponseForModelContextPreservesExistingMetadata(t *testing.T) {
	response := toolsPkg.WithResponseMetadata(
		toolsPkg.NewTextResponse(strings.Repeat("line\n", 2000)),
		map[string]any{
			"start_time": 1,
			"end_time":   2,
		},
	)

	compacted := compactToolResponseForModelContext("bash", response)
	if compacted.Content == response.Content {
		t.Fatal("expected large tool output to be compacted")
	}

	var metadata map[string]any
	if err := json.Unmarshal([]byte(compacted.Metadata), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}

	if metadata["start_time"] != float64(1) || metadata["end_time"] != float64(2) {
		t.Fatalf("expected existing metadata to be preserved, got %+v", metadata)
	}
	if _, ok := metadata["raw_content"].(string); !ok {
		t.Fatalf("expected raw_content to be preserved in metadata, got %+v", metadata)
	}
	if metadata["compacted"] != true {
		t.Fatalf("expected compacted flag to be true, got %+v", metadata)
	}
}

func TestSanitizeLoopDecisionStripsControlTag(t *testing.T) {
	msg := message.Message{
		Parts: []message.ContentPart{
			message.TextContent{Text: "<agent_loop_status>continue</agent_loop_status>\nNeed one more check."},
		},
	}

	decision := sanitizeLoopDecision(&msg)
	if decision != loopDecisionContinue {
		t.Fatalf("expected continue decision, got %s", decision)
	}
	if strings.Contains(msg.Content().Text, "agent_loop_status") {
		t.Fatalf("expected control tag to be stripped, got %q", msg.Content().Text)
	}
}

func TestFindHistoryCompactionBoundaryAlignsToUserMessage(t *testing.T) {
	history := []message.Message{
		{Role: message.User},
		{Role: message.Assistant},
		{Role: message.Tool},
		{Role: message.Assistant},
		{Role: message.User},
		{Role: message.Assistant},
		{Role: message.Tool},
		{Role: message.Assistant},
		{Role: message.User},
		{Role: message.Assistant},
	}

	boundary := findHistoryCompactionBoundary(history, 4)
	if boundary != 4 {
		t.Fatalf("expected boundary to align to latest user message before tail, got %d", boundary)
	}
}

func TestHistoryNeedsCompactionWhenEstimatedUsageNearWindow(t *testing.T) {
	history := []message.Message{
		{
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: strings.Repeat("x", 5000)},
			},
		},
	}

	if !historyNeedsCompaction(history, 1500, 400) {
		t.Fatal("expected history to require compaction near the context window")
	}
}
