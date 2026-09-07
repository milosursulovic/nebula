---
name: nebula-new-phase
description: Start the next NEBULA development phase (or a specific phase number) — looks up progress and the relevant spec section, then enters plan mode following this project's established conventions. Use when the user says "idemo na fazu N" / "next phase" / "start phase N" for the NEBULA project.
---

# Starting a NEBULA phase

This codifies the ritual already used for Phases 1-7 — follow it exactly
rather than re-deriving an approach each time.

## Steps

1. **Read `CLAUDE.md`** in the repo root — its "Progress" table says which
   phases are done (with commit hashes) and names the next one. If the
   user named a specific phase number/argument to this skill, use that
   instead of the table's "next" suggestion (they may be revisiting an
   earlier phase or jumping ahead intentionally — don't assume it's an
   error, but do mention if it looks out of order).

2. **Look up the phase's spec section** using the `nebula-spec-reader`
   agent (or `CLAUDE.md`'s own section index table if the answer is
   short enough not to need a full read) — get the phase's own
   description (spec section 55+, "Development Phases") *and* any
   cross-referenced sections it depends on (e.g. Phase 5's "Scheduler"
   phase description points at sections 16-19 for reservation/concurrency
   detail — don't plan off the one-paragraph phase summary alone).

3. **Check for scope traps before planning**: read `CLAUDE.md`'s
   "Architecture conventions" section for anything already decided that
   bears on this phase (e.g. don't re-wire the scheduler until Phase 8 per
   Phase 6's own deferral note; domain packages don't import each other
   except where already justified). If the new phase is exactly the one
   a prior phase's deferral was pointing at (check `CLAUDE.md` and recent
   commit messages for "deferred to Phase N" language), that deferred
   work belongs in this plan.

4. **`EnterPlanMode`.** Write the plan following the shape every prior
   phase plan has used:
   - **Context**: why this phase, citing the specific prior-phase commits
     it builds on and the spec section(s).
   - Data model changes (next sequential migration number).
   - Package layout (new `internal/<domain>/` following the established
     file pattern, or additions to existing packages).
   - Implementation details with reasoning for any non-obvious choice —
     if there's a genuine fork with real tradeoffs, use `AskUserQuestion`
     *before* finalizing the plan text, not after.
   - Explicit scope boundaries: what this phase deliberately does NOT do
     yet, and which later phase/mechanism will do it — this project's
     plans consistently call this out rather than silently under-building
     or over-building.
   - A verification section shaped like `CLAUDE.md`'s checklist.

5. **`ExitPlanMode`** to request approval as usual. Do not start
   implementing before that approval, same as any other plan-mode task.

## After approval

Standard implementation loop applies (not part of this skill specifically
— see `CLAUDE.md`'s "Workflow for a new phase" steps 4-9): migrate, code,
`gofmt`/`build`/`vet`/`test -race`, verify against the real stack (the
`nebula-verifier` agent is well suited to this step), update `README.md`
(feature-grouped, no phase numbers), update `CLAUDE.md`'s Progress table,
commit and push.
