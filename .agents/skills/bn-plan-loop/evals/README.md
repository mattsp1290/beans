# Disposable evaluations

Run from the repository root:

```text
gofmt -d .agents/skills/bn-plan-loop/scripts/workflow_oracle.go
python3 .agents/skills/bn-plan-loop/evals/run_tests.py
```

The unit suite covers application-context and execution-map validation, semantic drift, workflow precedence/hold classification against the Go `issue.LoadWorkflow` oracle, state CAS, local lease exclusion, inert adversarial argv, monitor startup races, unchanged-monitor mutation exclusion, routing, and scenario discovery. `fixture_repo.py` proves the real repository-built `bn` can initialize an isolated hub/project, publish a valid ready two-node plan, link a marker issue, and leave local bare remotes current.

Every fixture path, remote, trace, config, and fake executable must resolve beneath one `mktemp` root. The fake accepts only state and trace files beneath `BN_PLAN_LOOP_FIXTURE_ROOT`; it never delegates to a live hub. Set fixture-specific `BEANS_HUB`, `BEANS_PROJECT`, `BN_ACTOR`, and `GIT_CONFIG_GLOBAL`. Never point an evaluation at `~/.beans`, GitHub, or a network remote.

`run_tests.py` compiles every Python source without writing bytecode and executes normal two-node completion, interrupted materialization recovery, feedback/approval holds, false-terminal rejection, contract drift, local concurrency fencing, and monitor boundaries. The real-`bn` scenarios run executor commands through the production persistent session, typed Beans/Git transactions, checkout claims, release/reacquire holds, and locked reconciliation; only simulated human actions bypass that session. Scenario request/expectation documents under `cases/` define the corresponding forward agent evaluations. For a live `/goal $bn-plan-loop <fixture-plan-id>` run, preserve the goal transcript and independent review artifacts as evidence. Inspect argv traces with `assert_trace.py`; unchanged monitor traces must use `--unchanged-monitor`. Never retain fixture roots or `__pycache__` in the repository.
