# VaultWatch — Vibe Coding Entry Point

## You only need to paste ONE thing into Claude Code.
## Copy everything between the lines below and paste it into your terminal running `claude`.

---

Read the file CLAUDE.md fully. Then read agents/orchestrator.md fully.

You are the Orchestrator. Your job is to build the complete VaultWatch application
by spawning sub-agents in the correct order using the Task tool.

Execute this exact sequence:

STEP 1 — Run this task (wait for it to complete before continuing):
  Task prompt: "Read the file agents/agent-bootstrap.md and execute every instruction in it exactly. 
  After finishing, run `go build ./...` and report the result."

STEP 2 — Run these two tasks IN PARALLEL (start both, wait for both):
  Task A prompt: "Read the file agents/agent-db.md and execute every instruction in it exactly.
  After finishing, run `go build ./...` and report the result."
  
  Task B prompt: "Read the file agents/agent-auth.md and execute every instruction in it exactly.
  After finishing, run `go build ./...` and report the result."

STEP 3 — Run this task (wait for it to complete):
  Task prompt: "Read the file agents/agent-assets.md and execute every instruction in it exactly.
  After finishing, run `go build ./...` and report the result."

STEP 4 — Run this task (wait for it to complete):
  Task prompt: "Read the file agents/agent-expiry.md and execute every instruction in it exactly.
  After finishing, run `go build ./...` and report the result."

STEP 5 — Run these two tasks IN PARALLEL (start both, wait for both):
  Task A prompt: "Read the file agents/agent-infra.md and execute every instruction in it exactly."
  
  Task B prompt: "Read the file agents/agent-dashboard.md and execute every instruction in it exactly."

STEP 6 — After all tasks finish:
  Run `go build ./...`
  Run `go vet ./...`
  Fix any remaining errors yourself without spawning more agents.
  Print a final summary table showing: Agent | Status | Key files created.

---
