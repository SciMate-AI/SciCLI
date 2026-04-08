package chat

import (
	"slices"
	"strings"
	"unicode"
)

type slashCommand struct {
	Command      string
	InsertText   string
	Args         string
	Category     string
	Description  string
	Priority     int
	RequiresArgs bool
}

var slashCommands = []slashCommand{
	{Command: "/new", InsertText: "/new", Category: "session", Description: "Create a clean chat session.", Priority: 60},
	{Command: "/compact", InsertText: "/compact", Category: "session", Description: "Compact the current conversation.", Priority: 40},
	{Command: "/skills", InsertText: "/skills", Category: "tools", Description: "Open the installed skill browser.", Priority: 30},
	{Command: "/tasks", InsertText: "/tasks", Category: "tasks", Description: "Show delegated task sessions.", Priority: 70},
	{Command: "/install-skill", InsertText: "/install-skill ", Args: "<path-or-url>", Category: "tools", Description: "Install a skill from a local path or GitHub URL.", RequiresArgs: true},
	{Command: "/ultrawork", InsertText: "/ultrawork ", Args: "[on|off|auto]", Category: "mode", Description: "Switch work mode: on, off, or auto."},
	{Command: "/parent", InsertText: "/parent", Category: "session", Description: "Jump back to the parent session.", Priority: 20},
	{Command: "/research set", InsertText: "/research set ", Args: "<objective>", Category: "research", Description: "Save the current research objective.", Priority: 100, RequiresArgs: true},
	{Command: "/research show", InsertText: "/research show", Category: "research", Description: "Show the current research state.", Priority: 50},
	{Command: "/experiment add", InsertText: "/experiment add ", Args: "<title>", Category: "research", Description: "Create a new experiment candidate.", Priority: 90, RequiresArgs: true},
	{Command: "/experiment list", InsertText: "/experiment list", Category: "research", Description: "List current experiment candidates.", Priority: 55},
	{Command: "/experiment compare", InsertText: "/experiment compare", Category: "research", Description: "Compare experiment lineages and scores.", Priority: 45},
	{Command: "/experiment activate", InsertText: "/experiment activate ", Args: "<id>", Category: "research", Description: "Set the active experiment by id.", RequiresArgs: true},
	{Command: "/experiment promote", InsertText: "/experiment promote ", Args: "[id]", Category: "research", Description: "Mark an experiment as the current best candidate."},
	{Command: "/experiment evaluate", InsertText: "/experiment evaluate ", Args: "<score> <keep|discard|mutate|branch> <summary>", Category: "research", Description: "Score the active experiment and record a decision.", RequiresArgs: true},
	{Command: "/experiment rerun", InsertText: "/experiment rerun ", Args: "[id]", Category: "research", Description: "Clone the active experiment for another run."},
	{Command: "/experiment evolve", InsertText: "/experiment evolve ", Args: "[title]", Category: "research", Description: "Create the next generation from the active experiment."},
	{Command: "/experiment propose", InsertText: "/experiment propose", Category: "research", Description: "Ask the agent to propose the next experiment.", Priority: 65},
	{Command: "/artifact list", InsertText: "/artifact list ", Args: "[query]", Category: "artifacts", Description: "Search experiment artifacts and provenance."},
	{Command: "/artifact show", InsertText: "/artifact show ", Args: "<artifact-id>", Category: "artifacts", Description: "Show one artifact in detail.", RequiresArgs: true},
	{Command: "/help", InsertText: "/help", Category: "help", Description: "Show the main slash-command help summary.", Priority: 80},
}

func slashQuery(value string) string {
	trimmedLeft := strings.TrimLeftFunc(value, unicode.IsSpace)
	if !strings.HasPrefix(trimmedLeft, "/") {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(trimmedLeft))
}

func filterSlashCommands(value string) []slashCommand {
	query := slashQuery(value)
	if query == "" {
		return nil
	}

	filtered := make([]slashCommand, 0, len(slashCommands))
	for _, cmd := range slashCommands {
		command := strings.ToLower(cmd.Command)
		insertText := strings.ToLower(strings.TrimSpace(cmd.InsertText))
		description := strings.ToLower(cmd.Description)
		if query == "/" ||
			strings.HasPrefix(command, query) ||
			strings.HasPrefix(insertText, query) ||
			strings.Contains(command, query) ||
			strings.Contains(description, strings.TrimPrefix(query, "/")) {
			filtered = append(filtered, cmd)
		}
	}

	slices.SortStableFunc(filtered, func(a, b slashCommand) int {
		aCommand := strings.ToLower(a.Command)
		bCommand := strings.ToLower(b.Command)
		aPrefix := strings.HasPrefix(aCommand, query)
		bPrefix := strings.HasPrefix(bCommand, query)
		if aPrefix != bPrefix {
			if aPrefix {
				return -1
			}
			return 1
		}
		if a.Priority != b.Priority {
			return b.Priority - a.Priority
		}
		if len(aCommand) != len(bCommand) {
			return len(aCommand) - len(bCommand)
		}
		return strings.Compare(aCommand, bCommand)
	})

	return filtered
}

func slashCommandHasArguments(value string, cmd slashCommand) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	command := strings.ToLower(strings.TrimSpace(cmd.InsertText))
	if !strings.HasPrefix(trimmed, command) {
		return false
	}
	return len(trimmed) > len(command)
}

func (c slashCommand) DisplayCommand() string {
	if strings.TrimSpace(c.Args) == "" {
		return c.Command
	}
	return c.Command + " " + c.Args
}
