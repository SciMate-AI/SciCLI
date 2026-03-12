package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactMCPToolResponseKeepsShortOutput(t *testing.T) {
	response := compactMCPToolResponse("rdkit_describe_molecule", `{"formula":"C8H10N4O2"}`)

	if response.Content != `{"formula":"C8H10N4O2"}` {
		t.Fatalf("expected short output to remain unchanged, got %q", response.Content)
	}
	if response.Metadata != "" {
		t.Fatalf("expected no metadata for short output, got %q", response.Metadata)
	}
}

func TestCompactMCPToolResponseSummarizesLargeJSON(t *testing.T) {
	rawBlock := strings.Repeat("ATOM  1  C\n", 800)
	input := map[string]any{
		"canonical_smiles": "Cn1c(=O)n(C)c2ncn(C)c2c1=O",
		"formula":          "C8H10N4O2",
		"mol_block_3d":     rawBlock,
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	response := compactMCPToolResponse("rdkit_describe_molecule", string(data))

	if response.Metadata == "" {
		t.Fatal("expected summarized response metadata to contain raw output")
	}
	if !strings.Contains(response.Content, "compacted for model context") {
		t.Fatalf("expected compacted notice in response, got %q", response.Content)
	}
	if strings.Contains(response.Content, rawBlock) {
		t.Fatal("expected raw mol block to be removed from model context")
	}
	if !strings.Contains(response.Content, "[omitted") {
		t.Fatalf("expected summarized placeholder in response, got %q", response.Content)
	}

	var metadata MCPToolResponseMetadata
	if err := json.Unmarshal([]byte(response.Metadata), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.RawContent == "" || !metadata.Compacted {
		t.Fatalf("expected metadata to preserve raw output and compaction flag, got %+v", metadata)
	}
	if !strings.Contains(metadata.RawContent, `"mol_block_3d"`) || !strings.Contains(metadata.RawContent, "ATOM  1  C") {
		t.Fatal("expected metadata raw content to include original mol block")
	}
}
