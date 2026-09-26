---
tags: [negative, delegation-contract]
description: >-
  Asks for a second opinion on a proposal without naming another agent as the
  source. Without that, Claude may just answer with its own feedback -- the
  skill is about dispatching agents, not about Claude reviewing a proposal
  itself. Moved here from trigger-de-2 (run 3, 2026-09-26): the skill never
  fired for this prompt in any of 6 runs, with or without a description
  clarifying "second opinion from another agent".
allowed_tools: [Read, Glob, Grep, Skill]
---

Ich brauche eine zweite Meinung zu diesem Vorschlag, bevor ich ihn umsetze: Wir sollten die Konfigurationsdatei so aendern, dass die Zeitueberschreitung automatisch verdoppelt wird, wenn ein Dienst dreimal hintereinander fehlschlaegt.
