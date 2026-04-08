# `scicli` Implementation Plan

> Historical note:
> This document captures the original rebrand-and-port plan from the early `scicli` phase.
> It is no longer the full product direction.
> The current product roadmap lives in [docs/scicli-roadmap.md](./scicli-roadmap.md).
> Several items listed below were intentionally removed later, including built-in MCP presets, token-aware MCP injection, and CAE run/log/artifact commands.

## Summary
- Use `opencode` as the CLI and agent baseline.
- Do not port VS Code-only systems from `scimate-vscode` such as webviews, the plan engine, or the fallback workspace/terminal/patch tools created for the extension host.
- Original proposal items that were later reduced or removed:
  - built-in account auth flows
  - token refresh and token-aware remote MCP calls
  - built-in SciMate MCP server presets
  - CAE run, log, and artifact commands
- Durable outcomes that remain relevant:
  - `scicli` branding, config paths, and local state
  - keeping `opencode` as the local coding baseline

## Implementation
- Rebrand the `opencode` baseline to `scicli` at the CLI/config/data-directory level.
- Keep the existing `opencode` local coding tools, permissions, sessions, and MCP tool exposure for the agent.
- Original configuration additions under consideration:
  - built-in MCP defaults for `cae-agent`, `origin`, and `rdkit`
- Original auth additions under consideration:
  - password signup/login
  - refresh-token based renewal
  - local token persistence
- Original MCP additions under consideration:
  - `streamable-http` transport
  - access-token injection when a tool schema declares `access_token`
- Original CLI additions under consideration:
  - `scicli auth register|login|logout|status`
  - `scicli mcp list-tools|call`
  - dedicated remote run-management commands

## Scope Decisions
- Keep `opencode` agent behavior as the main code-agent path.
- Do not add a second plan/replay engine.
- Do not port VS Code UI state, review flows, or local workspace MCP shims.
- This specific backend-defaults direction was later dropped.

## Validation
- Build and smoke test the retained commands.
- Verify the remaining auth flow if that subsystem is still enabled.
- Verify only the MCP behavior that still exists in the current branch.
