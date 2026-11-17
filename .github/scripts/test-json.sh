#!/usr/bin/env bash
# Runs `go test -json` and prints one line per package with its duration, slowest
# first, so a slow package shows in the job log without reading the raw events.
# A failure prints the output of the failed tests and packages.
# Usage: test-json.sh <label> <go test arguments...>
set -uo pipefail

label=$1
shift
out="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/go-test-${label}.json"

go test -json "$@" >"$out"
status=$?

echo "::group::Duration per package (${label})"
jq -rs '
  [.[] | select(.Test == null and .Package != null and (.Action | IN("pass", "fail")))]
  | sort_by(-(.Elapsed // 0))[]
  | "\((.Action | ascii_upcase))  \(.Elapsed // 0)s  \(.Package)"
' "$out"
jq -rs '
  [.[] | select(.Test != null and (.Action | IN("pass", "fail", "skip")))]
  | group_by(.Action) | map("\(length) \(.[0].Action)") | join(", ")
  | "tests (" + $label + "): " + .
' --arg label "$label" "$out"
echo "::endgroup::"

if [ "$status" -ne 0 ]; then
  jq -rs '
    ([.[] | select(.Action == "fail" and .Test != null) | .Package + " " + .Test]) as $failed
    | .[]
    | select(
        .Action == "build-output"
        or (.Action == "output" and (.Test == null or ((.Package + " " + .Test) | IN($failed[]))))
      )
    | .Output
  ' -j "$out"
fi

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "### go test (${label})"
    echo
    echo "| Package | Result | Duration |"
    echo "| --- | --- | --- |"
    jq -rs '
      [.[] | select(.Test == null and .Package != null and (.Action | IN("pass", "fail")))]
      | sort_by(-(.Elapsed // 0))[]
      | "| \(.Package) | \(.Action) | \(.Elapsed // 0)s |"
    ' "$out"
    echo
  } >>"$GITHUB_STEP_SUMMARY"
fi

exit "$status"
