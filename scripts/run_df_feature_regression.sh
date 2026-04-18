#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATUS=0

export HOME="${HOME:-/root}"
export GOPATH="${GOPATH:-/root/go}"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
export GOCACHE="${GOCACHE:-$HOME/.cache/go-build}"

run_case() {
  local name="$1"
  shift
  echo "== ${name} =="
  if "$@"; then
    echo "[PASS] ${name}"
  else
    echo "[FAIL] ${name}"
    STATUS=1
  fi
  echo
}

cd "$ROOT_DIR" || exit 1

echo "Running upstream DeerFlow feature regression baseline (P0/P1 core)..."
echo

run_case "P0/P1 langgraphcompat deterministic" \
  go test ./pkg/langgraphcompat -run \
  'TestIssue2143And2125CustomSkillUsesMountedSkillPath|TestIssue2139DefaultRuntimeExposesWebSearchTools|TestIssue2126CompactionKeepsCompletedOutputStateForRemainingWrite|TestResolveRunConfigUsesGatewayProviderModelForAlias|TestResolveRunConfigResolvesDefaultModelAliasToProviderModel|TestResolveRunConfigKeepsReasoningEffortForSupportedGatewayModel|TestResolveRunConfigUsesGatewayModelRequestTimeout|TestCrossInstanceCancelIsIdempotentUnderConcurrentStaleRequests' \
  -count=1

run_case "P0/P1 agent deterministic" \
  go test ./pkg/agent -run \
  'TestIssue2123ToolCallsRemainPairedWithToolResults|TestIssue2123ToolCallContinuitySurvivesFailedToolRetry|TestIssue2124ProviderErrorsRemainDiagnosable|TestIssue2139WebSearchTextSimulationTriggersRetryAndRealToolCall' \
  -count=1

run_case "P1 web tool deterministic" \
  go test ./pkg/tools/builtin -run \
  'TestWebSearchHandler|TestIssue2139WebSearchHandlerReturnsDiagnosableError|TestWebFetchHandler|TestWebFetchPrefersMainContent|TestWebFetchScoresReadableContainerOverCookieBanner|TestWebFetchMarkdownifiesLinksAndLists|TestWebSearchUsesUserAgent' \
  -count=1

if [ "${RUN_LIVE:-0}" = "1" ]; then
  if [ -z "${DEERFLOW_LIVE_BASE_URL:-}" ]; then
    echo "[FAIL] RUN_LIVE=1 but DEERFLOW_LIVE_BASE_URL is empty"
    STATUS=1
  else
    echo "Running live behavior regression against ${DEERFLOW_LIVE_BASE_URL} ..."
    echo
    export DEERFLOW_LIVE_BEHAVIOR=1
    export DEERFLOW_LIVE_STREAM_TIMEOUT="${DEERFLOW_LIVE_STREAM_TIMEOUT:-6m}"

    run_case "P0 live concrete prompt" \
      go test ./pkg/langgraphcompat -run 'TestLiveBehaviorConcretePromptExecutesWithoutClarification' -count=1 -v

    run_case "P0 live ambiguous prompt" \
      go test ./pkg/langgraphcompat -run 'TestLiveBehaviorAmbiguousPromptRequestsMoreDetail' -count=1 -v

    run_case "P0 live clarify-then-execute followup" \
      go test ./pkg/langgraphcompat -run 'TestLiveBehaviorClarificationThenConcreteFollowupExecutes' -count=1 -v

    run_case "P0 live CSV long-run no-repeat" \
      go test ./pkg/langgraphcompat -run 'TestLiveIssue2126CSVLongRunDoesNotRepeatCompletedOutputs' -count=1 -v

    run_case "P1 live web search" \
      go test ./pkg/tools/builtin -run 'TestLiveIssue2139WebSearchHandlerReachesNetwork' -count=1 -v
  fi
fi

if [ "$STATUS" -eq 0 ]; then
  echo "All selected regression cases passed."
else
  echo "Some regression cases failed."
fi

exit "$STATUS"
