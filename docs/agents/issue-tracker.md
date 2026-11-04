# Issue tracker: local Markdown

Issues and specs for this repo live as markdown files in `.scratch/`.

## Scratch root (primary worktree)

Durable tracker content belongs on the repository's **primary worktree** — the first path in `git worktree list`, usually the checkout under `~/Work/...`. Do **not** keep specs, tickets, grilling decisions, STATUS, research notes, or the Project Hub only inside disposable agent worktrees (`~/.t3/worktrees/`, `~/.cache/worktrees/`, `~/worktrees/`, `.worktrees/`). Those checkouts are removed often and the files disappear with them.

Resolve the scratch root before creating or updating tracker files:

```bash
# From any linked worktree of the project:
git worktree list --porcelain | awk '/^worktree / { print $2; exit }'
```

Write under `<primary>/.scratch/...`. Pass that same `<primary>` to `generate-hub.mjs` and `/board`. Code edits may stay on a feature worktree; tracker writes still target `<primary>`.

**Ephemeral exception:** short-lived draft artifacts tied to a feature branch (for example `.scratch/pr-description/body.md` or `.metrics/`) may live on the feature worktree. They are not the issue tracker and must not replace specs or tickets on the primary.

When `/setup` scaffolds `.scratch/`, create it on the primary worktree, not on a linked agent checkout.

## Conventions

- One feature per directory: `.scratch/<feature-slug>/`
- The spec is `.scratch/<feature-slug>/spec.md`
- Implementation issues are one file per ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01`, never a single combined tickets file
- Triage state is recorded as a `Status:` line near the top of each issue file:
  - `needs-triage`: newly created or needs initial assessment
  - `ready-for-agent`: ready to be picked up and implemented by an agent
  - `claimed`: an agent or developer has claimed this ticket and is working on it
  - `blocked`: blocked by unresolved dependencies or missing information
  - `resolved`: fully implemented, tested, and verified
  - `wontfix`: closed without action
- Blocking relationships are declared near the top:
  `**Blocked by:** 01, 02` or `**Blocked by:** None (can start immediately)`
- Acceptance criteria are declared as checkboxes:
  `- [ ] Criterion 1`
  `- [x] Completed criterion`
- Comments and conversation history append to the bottom of the file under a `## Comments` heading

## Visual dashboard

An interactive Project Hub dashboard is maintained at `.scratch/index.html` on the primary worktree.

Refresh it with `/setup`: resolve the setup skill directory, then `node <setup>/scripts/generate-hub.mjs <primary-worktree-root>`.

## When a skill says "publish to the issue tracker"

Resolve the primary worktree (section above). Create the file under `<primary>/.scratch/<feature-slug>/` (creating the directory if needed). Refresh the Project Hub against `<primary>`.

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path on the primary worktree. The user or prompt will normally pass the path or ticket number directly.

## Claiming and resolving tickets

### Prefer subagents

When implementing a ticket, dispatch a dedicated subagent for the vertical slice with a clean context. The main agent works with the operator and tracks which dependencies have been satisfied.

### Claim

Before starting, set `Status: claimed` in the ticket file on the primary worktree.

### Resolve

Check all acceptance criteria (`- [x]`), set `Status: resolved`, and refresh the Project Hub on the primary worktree.
