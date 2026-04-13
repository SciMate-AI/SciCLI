# SciCLI

Terminal-based AI assistant for coding and research workflows.

Adapted from the original `opencode` code-agent baseline and extended toward a terminal-first research workbench for scientific and engineering use cases.

> [!WARNING]
> SciCLI is still evolving quickly. Interfaces, prompts, defaults, and provider/model support may change between releases.

## What Makes SciCLI Different

SciCLI is not just a generic terminal chat wrapper around an LLM. It is designed to be a practical coding and research agent with a local-first terminal UX and optional support for user-configured MCP servers.

- **Configurable MCP integration**: SciCLI can load tools from MCP servers you explicitly configure, so internal APIs, research backends, and local MCP utilities can be exposed as normal agent tools.
- **Context-window protection for long tool outputs**: long MCP tool returns are compacted before being sent back to the model, while the full raw result remains available in metadata for the UI. This reduces 400 errors caused by oversized tool context.
- **Terminal UI built for tool-heavy sessions**: sessions, permissions, logs, provider/model switching, file edits, and tool results all live in the TUI. Long histories can be browsed with mouse wheel support, a visible scrollbar, and scroll position hints.
- **Local coding tools plus configured MCP tools**: the agent can inspect files, run shell commands, edit code, apply patches, fetch URLs, read diagnostics, and call MCP tools in the same conversation.
- **Conversation continuity**: automatic session compaction summarizes long conversations before they exceed the current model's context window, so work can continue without manually restarting from scratch.
- **Structured research session memory**: each chat can now keep a persisted research objective and stage outside the raw transcript, so the operator can reopen a session and recover the current direction immediately.
- **Structured experiment loop skeleton**: experiment plans, linked delegated-task runs, and evaluation notes are now persisted as machine-readable research state instead of being buried in prompts.
- **Artifact-centered experiment provenance**: completed experiment runs now capture prompt, final response, task timeline, and modified-file artifacts so results are inspectable without replaying the full chat.
- **Project memory support**: SciCLI automatically looks for files such as `SCICLI.md`, `scicli.md`, `CLAUDE.md`, and `.github/copilot-instructions.md` to load project-specific guidance into the agent context.
- **Interactive and non-interactive modes**: use the full TUI for exploratory work, or run one-shot prompts from scripts and CI with `-p`.
- **Multi-provider model routing**: SciCLI supports OpenAI, Anthropic, Gemini, OpenRouter, Groq, xAI, Copilot, Bedrock, Vertex AI, Azure OpenAI, local OpenAI-compatible endpoints, and custom OpenAI-compatible servers.
- **Developer ergonomics**: SQLite-backed history, session switching, custom slash-like commands, external editor support, LSP diagnostics, and permission prompts are built in.

## Installation

### npm

```bash
npm install -g @scimate/scicli
```

The npm package installs a thin wrapper around the Go binary. It tries to download a matching release artifact first and falls back to `go build` if needed.

### Go

```bash
go install github.com/SciMate-AI/scicli@latest
```

### Build from source

```bash
git clone https://github.com/SciMate-AI/scicli.git
cd scicli
go build ./...
```

## Quick Start

### 1. Launch the interactive TUI

```bash
scicli
```

On first launch, SciCLI opens a single onboarding flow if no usable provider/model is configured yet. That flow handles provider selection, credentials, and default model selection.

### 2. Configure a model provider

SciCLI can detect many credentials from environment variables automatically, for example:

```bash
export OPENAI_API_KEY=...
export GEMINI_API_KEY=...
export ANTHROPIC_API_KEY=...
```

You can also configure providers manually in `~/.scicli.json`.

### 3. Configure MCP servers if you need external tools

Add MCP servers to `~/.scicli.json` or `./.scicli.json`:

```json
{
  "mcpServers": {
    "research-api": {
      "type": "sse",
      "url": "https://your-server.example/sse"
    }
  }
}
```

SciCLI does not ship built-in remote CAE, run-management, or service-specific MCP presets. If you need remote tools, add them explicitly in configuration.

For Scientist Bench literature review and paper writing, a Zotero MCP server is useful because it can normalize references into formatted citations and BibTeX instead of relying on model-written citation strings. A typical local stdio setup looks like:

