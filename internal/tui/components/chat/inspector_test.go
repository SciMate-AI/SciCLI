package chat

import (
	"strings"
	"testing"

	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/taskrun"
)

func TestDeriveRunStatusRunningWhenAssistantUnfinished(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do work"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "partial"}}},
	}

	status, detail := deriveRunStatus(msgs)
	if status != taskrun.StatusRunning {
		t.Fatalf("expected running, got %q", status)
	}
	if detail == "" {
		t.Fatalf("expected detail")
	}
}

func TestDeriveRunStatusCompleteWhenAssistantEndsTurn(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "finish"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.TextContent{Text: "done"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		}},
	}

	status, _ := deriveRunStatus(msgs)
	if status != taskrun.StatusComplete {
		t.Fatalf("expected complete, got %q", status)
	}
}

func TestDeriveRunStatusBlockedWhenPermissionDenied(t *testing.T) {
	msgs := []message.Message{
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.Finish{Reason: message.FinishReasonPermissionDenied},
		}},
	}

	status, _ := deriveRunStatus(msgs)
	if status != taskrun.StatusBlocked {
		t.Fatalf("expected blocked, got %q", status)
	}
}

func TestTimelineSnapshotMatchesConsoleQueryMetadata(t *testing.T) {
	snapshot := timelineSnapshot{
		ToolName: "bash",
		Detail:   "Waiting on permission",
		Metadata: taskrun.EventMetadata{
			ToolInputPreview: "{\"command\":\"npm publish\"}",
			FinishReason:     "permission_denied",
			PermissionReason: "Permission approval required before running bash",
		},
	}

	if !snapshot.matchesConsoleQuery("npm publish") {
		t.Fatalf("expected tool input preview to match query")
	}
	if !snapshot.matchesConsoleQuery("approval bash") {
		t.Fatalf("expected permission reason to match query")
	}
	if snapshot.matchesConsoleQuery("python") {
		t.Fatalf("did not expect unrelated query to match")
	}
}

func TestTimelineSnapshotMetadataLine(t *testing.T) {
	snapshot := timelineSnapshot{
		Metadata: taskrun.EventMetadata{
			ToolInputPreview: "{\"command\":\"go test ./...\"}",
			FinishReason:     "end_turn",
		},
	}

	line := snapshot.MetadataLine(80)
	if line == "" {
		t.Fatalf("expected metadata line to render")
	}
	if !containsAll(line, []string{"input", "go test", "finish end_turn"}) {
		t.Fatalf("unexpected metadata line: %q", line)
	}
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
