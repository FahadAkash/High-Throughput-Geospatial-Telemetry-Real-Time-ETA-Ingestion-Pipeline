#!/usr/bin/env bash
# PreToolUse (Edit|Write): block edits to files Claude must not change by hand.
# Fails closed: without jq the hook cannot inspect the call, so it blocks it.
set -u

if ! command -v jq >/dev/null 2>&1; then
  echo "Blocked: jq is required by .claude/hooks but was not found on PATH. Install jq." >&2
  exit 2
fi

INPUT=$(cat)
FILE=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE" ] && exit 0
FILE="${FILE//\\//}"
BASE="${FILE##*/}"

block() {
  echo "Blocked: $1" >&2
  exit 2
}

# Safe templates first, because case patterns are matched top to bottom.
case "$BASE" in
  .env.example|.env.sample|.env.template) exit 0 ;;
esac

case "$BASE" in
  .env|.env.*|*.pem|*.key|*.p12|*.pfx|id_rsa*)
    block "$FILE looks like a secrets file. Ask the user to edit it." ;;
  go.sum|go.work.sum)
    block "$BASE is managed by the Go toolchain. Run 'go mod tidy' or 'go get' instead of editing it." ;;
  *.pb.go|*_gen.go|*.gen.go)
    block "$FILE is generated code. Change the source and regenerate it." ;;
esac

case "$FILE" in
  */.git/*|.git/*)
    block "Edits inside .git are not allowed." ;;
  */vendor/*|vendor/*)
    block "vendor/ is managed by 'go mod vendor'. Do not edit it by hand." ;;
  */secrets/*|secrets/*)
    block "secrets/ is off limits. Ask the user to edit it." ;;
  */bench/results/*|bench/results/*)
    block "bench/results/ holds generated benchmark output. Re-run the benchmark instead of editing results." ;;
esac

# Files that declare themselves generated.
if [ -f "$FILE" ] && head -n 5 "$FILE" 2>/dev/null | grep -q 'Code generated' \
  && head -n 5 "$FILE" 2>/dev/null | grep -q 'DO NOT EDIT'; then
  block "$FILE is marked 'Code generated ... DO NOT EDIT.' Change the source and regenerate it."
fi

# go.mod: escalate to the user instead of silently allowing or denying.
if [ "$BASE" = "go.mod" ]; then
  jq -n '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "ask",
      permissionDecisionReason: "Direct edit to go.mod. Prefer go get or go mod tidy. Approve only if intentional, for example a go directive or replace change."
    }
  }'
  exit 0
fi

exit 0
