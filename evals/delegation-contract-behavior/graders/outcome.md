---
type: llm
weight: 2
---

PASS if the response gives each subagent its own isolated write scope (for example a separate worktree, or clearly non-overlapping files) and distinguishes an orchestrating role from the writers, so two agents cannot edit the same file at the same time.
FAIL if the response only lists the three tasks or suggests running them without addressing how simultaneous writes are kept from colliding.
