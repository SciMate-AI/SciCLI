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
