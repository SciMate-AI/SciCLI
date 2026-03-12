package agent

import (
	"encoding/json"
	"strings"

	"github.com/SciMate-AI/scicli/internal/llm/tools"
)

func compactToolResponseForModelContext(toolName string, response tools.ToolResponse) tools.ToolResponse {
	raw := strings.TrimSpace(response.Content)
	if raw == "" || metadataHasRawContent(response.Metadata) {
		return response
	}

	summary := summarizeMCPOutput(toolName, raw)
	if summary == raw {
		return response
	}

	response.Content = summary
	response.Metadata = mergeToolResponseMetadata(response.Metadata, MCPToolResponseMetadata{
		RawContent:    raw,
		OriginalChars: len(raw),
		OriginalLines: lineCount(raw),
		Compacted:     true,
	})
	return response
}

func metadataHasRawContent(metadata string) bool {
	if strings.TrimSpace(metadata) == "" {
		return false
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(metadata), &parsed); err != nil {
		return false
	}

	rawContent, ok := parsed["raw_content"].(string)
	return ok && strings.TrimSpace(rawContent) != ""
}

func mergeToolResponseMetadata(existing string, extra MCPToolResponseMetadata) string {
	merged := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		_ = json.Unmarshal([]byte(existing), &merged)
	}

	if extra.RawContent != "" {
		merged["raw_content"] = extra.RawContent
	}
	if extra.OriginalChars > 0 {
		merged["original_chars"] = extra.OriginalChars
	}
	if extra.OriginalLines > 0 {
		merged["original_lines"] = extra.OriginalLines
	}
	if extra.Compacted {
		merged["compacted"] = true
	}

	if len(merged) == 0 {
		return ""
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return existing
	}
	return string(data)
}
