---
type: llm
weight: 2
---

PASS if the plan has the lead itself performing none of the three edits directly, and instead assigns each of the three tasks to its own subagent, naming for each dispatch: (a) that subagent's role, (b) the boundaries of what it may touch, and (c) what it must hand back to the lead.
FAIL if the plan has the assistant doing the rename, the tests, or the docs update itself, or delegates the work without stating a role, boundaries, and a return step for each dispatch.
