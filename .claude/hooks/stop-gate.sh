#!/usr/bin/env bash
# Stop: before Claude ends a turn, verify Go code if any Go file changed since HEAD.
# Exit 2 sends the failure output back to Claude so it keeps working.
# A fingerprint of the Go changes avoids re-running when nothing changed since the last run.
# Claude Code stops overriding this hook after 8 consecutive blocks, so it cannot loop forever.
# Fails open if jq, go, or git is missing.
set -u

command -v jq >/dev/null 2>&1 || exit 0

INPUT=$(cat)
ACTIVE=$(printf '%s' "$INPUT" | jq -r '.stop_hook_active // false')
CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
[ -z "$CWD" ] && CWD="${CLAUDE_PROJECT_DIR:-$(pwd)}"
cd "$CWD" || exit 0

[ -f go.mod ] || exit 0
command -v go >/dev/null 2>&1 || exit 0
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || exit 0

GO_PATHS=('*.go' 'go.mod' 'go.sum')

CHANGED=$(git status --porcelain -- "${GO_PATHS[@]}" 2>/dev/null)
[ -z "$CHANGED" ] && exit 0

STATE_FILE="${CLAUDE_PROJECT_DIR:-$CWD}/.claude/.stop-gate-state"

fingerprint() {
  {
    git status --porcelain -- "${GO_PATHS[@]}"
    git diff HEAD -- "${GO_PATHS[@]}" 2>/dev/null || git diff -- "${GO_PATHS[@]}"
    while IFS= read -r -d '' f; do
      printf '%s\n' "$f"
      cat "$f" 2>/dev/null
    done < <(git ls-files -o --exclude-standard -z -- '*.go')
  } | cksum | cut -d' ' -f1
}

FP=$(fingerprint)

if [ -f "$STATE_FILE" ]; then
  LAST_FP=""
  LAST_RESULT=""
  read -r LAST_FP LAST_RESULT < "$STATE_FILE" || true
  if [ "$LAST_FP" = "$FP" ]; then
    # Same code as a passing run: nothing to verify.
    [ "$LAST_RESULT" = "pass" ] && exit 0
    # Same code as a failing run and Claude is already continuing because of this hook:
    # nothing changed, so let Claude stop and report instead of repeating the same failure.
    [ "$ACTIVE" = "true" ] && exit 0
  fi
fi

FAILS=""
add_fail() {
  FAILS="${FAILS}
== $1 ==
$(printf '%s\n' "$2" | tail -n "${3:-40}")
"
}

OUT=$(gofmt -l . 2>&1)
[ -n "$OUT" ] && add_fail "gofmt: run gofmt -w on these files" "$OUT"

if ! OUT=$(go build ./... 2>&1); then add_fail "go build" "$OUT"; fi
if ! OUT=$(go vet ./... 2>&1); then add_fail "go vet" "$OUT"; fi
if ! OUT=$(go test -short -count=1 -timeout 4m ./... 2>&1); then add_fail "go test -short" "$OUT" 60; fi

if { [ -f .golangci.yml ] || [ -f .golangci.yaml ] || [ -f .golangci.toml ]; } \
  && command -v golangci-lint >/dev/null 2>&1 \
  && git rev-parse HEAD >/dev/null 2>&1; then
  if ! OUT=$(golangci-lint run --new-from-rev=HEAD ./... 2>&1); then
    add_fail "golangci-lint (new issues only)" "$OUT"
  fi
fi

mkdir -p "$(dirname "$STATE_FILE")" 2>/dev/null

if [ -n "$FAILS" ]; then
  printf '%s %s\n' "$FP" fail > "$STATE_FILE" 2>/dev/null
  {
    echo "Go verification failed. Fix the problems below, then finish."
    printf '%s\n' "$FAILS"
  } >&2
  exit 2
fi

printf '%s %s\n' "$FP" pass > "$STATE_FILE" 2>/dev/null
exit 0
