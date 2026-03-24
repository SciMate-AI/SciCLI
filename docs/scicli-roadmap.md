# `scicli` Product Roadmap

## Goal

Build `scicli` into a system that combines:

- the best parts of `oh my opencode` / `opencode`
  - strong local coding loop
  - terminal-first UX
  - tool execution, patching, sessions, delegated tasks
  - workbench-style operator control
- the best parts of `EvoScientist`
  - research-oriented planning
  - experiment loop execution
  - candidate comparison and iteration
  - artifact-centered scientific workflow memory

The target is not "a coding agent with some science tools".
The target is "a scientific workbench that can code, run experiments, evaluate results, and evolve the next step".

## Current State

`scicli` already has a meaningful part of the `opencode` side:

- terminal coding agent baseline
- local toolchain and permission model
- multi-session TUI
- delegated tasks and task inspector
- work modes including `ultrawork`
- context compaction
- project memory support

It also has part of the science-integration layer:

- configurable MCP support
- no built-in remote CAE/SciMate command surface
- bundled skill discovery

It does not yet have the core `EvoScientist` loop:

- no explicit research objective model
- no hypothesis or experiment plan object
- no structured experiment lifecycle
- no scoring/ranking of candidate runs
- no evolutionary iteration across results
- no persistent research-state graph tying prompts, code, runs, results, and conclusions together

## Product Thesis

`scicli` should be organized around two loops:

1. Operator loop
   - a human steers objectives, risk, approvals, and scope in a strong terminal UI
2. Research loop
   - the system proposes, executes, evaluates, and refines experiments in a structured way

`oh my opencode` contributes the operator loop.
`EvoScientist` contributes the research loop.
`scicli` should be the place where both loops share the same workspace, artifacts, and session history.

## Design Principles

- Terminal-first, not dashboard-first.
- Local-first for code and workspace state.
- Structured state beats prompt-only state for experiments.
- Explicit artifacts beat free-form chat summaries.
- Keep one primary agent path; avoid building a second disconnected engine unless the loop truly requires it.
- Scientific autonomy must remain inspectable and interruptible by the operator.

## Roadmap

### Phase 0: Stabilize The Base

Status: complete

Objectives:

- remove confusing duplicated startup flows
- remove hidden product assumptions such as built-in MCP defaults
- remove bundled remote CAE/SciMate command surfaces that no longer exist
- clarify the product direction in docs and UI labels

Deliverables:

- one real onboarding flow
- explicit MCP configuration
- no built-in remote run/log/artifact commands
- command palette terminology aligned with actual behavior
- roadmap and architecture docs updated

Exit criteria:

- first-run flow is singular and predictable
- docs describe the current product honestly

### Phase 1: Research Session Model

Status: in progress

Objectives:

- introduce a first-class research session on top of chat sessions
- persist objective, constraints, domain, success criteria, and current stage

Deliverables:

- `research` domain model in local state/db
- session metadata for:
  - objective
  - hypotheses
  - experiment queue
  - evaluation criteria
  - decision log
- TUI inspector section for research-state summary

Current slice already landing:

- persisted local research state keyed by session
- minimal objective + stage storage
- command-palette action and `/research set` slash command
- inspector summary panel for the active research session

Exit criteria:

- an operator can reopen a session and see the current research state without reading the whole chat
- delegated tasks can attach outputs back to a research session

### Phase 2: Structured Experiment Loop

Status: in progress

Objectives:

- move from ad hoc tool use to a clear experiment lifecycle

Lifecycle:

1. define objective
2. propose hypotheses
3. generate candidate experiment plans
4. execute runs
5. collect artifacts
6. evaluate outcomes
7. decide next iteration

Deliverables:

- experiment-plan schema
- experiment-run records linked to tasks and artifacts
- evaluation-result schema
- command/TUI affordances to create, inspect, rerun, and compare experiments

Current slice already landing:

- experiment-plan schema in persisted research state
- delegated task runs can attach to the active experiment automatically
- evaluation records with score, decision, and summary
- the agent can propose the next candidate experiment from existing experiment history
- experiment lineage now survives proposal and rerun flows, enabling comparison across generations
- slash commands and inspector summaries for experiment creation and review