```json
{
  "mcpServers": {
    "zotero": {
      "type": "stdio",
      "command": "uvx",
      "args": ["zotero-mcp"]
    }
  }
}
```

Or install the built-in preset into your config:

```bash
scicli mcp install zotero
```

With that configured, the `research_agent` can discover papers from web/API search, then normalize them through Zotero before passing structured references to the `paper_writer`.

### 4. Optional local dependencies

SciCLI works without these tools, but some features are better with them installed:

- `rg` / `ripgrep`: faster file search, grep, and project scanning
- `fzf`: better interactive selection for some terminal workflows
- `uvx`: useful if one of your configured MCP servers is launched locally through `uvx`
- language servers such as `gopls` or `typescript-language-server`: diagnostics support

### 5. Optional project memory

Project memory is no longer part of first-run onboarding. If you want SciCLI to generate or refresh `SCICLI.md`, open the command palette with `Ctrl+K` and run `Generate Project Memory`.

## CLI Usage

### Interactive mode

```bash
scicli
scicli -d
scicli -c /path/to/project
```

### Non-interactive mode

```bash
scicli -p "Explain the use of context in Go"
scicli -p "Summarize the changes in this repository" -f json
scicli -p "Check whether the tests mention flaky behavior" -q
scicli --work-mode ultrawork -p "Investigate and fix the failing tests"
scicli ultrawork "Ship this refactor end to end"
```

### MCP inspection and direct calls

```bash
scicli mcp list-tools --server research-api
scicli mcp call --server rdkit --tool rdkit_describe_molecule --args "{\"smiles\":\"Cn1c(=O)n(C)c2ncn(C)c2c1=O\"}"
```

## Key Features

### Terminal UI

- Searchable command palette (`Ctrl+K`) for session switching, work-mode controls, and provider/model switching
- `Set Research Objective` command to persist a session-level research goal
- `Add Experiment Plan` and `Evaluate Active Experiment` commands for the Phase 2 research loop
- `Propose Next Experiment` to ask the agent for a candidate plan based on prior experiment state
- `Generate Project Memory` command to create or refresh `SCICLI.md` on demand
- Session history browser with persistent saved sessions
- Scrollable conversation history with mouse wheel support, visible scrollbar, and position indicator
- Model/provider switcher from inside the UI
- Searchable skill browser (`/skills`) with in-TUI install and uninstall actions
- Transcript-first workbench layout with lightweight overlays for research state, tasks, and configuration
- Delegated task browser (`/tasks`) for inspecting child-agent sessions spawned from the current chat
- Permission prompts for tool execution
- Logs view for debugging and tool inspection
- External editor support for composing long prompts
- Expand/collapse tool results with `Ctrl+G`

### Agent Tooling

Built-in local tools include:

- `bash`
- `glob`
- `grep`
- `ls`
- `view`
- `write`
- `edit`
- `patch`
- `fetch`
- `diagnostics`
- `sourcegraph`
- `agent` for delegated sub-tasks
- `activate_skill` for `SKILL.md`-based Agent Skills activation

Remote MCP tools are loaded dynamically from configured servers and appear to the agent alongside built-in tools.

### Agent Skills

SciCLI can discover and activate `SKILL.md`-based Agent Skills from skill directories that contain a `SKILL.md` file.

SciCLI also ships with bundled extension skills synced on startup into:

- `$HOME/.scicli/extensions/claude-scientific-skills/skills` from `K-Dense-AI/claude-scientific-skills`
- `$HOME/.scicli/extensions/hpc-skills/skills` from `SciMate-AI/HPC-Skills`

Discovered roots include:

- `./.scicli/skills`
- `./.gemini/skills`
- `./.claude/skills`
- `$HOME/.scicli/skills`
- `$HOME/.gemini/skills`
- `$HOME/.claude/skills`
- bundled extension skills under `./.scicli/extensions/*/skills`, `./.gemini/extensions/*/skills`, `./.claude/extensions/*/skills`, `$HOME/.scicli/extensions/*/skills`, `$HOME/.gemini/extensions/*/skills`, and `$HOME/.claude/extensions/*/skills`

