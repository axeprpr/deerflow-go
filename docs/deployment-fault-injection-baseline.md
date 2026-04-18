# Deployment Fault-Injection Baseline

This baseline documents runtime-stack deploy-split production checks implemented in `internal/stackcmd/process_launcher_integration_test.go`.

## Scope

- Restart resilience for runtime worker-like processes.
- Dependency readiness gating before dependent process launch.
- Failure isolation behavior (`failureIsolation=true`) under mixed healthy/failing processes.

## Coverage

1. `TestProcessLauncherRestartsFailingProcessUntilReady`
- Injects a deterministic first-start failure.
- Verifies `on-failure` restart policy recovers process and reaches `/healthz`.
- Asserts restart count observed by helper process state file.

2. `TestProcessLauncherDependencyGateBlocksDependentStartUntilReady`
- Injects delayed readiness on dependency process.
- Verifies dependent process does not start before dependency becomes ready.
- Asserts dependency readiness precedes dependent readiness.

3. `TestProcessLauncherFailureIsolationKeepsHealthyProcessRunningUntilClose`
- Runs one healthy and one immediate-failing process with `failureIsolation=true`.
- Verifies launcher does not fail fast while healthy process is still running.
- Verifies final `Start()` error reports failed process after explicit close.

## Notes

- Validation is process-level and cross-platform friendly (no shell script assets).
- Helpers are implemented as Go test-binary helper processes gated by `DEERFLOW_PROCESS_HELPER=1`.
- This baseline complements manifest/host-plan static validation in existing `bundle_test` and `command_test`.
