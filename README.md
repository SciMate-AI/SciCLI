# SciCLI

Terminal-based AI assistant for coding and SciMate workflows.

Adapted from the original `opencode` code-agent baseline and significantly extended for SciMate-native tooling, authentication, MCP integration, and scientific/engineering use cases.

> [!WARNING]
> SciCLI is still evolving quickly. Interfaces, prompts, defaults, and provider/model support may change between releases.

## What Makes SciCLI Different

SciCLI is not just a generic terminal chat wrapper around an LLM. It is designed to be a practical coding and scientific workflow agent with a local-first terminal UX and built-in support for SciMate services.

- **SciMate-native MCP integration**: SciCLI boots with default remote MCP endpoints for `cae-agent`, `origin`, and `rdkit`, so chemistry, CAE, and origin-analysis workflows can be exposed as normal agent tools.
- **Built-in SciMate auth flow**: `scicli auth register|login|logout|status` store a persistent local session, and MCP tools that require `access_token` can receive a refreshed token automatically instead of asking the user to paste credentials into prompts.
- **Context-window protection for long tool outputs**: long MCP tool returns are compacted before being sent back to the model, while the full raw result remains available in metadata for the UI. This reduces 400 errors caused by oversized tool context.
- **Terminal UI built for tool-heavy sessions**: sessions, permissions, logs, account actions, provider/model switching, file edits, and tool results all live in the TUI. Long histories can be browsed with mouse wheel support, a visible scrollbar, and scroll position hints.
- **Local coding tools plus remote tools**: the agent can inspect files, run shell commands, edit code, apply patches, fetch URLs, read diagnostics, and call MCP tools in the same conversation.
- **Conversation continuity**: automatic session compaction summarizes long conversations before they exceed the current model's context window, so work can continue without manually restarting from scratch.
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

On first launch, SciCLI opens an onboarding flow if no usable provider/model is configured yet.

### 2. Configure a model provider

SciCLI can detect many credentials from environment variables automatically, for example:

```bash
export OPENAI_API_KEY=...
export GEMINI_API_KEY=...
export ANTHROPIC_API_KEY=...
```

You can also configure providers manually in `~/.scicli.json`.

### 3. Log in to SciMate if you use protected MCP services

```bash
scicli auth login
```

After login, remote MCP tools whose schema includes `access_token` can use the stored token automatically. SciCLI will also refresh the token before protected calls when the saved session is close to expiry.

### 4. Optional local dependencies

SciCLI works without these tools, but some features are better with them installed:

- `rg` / `ripgrep`: faster file search, grep, and project scanning
- `fzf`: better interactive selection for some terminal workflows
- language servers such as `gopls` or `typescript-language-server`: diagnostics support

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
```

### Auth commands

```bash
scicli auth register
scicli auth login
scicli auth status
scicli auth logout
```

### MCP inspection and direct calls

```bash
scicli mcp list-tools
scicli mcp call rdkit_describe_molecule "{\"smiles\":\"Cn1c(=O)n(C)c2ncn(C)c2c1=O\"}"
```

### Run management

```bash
scicli runs start
scicli runs last
scicli runs log
scicli runs artifacts list
```

## Key Features

### Terminal UI

- Command palette (`Ctrl+K`) for account actions, session switching, and provider/model switching
- Session history browser with persistent saved sessions
- Scrollable conversation history with mouse wheel support, visible scrollbar, and position indicator
- Account dialog for register/login/logout/token refresh from inside the UI
- Model/provider switcher from inside the UI
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

Remote MCP tools are loaded dynamically from configured servers and appear to the agent alongside built-in tools.

### Context Management

SciCLI includes two complementary protections against context blowups:

- `autoCompact`: summarizes long sessions before they exceed the model context window
- MCP tool result compaction: large remote-tool outputs are summarized before being sent back to the model, while the UI can still show the complete raw payload

This is especially important for tools that can return bulky JSON, molecular blocks, coordinates, or large generated documents.

### SciMate Integration

By default, SciCLI is prepared to work with SciMate services:

- `cae-agent` MCP server
- `origin` MCP server
- `rdkit` MCP server
- Supabase-backed SciMate authentication

These defaults can be overridden through configuration or environment variables.

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
  "mcpServers": {
    "cae-agent": {
      "type": "sse",
      "url": "https://your-cae-agent/sse"
    },
    "rdkit": {
      "type": "streamable-http",
      "url": "https://your-rdkit-server/mcp"
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
    "theme": "scicli"
  },
  "autoCompact": true,
  "debug": false,
  "debugLSP": false
}
```

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
| `SCICLI_MCP_CAE_AGENT_URL` | Override default `cae-agent` MCP endpoint |
| `SCICLI_MCP_ORIGIN_URL` | Override default `origin` MCP endpoint |
| `SCICLI_MCP_RDKIT_URL` | Override default `rdkit` MCP endpoint |
| `SCICLI_SUPABASE_URL` | Override SciMate auth backend URL |
| `SCICLI_SUPABASE_ANON_KEY` | Override SciMate auth anon key |
| `SHELL` | Default shell path used by the `bash` tool |

## MCP Support

SciCLI supports three MCP transport types:

- `stdio`
- `sse`
- `streamable-http`

Once configured, MCP tools are auto-discovered and exposed to the coding agent. They go through the same permission system as built-in tools.

SciCLI's MCP implementation also includes practical behavior for real-world agent use:

- automatic startup of MCP clients when needed
- remote auth token injection for tools that request `access_token`
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
- `Ctrl+O`: switch provider / model
- `Ctrl+N`: create a new session
- `Ctrl+E`: open the external editor
- `Ctrl+F`: open the file picker
- `Ctrl+T`: switch theme
- `Ctrl+G`: toggle tool output expansion
- `PgUp` / `PgDn` / `Ctrl+U` / `Ctrl+D`: scroll chat history
- `Esc`: cancel current generation or close the active overlay

Useful command-palette actions:

- `Account`: open the account panel with current login status
- `Login` / `Register` / `Logout` / `Refresh Login`
- `Switch Session`
- `Switch Provider / Model`

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
