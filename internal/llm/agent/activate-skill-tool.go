package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/skills"
)

const ActivateSkillToolName = "activate_skill"

type activateSkillParams struct {
	SkillName string `json:"skill_name"`
	Reason    string `json:"reason,omitempty"`
}

type activateSkillTool struct {
	skillsSvc skills.Service
}

func NewActivateSkillTool(skillsSvc skills.Service) tools.BaseTool {
	return &activateSkillTool{skillsSvc: skillsSvc}
}

func (t *activateSkillTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name: ActivateSkillToolName,
		Description: "Activate an available skill when the task matches specialized workflow guidance. Use this before following a skill's instructions. After activation, the skill content is injected into the conversation context for the current session.",
		Parameters: map[string]any{
			"skill_name": map[string]any{
				"type":        "string",
				"description": "The exact skill ID or unique skill name to activate.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Short reason why this skill applies to the current task.",
			},
		},
		Required: []string{"skill_name"},
	}
}

func (t *activateSkillTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	var params activateSkillParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	sessionID, _ := tools.GetContextValues(ctx)
	if sessionID == "" {
		return tools.ToolResponse{}, fmt.Errorf("session_id is required")
	}
	skill, err := t.skillsSvc.Activate(ctx, sessionID, params.SkillName)
	if err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	var out strings.Builder
	fmt.Fprintf(&out, "Activated skill %q.\n", skill.ID)
	if strings.TrimSpace(params.Reason) != "" {
		fmt.Fprintf(&out, "Reason: %s\n", strings.TrimSpace(params.Reason))
	}
	fmt.Fprintf(&out, "Resolve relative paths in this skill from: %s\n", skill.Dir)
	fmt.Fprintf(&out, "Skill file: %s\n\n", skill.Path)
	fmt.Fprintf(&out, "<skill id=%q scope=%q>\n%s\n</skill>", skill.ID, skill.Scope, strings.TrimSpace(skill.Content))
	return tools.NewTextResponse(out.String()), nil
}
