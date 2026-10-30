#!/usr/bin/env bash
# PreToolUse (Bash, only when the command matches "git commit *"):
# block the commit if Go code is unformatted, fails go vet, or fails unit tests.
# Fails open if go or jq is missing, because the Stop gate and CI still cover it.
set -u

INPUT=$(cat)

CWD=""
if command -v jq >/dev/null 2>&1; then
  CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
fi
[ -z "$CWD" ] && CWD="${CLAUDE_PROJECT_DIR:-$(pwd)}"
cd "$CWD" || exit 0

[ -f go.mod ] || exit 0
command -v go >/dev/null 2>&1 || exit 0

fail() {
  printf 'Commit blocked: %s\n' "$1" >&2
  exit 2
}

UNFORMATTED=$(gofmt -l . 2>&1)
if [ -n "$UNFORMATTED" ]; then
  fail "gofmt reports problems in:
$UNFORMATTED
Run gofmt -w on these files, then commit again."
fi

if ! OUT=$(go vet ./... 2>&1); then
  fail "go vet failed:
$(printf '%s\n' "$OUT" | tail -n 40)"
fi

if ! OUT=$(go test -short -count=1 -timeout 4m ./... 2>&1); then
  fail "unit tests failed:
$(printf '%s\n' "$OUT" | tail -n 60)"
fi

exit 0