The agent receives a catalog of recommended skills for the current user request and can call `activate_skill` to inject a skill into the current session context on demand. Relative paths referenced by a skill are resolved from that skill's directory.

Scientist Bench workers use the same mechanism. In addition to generic session-level recommendation, worker prompts now include role-aware and node-aware skill suggestions, so literature-review nodes can proactively activate skills like `citation-management` or `research-lookup`, and paper-drafting nodes can activate `scientific-writing` or `venue-templates` when those skills are available.

### Context Management

SciCLI includes two complementary protections against context blowups:

- `autoCompact`: summarizes long sessions before they exceed the model context window
- MCP tool result compaction: large remote-tool outputs are summarized before being sent back to the model, while the UI can still show the complete raw payload

This is especially important for tools that can return bulky JSON, molecular blocks, coordinates, or large generated documents.

### MCP Integration

SciCLI can integrate with whatever MCP servers you configure. There are no built-in remote CAE, run-management, or service-specific MCP integrations in the current product.

### Project Context Files

SciCLI automatically looks for these files and directories to enrich the system context:

- `.github/copilot-instructions.md`
- `.cursorrules`
- `.cursor/rules/`
- `CLAUDE.md`
- `CLAUDE.local.md`
- `SCICLI.md`
- `SCICLI.local.md`
- `scicli.md`
- `scicli.local.md`
- `SciCLI.md`
- `SciCLI.local.md`

This makes it easier to keep project-specific instructions in-repo instead of repeating them in every chat.

## Configuration

SciCLI reads configuration from:

- `$HOME/.scicli.json`
- `$XDG_CONFIG_HOME/scicli/.scicli.json`
- `./.scicli.json`

### Example configuration

```json
{
  "data": {
    "directory": ".scicli"
  },
  "providers": {
    "gemini": {
      "apiKey": "your-api-key",
      "disabled": false
    },
    "openai": {
      "apiKey": "your-api-key",
      "disabled": false
    },
    "anthropic": {
      "apiKey": "your-api-key",
      "disabled": false
    },
    "openai-compatible": {
      "apiKey": "optional-api-key",
      "baseUrl": "https://your-compatible-endpoint/v1",
      "model": "your-upstream-model-id",
      "disabled": false
    }
  },
  "agents": {
    "coder": {
      "model": "gemini-3.1-pro-preview",
      "maxTokens": 5000
    },
    "task": {
      "model": "gemini-3.1-flash-lite-preview",
      "maxTokens": 5000
    },
    "summarizer": {
      "model": "gemini-3.1-pro-preview",
      "maxTokens": 5000
    },
    "title": {
      "model": "gemini-3.1-flash-lite-preview",
      "maxTokens": 80
    }
  },
  "shell": {
    "path": "/bin/bash",
    "args": ["-l"]
  },
  "skills": {
    "paths": ["path/to/more/skills"],
    "disabled": ["example/skill"]
  },
  "mcpServers": {
    "rdkit": {
      "type": "streamable-http",
      "url": "https://your-rdkit-server/mcp"
    },
    "research-api": {
      "type": "sse",
      "url": "https://your-server.example/sse"
    },
    "arxiv": {
      "type": "stdio",
      "command": "uvx",
      "args": ["arxiv-paper-mcp-server"]
    },
    "local-toolbox": {
      "type": "stdio",
      "command": "path/to/mcp-server",
      "args": []
    }
  },
  "lsp": {
    "go": {
      "disabled": false,
      "command": "gopls"
    }
  },
  "tui": {
    "theme": "scicli",
    "altScreen": false,
    "mouse": false,
    "linkAllAgentModels": true
  },
  "autoCompact": true,
  "debug": false,
  "debugLSP": false
}
```

`altScreen` controls whether `scicli` launches into a true alternate terminal page. The default is `false`: SciCLI stays in the primary terminal buffer, clears to the top on startup, and preserves normal terminal selection behavior better. `mouse` controls whether SciCLI captures mouse input; leaving it `false` usually preserves terminal text selection and scrollback behavior better. `linkAllAgentModels` controls whether model changes in the UI apply only to the coder agent or to `coder`, `task`, `summarizer`, and `title` together.

### Useful environment variables

