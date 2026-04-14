package message

import (
	"reflect"
	"testing"
)

func TestMarshallPartsGeminiRawContentRoundTrip(t *testing.T) {
	t.Parallel()

	parts := []ContentPart{
		TextContent{Text: "hello"},
		GeminiRawContent{
			Parts: []map[string]any{
				{
					"thought":          true,
					"text":             "plan",
					"thoughtSignature": "sig-1",
				},
				{
					"functionCall": map[string]any{
						"name": "read_file",
						"args": map[string]any{"path": "go.mod"},
					},
				},
			},
		},
	}

	data, err := marshallParts(parts)
	if err != nil {
		t.Fatalf("marshallParts() error = %v", err)
	}

	got, err := unmarshallParts(data)
	if err != nil {
		t.Fatalf("unmarshallParts() error = %v", err)
	}

	raw, ok := got[1].(GeminiRawContent)
	if !ok {
		t.Fatalf("expected GeminiRawContent, got %T", got[1])
	}

	if !reflect.DeepEqual(raw.Parts, parts[1].(GeminiRawContent).Parts) {
		t.Fatalf("raw parts mismatch\nwant: %#v\ngot:  %#v", parts[1].(GeminiRawContent).Parts, raw.Parts)
	}
}

func TestMarshallPartsScientistBenchContentRoundTrip(t *testing.T) {
	t.Parallel()

	parts := []ContentPart{
		ScientistBenchContent{
			Kind:          "agent",
			AgentID:       "method_planner",
			AgentLabel:    "Method Planner",
			State:         "streaming",
			Title:         "Generating response",
			Detail:        "Drafting the method section",
			ToolName:      "rg",
			CaseID:        "case-1",
			NodeID:        "node-method-plan",
			RunID:         "run-1",
			TaskSessionID: "sbtask-1",
		},
		TextContent{Text: "Working on the draft."},
	}

	data, err := marshallParts(parts)
	if err != nil {
		t.Fatalf("marshallParts() error = %v", err)
	}

	got, err := unmarshallParts(data)
	if err != nil {
		t.Fatalf("unmarshallParts() error = %v", err)
	}

	meta, ok := got[0].(ScientistBenchContent)
	if !ok {
		t.Fatalf("expected ScientistBenchContent, got %T", got[0])
	}

	if !reflect.DeepEqual(meta, parts[0].(ScientistBenchContent)) {
		t.Fatalf("metadata mismatch\nwant: %#v\ngot:  %#v", parts[0].(ScientistBenchContent), meta)
	}
}
