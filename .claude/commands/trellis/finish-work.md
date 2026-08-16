# Finish Work

Wrap up the current session: archive the active task (and any other completed-but-unarchived tasks the user wants to clean up), record the session journal, and create one combined wrap-up commit. Product/code commits happen in workflow Phase 3.4 before this command. Archive changes must never become a standalone commit.

## Step 1: Survey current state

```bash
python3 ./.trellis/scripts/get_context.py --mode record
```

This prints:

- **My active tasks** — review whether any besides the current one are actually done (code merged, AC met) and should be archived this round.
- **Git status** — quick visual on what's dirty.
- **Recent commits** — you'll need their hashes in Step 4 for `--commit`.

If `--mode record` surfaces other completed tasks not tied to the current session, surface them to the user with a one-shot confirmation: "These N tasks look done — archive them too in this round? [y/N]". Default is no; the current active task is always archived in Step 3 regardless.

## Step 2: Sanity check — classify dirty paths

Run:

```bash
git status --porcelain
```

Do not automatically ignore pre-existing paths under `.trellis/workspace/` or `.trellis/tasks/`. Before this command generates new bookkeeping files, classify every dirty path so unrelated work cannot leak into the wrap-up commit.

For each remaining dirty path, decide whether it belongs to **the current task** or to **other parallel work** (e.g., another terminal window editing the same repo). Heuristics:

- Paths referenced in the current task's `prd.md` / `implement.jsonl` / `check.jsonl` → current task
- Paths in code areas matching the task's stated scope, or that you remember editing this session → current task
- Paths in unrelated areas you have no recollection of touching this session → other parallel work

Then route:

- **Product/code changes from the current task** — bail out with:
  > "Working tree has uncommitted code changes from this task: `<list>`. Return to workflow Phase 3.4 to commit them before running `/trellis:finish-work`."

  Do NOT run `git commit` here. Do NOT prompt the user to commit. The user goes back to Phase 3.4 and the AI drives the batched commit there.
- **Related Trellis wrap-up changes** (spec, workflow, finish-work instructions, journal, or task metadata intended for this same wrap-up) — keep them unstaged and include them in the final combined commit in Step 5.
- **All remaining paths look unrelated** (other parallel-window work) — report them once and continue to Step 3:
  > "FYI, dirty files outside this task's scope — leaving them for the other window: `<list>`."
- **Genuinely unsure** — ask the user once: "Are `<list>` this task's work I forgot to commit, or another window's? (commit / ignore)" — then route per their answer.

## Step 3: Archive task(s)

```bash
python3 ./.trellis/scripts/task.py archive <task-name> --no-commit
```

At minimum: the current active task (if any). Plus any extra tasks the user confirmed in Step 1. Always pass `--no-commit`; repeat the command for each task and leave all archive moves unstaged until Step 5.

If there is no active task and the user did not confirm any cleanup archives, skip this step.

## Step 4: Record session journal

```bash
python3 ./.trellis/scripts/add_session.py \
  --title "Session Title" \
  --commit "hash1,hash2" \
  --summary "Brief summary" \
  --no-commit
```

Use the work-commit hashes produced in Phase 3.4 (visible in Step 1's `Recent commits` list, or via `git log --oneline`) for `--commit`. Always pass `--no-commit`; there are no archive commit hashes to include.

## Step 5: Commit the combined wrap-up

1. Run `git status --short` and identify only the files produced by Steps 3–4 plus any related spec/workflow wrap-up files explicitly classified in Step 2.
2. Stage those exact paths with `git add -- <paths>`. Never use `git add .`, `git add -A`, or force-add `.trellis/`.
3. Inspect `git diff --cached --name-status` and `git diff --cached --check`. If unrelated or previously staged files appear, stop and report them instead of committing.
4. Create exactly one wrap-up commit, for example `chore(trellis): archive tasks and record session`.

Final git log order: `<work commits from Phase 3.4>` → `<one combined archive + journal + related wrap-up commit>`. An archive-only commit is forbidden.
