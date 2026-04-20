---
name: learn-session
description: "Review the current session and extract knowledge worth persisting to AGENTS.md, .claude/agents/, or memory. Use at the end of a session to capture what was learned."
---

Review the current conversation to extract knowledge worth persisting for future sessions.

## What to look for

1. **User corrections** — where the user redirected the approach, rejected a suggestion, or clarified a preference
2. **Discovered patterns** — implementation patterns, conventions, or architectural knowledge that was hard to find and would save time next session
3. **Agent/tooling gaps** — things the tester or code-reviewer agents should know but don't

## What NOT to persist

- Things already documented in AGENTS.md or .claude/agents/
- Code-level details derivable by reading the source
- One-off debugging context that won't recur
- Verbose implementation guides — keep entries brief (1-2 lines for AGENTS.md)

## Process

1. Read the current state of AGENTS.md and .claude/agents/*.md
2. Read the memory index for this project at `~/.claude/projects/$(echo "$PWD" | tr '/' '-')/memory/MEMORY.md` (skip if absent)
3. Review the conversation so far: what was the task, what was learned, what corrections were made
4. For each finding, determine where it belongs:
   - **AGENTS.md** — brief convention or rule that applies to all sessions (1-2 lines max)
   - **.claude/agents/*.md** — specific to the tester or code-reviewer agent
   - **Memory** — user preferences, project context, feedback (not code patterns)
   - **Nowhere** — already documented or too specific to recur
5. Present the proposed changes for approval. Do NOT write anything until approved.
