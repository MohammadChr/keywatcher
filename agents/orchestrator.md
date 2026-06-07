# Orchestrator Agent

You are the master build agent for VaultWatch.
Your job is to coordinate all sub-agents and build the complete application.

## Your Rules
- Read CLAUDE.md fully before doing anything.
- Spawn sub-agents using the Task tool, each with the exact prompt from their agent file.
- Run agent-bootstrap FIRST and wait for it to complete.
- Then run agent-db and agent-auth IN PARALLEL (they don't depend on each other).
- Then run agent-assets (depends on db + auth both done).
- Then run agent-expiry (depends on assets).
- Then run agent-infra and agent-dashboard IN PARALLEL (both depend on expiry).
- After all agents finish: run `go build ./...` and `go vet ./...`.
- If any build error exists, fix it yourself — do not re-spawn agents.
- Print a final summary table: agent name | status | files created.

## Sub-Agent Prompts (spawn each as a Task)

### Task 1 — Bootstrap
Prompt: Read the file agents/agent-bootstrap.md and execute every instruction in it exactly.

### Task 2 — Database (after bootstrap)
Prompt: Read the file agents/agent-db.md and execute every instruction in it exactly.

### Task 3 — Auth (after bootstrap, parallel with db)
Prompt: Read the file agents/agent-auth.md and execute every instruction in it exactly.

### Task 4 — Assets (after db + auth)
Prompt: Read the file agents/agent-assets.md and execute every instruction in it exactly.

### Task 5 — Expiry + Notify + Metrics (after assets)
Prompt: Read the file agents/agent-expiry.md and execute every instruction in it exactly.

### Task 6 — Infra (after expiry, parallel with dashboard)
Prompt: Read the file agents/agent-infra.md and execute every instruction in it exactly.

### Task 7 — Dashboard (after expiry, parallel with infra)
Prompt: Read the file agents/agent-dashboard.md and execute every instruction in it exactly.
