# Goal-driven MVP skills

These additive skills leave `bn-implementation-plan`, `bn-plan-loop`, and
`bn-handoff` unchanged. They build a bounded MVP on a selected non-default branch,
with a manual product check before the final PR to main.

| Goal | Result |
| --- | --- |
| `/goal $bn-produce` | One useful milestone, executable slices, and deferred backlog |
| `/goal $bn-build <milestone-issue-id>` | Reviewed slices integrated into the target and a runnable demo |
| `/goal $bn-audit <milestone-issue-id>` | An immutable snapshot assessed, with findings in a Beans request |

`bn-produce` also accepts an outcome or an existing plan/issue as context. The
builder consumes the new milestone contract, not an old plan execution map.
Existing detailed plans remain available when the change warrants them.

## Install the set

Use a reviewed Beans checkout containing all three folders. From its root, this
non-destructive installer adds links under `$HOME/.agents/skills`; it refuses to
replace any different existing entry. It validates the whole set before writing.

```sh
python3 - <<'PY'
from pathlib import Path
root = Path.cwd().resolve()
names = ("bn-produce", "bn-build", "bn-audit")
destination = Path.home() / ".agents" / "skills"
pairs = [(root / ".agents" / "skills" / name, destination / name) for name in names]
for source, link in pairs:
    if not (source / "SKILL.md").is_file():
        raise SystemExit(f"Missing skill: {source}")
    if (link.exists() or link.is_symlink()) and link.resolve() != source:
        raise SystemExit(f"Existing entry preserved: {link}")
destination.mkdir(parents=True, exist_ok=True)
for source, link in pairs:
    if not link.is_symlink() and not link.exists():
        link.symlink_to(source, target_is_directory=True)
    print(f"{link.name}: {link.resolve()}")
PY
```

All three folders must remain adjacent: the producer/auditor reference the
builder's shared contract. Do not copy one folder alone. Restart or open a new
Codex session if its skill catalog was loaded before installation. Verify all
three names are discoverable from the consumer repository. Python 3.9+, Git, a
current `bn` with issue/request commands, and source/hub remote access are needed.
The runtime uses POSIX file locking (macOS/Linux), matching the inspected hosts;
Windows needs a separately tested locking implementation before running it there.
Normal automatic skill selection remains enabled; mutation requires the explicit
active goal workflow and its recorded authority.

## Select an outcome once

Invoke the producer from the product repository. State what a user should be able
to do and any constraints you already know, for example:

```text
/goal $bn-produce Let me find a saved interview by status and resume it.
Preserve saved data and existing APIs. Integrate into mvp/saved-interviews.
Do not deploy or merge to main.
```

The producer grounds the milestone in current repository behavior, reuses supplied
decisions, and records its acceptance journey in a Beans epic. Optional fixes and
features go to the backlog. It finishes with the actual ID for the builder.

## Build and review

Run one builder coordinator per target. Start with two implementation slots plus
one reviewer if the harness permits; fewer is useful for a serial critical path.
Each implementing child uses an isolated branch/worktree. Run unrelated milestones
on other machines instead of competing for one queue.

Ownership is reserved on the source remote with immutable target and milestone
refs; `bn --claim` is only an assignment update. The coordinator holds a local
process lock while active and validates ownership before publishing. There is no
timeout takeover. Follow the [ownership and recovery procedure](../.agents/skills/bn-build/references/state-and-recovery.md)
after a crash or machine handoff. Stop the old coordinator/children before an
explicit administrative transfer; never delete locks merely because they look old.

One independent reviewer covers routine correctness and maintainability. A second
specialist checks high-risk changes. Reviews compare exact candidate/base SHAs
against the milestone target. Required fixes are batched; advisory paths allow
ordinary integration edits within the authorized outcome. Repository-required
checks and gates still apply.

Passing slices integrate into the target without a human hold between them.
Closing a slice means integrated into that named target, not released on main.
The builder checks the combined runtime journey and stops with an exact-SHA demo
handoff for manual testing. It does not poll indefinitely for the user.

## Audit and final PR

An optional auditor assesses a fixed target snapshot and files a normal Beans
request with reproducible findings. It does not edit product code. The builder
synchronizes the hub and triages linked requests at continuation and completion
boundaries. If it has stopped, resume explicitly with `/goal $bn-build <id>`.
Publishing a request alone does not wake a Codex session.

After manually testing the target, tell the builder which revision you accept.
It verifies current branch/check/audit state and creates or reconciles the final
PR to the default branch. Changed behavior needs renewed acceptance. You merge
the final PR; these skills do not deploy or write main.

Each role uses its own exclusive hub clone, with `BEANS_HUB`, separate `BEANS_HOME` state, and project selected
explicitly. Automated mutations never use the shared default hub checkout: Beans
can auto-commit concurrent hand edits, and its sync API has no conditional
clean-worktree transaction. Other sessions and editors must not write the dedicated
clone. All hub mutations use normal `bn` commands. The skills do not modify the backing
hub's layout, workflow configuration or schema. They do not bypass Git hooks,
force-push target branches, or weaken repository gates.

## Verification and qualification

From the Beans source checkout:

```text
python3 .agents/skills/bn-build/evals/run_tests.py
python3 .agents/skills/bn-plan-loop/evals/run_tests.py
make ci
```

The new evaluations isolate the hub, configuration, cache and remotes. Mechanism
tests are separate from forward agent runs and the real product pilot. See
[evaluation evidence](../.agents/skills/bn-build/evals/README.md) for cases and
actual scenario coverage. The [qualification record](../.agents/skills/bn-build/evals/qualification.md)
separates observed agent/pilot runs from specifications; a helper test alone does
not establish useful agent behavior.

The pilot measures brief-to-demo and approved-scope-to-demo, human interventions,
review cycles, failures, real concurrency and available usage accounting. The
north star is a verified MVP in 24 hours with at most one intermediate human
intervention. Count waiting honestly and record misses; do not weaken acceptance
to produce a passing metric. Final manual acceptance is separate from intermediate
interventions. One pilot does not prove a general speedup.

To roll back adoption, stop the goal and remove only the newly installed links
after checking their targets. Preserve source branches, ownership refs, issues
and partial work. The old skills remain available.