| Variable | Purpose |
| --- | --- |
| `ANTHROPIC_API_KEY` | Enable Anthropic models |
| `OPENAI_API_KEY` | Enable OpenAI models |
| `GEMINI_API_KEY` | Enable Gemini models |
| `OPENROUTER_API_KEY` | Enable OpenRouter models |
| `GROQ_API_KEY` | Enable Groq models |
| `XAI_API_KEY` | Enable xAI models |
| `GITHUB_TOKEN` | Enable GitHub Copilot if token-based auth is used |
| `LOCAL_ENDPOINT` | Use a local OpenAI-compatible endpoint |
| `OPENAI_COMPATIBLE_API_KEY` | API key for a custom OpenAI-compatible endpoint |
| `SHELL` | Default shell path used by the `bash` tool |

## MCP Support

SciCLI supports three MCP transport types:

- `stdio`
- `sse`
- `streamable-http`

Once configured, MCP tools are auto-discovered and exposed to the coding agent. They go through the same permission system as built-in tools.

SciCLI's MCP implementation also includes practical behavior for real-world agent use:

- automatic startup of MCP clients when needed
- support for long-running remote servers
- tool-result compaction so large payloads do not overwhelm model context

## LSP Integration

SciCLI can connect to local language servers and currently exposes diagnostics to the agent. This allows the assistant to inspect compile-time or lint-time issues while editing code.

Typical setup:

```json
{
  "lsp": {
    "go": {
      "disabled": false,
      "command": "gopls"
    },
    "typescript": {
      "disabled": false,
      "command": "typescript-language-server",
      "args": ["--stdio"]
    }
  }
}
```

### Skills CLI

- `scicli skills list`
- `scicli skills recommend "vasp convergence"`
- `scicli skills install <local-path-or-github-tree-url>`
- `scicli skills uninstall <skill-id>`
- `scicli skills disable <skill-id>`
- `scicli skills enable <skill-id>`

Inside the TUI, slash commands now include:

- `/research set <objective>`
- `/research show`
- `/experiment add <title>`
- `/experiment list`
- `/experiment compare`
- `/experiment activate <experiment-id>`
- `/experiment promote [experiment-id]`
- `/experiment evaluate <score> <keep|discard|mutate|branch> <summary>`
- `/experiment rerun [experiment-id]`
- `/experiment evolve [title]`
- `/experiment propose`
- `/artifact list [query]`
- `/artifact show <artifact-id>`
- `/skills`
- `/tasks`
- `/parent`
- `/install-skill <local-path-or-github-tree-url>`
- `/compact`
- `/new`
- `/ultrawork [on|off|auto]`

The skill browser supports:

- typing to filter skills
- `Ctrl+I` to install from a local path or GitHub tree URL
- `Ctrl+X` to uninstall a user-installed skill

The delegated task browser supports:

- browsing child-agent sessions created from the current chat
- pressing `Enter` to jump into the selected delegated task session
- using `/parent` or the command palette to jump back to the parent chat

The delegated task flow supports:

- near-real-time task status refresh for delegated child sessions
- opening delegated sessions from `/tasks` or the status bar
- stopping the selected delegated run directly from the task browser
- tracking linked experiment runs, captured artifacts, and task lineage in persisted research state

The experiment loop now also tracks explicit selection decisions and lineage:

- evaluation decisions are normalized to `keep`, `discard`, `mutate`, or `branch`
- experiments carry generation and lineage-root metadata
- research sessions can mark one promoted experiment as the current best candidate
- `/experiment evolve` creates the next generation from an evaluated `mutate` or `branch` candidate
- `/experiment propose` now returns and stores an explicit `mutate` or `branch` strategy for the proposed child experiment

The transcript-first chat flow now surfaces the research loop through slash commands, the task browser, and session state instead of a fixed multi-pane workbench.

When work mode is set to `ultrawork`, the current TUI session auto-approves tool permissions so autonomous runs are not interrupted by approval prompts. Delegated child-task sessions now inherit that session-level auto-approval as well.

## Custom Commands

Custom commands let you store reusable prompt templates as Markdown files.

Supported locations:

- `$XDG_CONFIG_HOME/scicli/commands/`
- `$HOME/.scicli/commands/`
- `<project>/.scicli/commands/`

