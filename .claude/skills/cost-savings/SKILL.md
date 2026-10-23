---
name: cost-savings
description: Use this skill to keep Claude Code token cost down in this repo. Load it when running UAT or VM tests (`task vm:*`, `task test:*`), reading test or CI logs, printing large files or reports, or when the developer starts work on another PR in a session that already delivered one ("start #NNNN", "next issue", "let's begin the next PR"), or raises a PR with `gh pr create`. Covers running UAT in a subagent, writing self-checking UAT scripts, suggesting a fresh session between PRs, and keeping command output small.
---

# Cost savings

Every turn re-sends the whole context. Long sessions, raw logs and repeated UAT output are the main costs. These practices keep the context small.

## 1. Run UAT in a subagent

- The main session designs the UAT plan, then hands execution to a subagent.
- The main session receives only a short summary (about 1–2k tokens).
- Pick the subagent model by plan quality:
  - **Haiku** only when the plan is complete: exact commands or script, VM setup (`task vm:reset`, `task vm:sync`, ...), the expected value for every check, and the pass/fail rule.
  - **Sonnet** otherwise.
- The subagent returns a table: one row per check, with evidence (version, hash, rc).
- On the first unexpected result the subagent stops and reports what it saw. It does not troubleshoot. Diagnosis goes back to the main session.
- Do not switch the main session's own model for UAT:
  - Prompt caching is per model. A switch re-reads the whole context uncached, and switching back misses again.
  - A large context may not fit a smaller model's window and forces a compaction.

## 2. Write UAT scripts that check themselves

- Assert expected values inside the script. Print one `PASS` or `FAIL` line per check.
- Write full command output to log files. Read a log only when its check fails.
- Compare before and after snapshots with a diff that prints only changed lines. Use one fact per line, and hash Secret data and other values.

## 3. Suggest a new session for the next PR

When the developer starts another PR in a session that already delivered one, suggest a handover:

1. Write down what the next PR needs: memory notes, design and review notes, branch and PR state.
2. Start a fresh `claude` session in the new worktree. Alternatively run `/compact` with a short keep-list.
3. Open the new session with a short brief that points to those notes.

Make the suggestion once. Do not block the request.

## 4. Keep command output small

- Never `cat` a log or report. Use `head`, `tail`, `grep -c`, or a targeted `grep` with a line limit.
- Summarise CI job logs in a subagent. Do not download them into the main context.
- Use the Read tool with `offset` and `limit` for large files.

## 5. Save KVM runner capacity when raising PRs

- Add the `skip-kvm-runner` label to every new PR by default: `gh pr create --label skip-kvm-runner ...`. It skips the KVM-runner jobs (UAT, integration).
- Leave the label off the top PR of a stack, and off any PR whose only UAT is the CI run. Those PRs need the KVM jobs. They also replace some local UAT runs.
- When a PR becomes the top of its stack, or is ready to merge, remove the label: `gh pr edit <num> --remove-label skip-kvm-runner`.