Exit criteria:

- each run has a machine-readable parent plan and evaluation record
- the agent can continue from prior experiment state instead of rediscovering context from scratch

### Phase 3: Artifact-Centered Scientific Memory

Status: in progress

Objectives:

- make outputs reusable as scientific objects, not just chat text or loose files

Deliverables:

- artifact index for:
  - code snapshots
  - prompts
  - configs
  - run logs
  - metrics
  - generated reports
  - derived conclusions
- provenance links:
  - objective -> hypothesis -> plan -> run -> artifact -> evaluation
- searchable artifact browser in the inspector

Current slice already landing:

- completed experiment runs persist local artifact refs
- provenance now links experiment runs to prompt, response, timeline, and modified-file artifacts
- inspector exposes a searchable artifact browser across the current research session
- slash commands can list and inspect artifact provenance without leaving the terminal

Exit criteria:

- users can answer "which run produced this result and why was it kept?" from the stored state
- downstream tasks can consume prior artifacts directly

### Phase 4: Evolutionary Iteration

Status: started

Objectives:

- add the real `EvoScientist` advantage: iterative improvement across candidate experiments

Deliverables:

- candidate generator for prompts, parameters, workflows, or code variants
- evaluator that scores runs against explicit criteria
- selection policy:
  - keep
  - discard
  - mutate
  - branch
- lineage tracking for experiment families

Current slice already landing:

- evaluation decisions are now normalized to explicit selection policies
- experiment lineage stores generation and root IDs instead of relying only on parent references
- research state now persists one promoted experiment as the current best candidate on the active lineage
- operators can create the next generation directly from a `mutate` or `branch` decision with `/experiment evolve`
- proposed child experiments now carry an explicit `mutate` or `branch` strategy instead of only free-form rationale

Exit criteria:

- the system can run at least one full propose -> execute -> evaluate -> evolve cycle with stored lineage
- users can compare generations, not just isolated runs

### Phase 5: Scientific Operator Workbench

Status: started

Objectives:

- make the TUI feel like a research workbench, not only a coding chat

Deliverables:

- left pane:
  - research navigation
  - objective
  - hypotheses
  - queued experiments
- center pane:
  - main operator chat and execution stream
- right pane:
  - active tasks
  - run state
  - artifacts
  - evaluation summaries
- fast actions for:
  - approve batch
  - stop branch
  - promote candidate
  - compare runs
  - generate report

Current slice now landing:

- the left workbench pane now mirrors research state instead of acting as a static placeholder
- operators can see the current objective, stage, active candidate, promoted candidate, and queued follow-up experiments without leaving the main chat
- the right-side lineage panel now surfaces per-lineage best candidate, current candidate, queue pressure, and next-step hints

Exit criteria:

- the workbench makes experiment state visible without leaving the terminal
- operator control remains stronger than autonomous drift

### Phase 6: Domain Packs

Status: later

Objectives:

- package research loops for real scientific domains instead of keeping everything generic

Candidate domains:

- CAE simulation
- chemistry / RDKit workflows
- literature-driven research
- materials workflows
- bioinformatics pipelines

Deliverables:

- domain-specific plan templates
- evaluator templates
- artifact parsers
- report generators
- run comparators

Exit criteria:

- at least one domain has an end-to-end polished loop with minimal manual glue

## Priority Order

Recommended execution order:

1. Phase 1: Research session model
2. Phase 2: Structured experiment loop
3. Phase 3: Artifact-centered scientific memory
4. Phase 5: Scientific operator workbench
5. Phase 4: Evolutionary iteration
6. Phase 6: Domain packs

Rationale:

- without structured state, evolutionary behavior will collapse back into prompt improvisation
- without artifacts and evaluation records, scientific iteration will be untrustworthy
- the UI should reflect the model after the model exists

## What "Done" Looks Like

`scicli` will be on the right track when a user can:

- define a research objective once
- generate several candidate experiment plans
- run them through tools or remote backends
- inspect artifacts and evaluation summaries in one terminal workbench
- branch from the best candidate
- keep a visible lineage of why the next generation exists

At that point, the product is no longer "SciMate-flavored opencode".
It becomes a true research workbench with a strong coding core.