Each `.md` file becomes a command in the UI command palette. Named placeholders like `$ISSUE_NUMBER` or `$AUTHOR_NAME` are supported and will prompt for values at execution time.

Example:

```markdown
# Review issue $ISSUE_NUMBER

RUN gh issue view $ISSUE_NUMBER --json title,body,comments
RUN git grep "$SEARCH_TERM"
```

## Keyboard Shortcuts

Common shortcuts:

- `Ctrl+C`: quit
- `Ctrl+L`: open logs
- `Ctrl+S`: switch sessions
- `Ctrl+K`: open the command palette
- `Alt+[` / `Alt+]`: rotate the highlighted delegated task in the status bar
- `Ctrl+O`: switch provider / model
- `Ctrl+N`: create a new session
- `Ctrl+E`: open the external editor
- `Ctrl+F`: open the file picker
- `Ctrl+T`: switch theme
- `Ctrl+G`: toggle tool output expansion
- `PgUp` / `PgDn` / `Ctrl+U` / `Ctrl+D`: scroll chat history
- `Esc`: cancel current generation or close the active overlay

Useful command-palette actions:

- `Set Research Objective`
- `Show Research State`
- `Add Experiment Plan`
- `Evaluate Active Experiment`
- `Promote Active Experiment`
- `Propose Next Experiment`
- `Evolve Active Experiment`
- `Rerun Active Experiment`
- `Compare Experiments`
- `List Experiment Artifacts`
- `Open Tasks`
- `Open Latest Task`
- `Stop Latest Running Task`
- `Switch Session`
- `Switch Provider / Model`

The status bar now mirrors delegated-task state:

- clicking the `Inspector` chip opens the delegated task list
- clicking the `Task ...` chip opens the currently highlighted delegated task
- `Alt+[` / `Alt+]` rotate which delegated task is highlighted there

The in-app help dialog shows the current complete keymap.

## Development

### Prerequisites

- Go 1.24 or newer

### Build

```bash
go build ./...
```

### Test

```bash
go test ./...
```

### Release automation

Tag-based GitHub Actions publish:

- GitHub release artifacts
- the npm package `@scimate/scicli`

Repository setup details live in [docs/release-setup.md](docs/release-setup.md).

## Architecture

Current product direction:

- the original rebrand-and-port baseline is documented in [docs/scicli-implementation-plan.md](docs/scicli-implementation-plan.md)
- the active product roadmap is documented in [docs/scicli-roadmap.md](docs/scicli-roadmap.md)

Roadmap execution status:

- Phase 0 is complete enough for the current branch: onboarding is singular, built-in/default MCP startup is gone, and the old built-in remote CAE command surface has been removed.
- Phase 1 is now in progress: a minimal persisted research-session layer exists in the TUI and can store a session objective with `/research set`.
- Phase 2 has started: experiment plans, delegated-task run links, and evaluation summaries now persist in research state, with new `/experiment ...` commands and task-browser visibility.
- Phase 3 has started: completed experiment runs now persist local provenance artifacts for comparison and review.
- Phase 4 is in progress: selection decisions, promoted candidates, and lineage-aware evolve flows are now stored and visible.
- Phase 5 has started: the workbench now surfaces research objective, candidate queue, and lineage/operator hints directly in the TUI.

Key packages:

- `cmd`: Cobra CLI entrypoints
- `internal/app`: application wiring and lifecycle
- `internal/config`: config loading, defaults, validation, onboarding
- `internal/llm`: providers, prompts, agents, and tools
- `internal/mcpclient`: MCP client implementations
- `internal/tui`: Bubble Tea terminal UI
- `internal/message`: message and content-part model
- `internal/session`: persistent session management
- `internal/db`: SQLite storage and migrations
- `internal/lsp`: language-server integration

## Acknowledgments

- [@isaacphi](https://github.com/isaacphi) for the [mcp-language-server](https://github.com/isaacphi/mcp-language-server) work that influenced the LSP integration
- [@adamdottv](https://github.com/adamdottv) for design direction and UI ideas
- the broader open source ecosystem around Bubble Tea, Cobra, SQLite, and MCP

## License

SciCLI is licensed under the MIT License. See [LICENSE](LICENSE).
