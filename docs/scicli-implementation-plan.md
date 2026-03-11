# `scicli` Implementation Plan

## Summary
- Use `opencode` as the CLI and agent baseline.
- Do not port VS Code-only systems from `scimate-vscode` such as webviews, the plan engine, or the fallback workspace/terminal/patch tools created for the extension host.
- Add the missing SciMate capabilities that matter in a standalone CLI:
  - Supabase auth with register/login/logout/status
  - token refresh and token-aware remote MCP calls
  - built-in SciMate MCP server presets
  - CAE run, log, and artifact commands
  - `scicli` branding, config paths, and local state

## Implementation
- Rebrand the `opencode` baseline to `scicli` at the CLI/config/data-directory level.
- Keep the existing `opencode` local coding tools, permissions, sessions, and MCP tool exposure for the agent.
- Extend configuration with:
  - `auth.supabase.url`
  - `auth.supabase.anonKey`
  - built-in MCP defaults for `cae-agent`, `origin`, and `rdkit`
- Add a lightweight auth subsystem:
  - Supabase password signup/login
  - refresh-token based renewal
  - local token persistence
- Extend MCP support with:
  - `streamable-http` transport
  - access-token injection when a tool schema declares `access_token`
- Add CLI commands:
  - `scicli auth register|login|logout|status`
  - `scicli mcp list-tools|call`
  - `scicli runs start|last`
  - `scicli log get`
  - `scicli artifacts list|get`

## Scope Decisions
- Keep `opencode` agent behavior as the main code-agent path.
- Do not add a second plan/replay engine.
- Do not port VS Code UI state, review flows, or local workspace MCP shims.
- Keep the current SciMate backend endpoints as defaults, but express them through `scicli` config.

## Validation
- Build and smoke test the new commands.
- Verify auth flow against Supabase.
- Verify token-aware MCP calls against the default CAE server.
- Verify run start, run log fetch, and artifact listing against the current backend.
