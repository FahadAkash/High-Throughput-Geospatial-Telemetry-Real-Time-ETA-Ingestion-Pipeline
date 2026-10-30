#!/usr/bin/env bash
# PostToolUse (Edit|Write): format Go files and report syntax errors back to Claude.
# Fails open: if jq or gofmt is missing, the hook does nothing.
set -u

command -v jq >/dev/null 2>&1 || exit 0
command -v gofmt >/dev/null 2>&1 || exit 0

INPUT=$(cat)
FILE=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE" ] && exit 0
FILE="${FILE//\\//}"

case "$FILE" in
  *.go) ;;
  *) exit 0 ;;
esac
[ -f "$FILE" ] || exit 0

# Syntax check first. gofmt -e reports every error and does not rewrite the file.
if ! ERR=$(gofmt -e -l "$FILE" 2>&1 >/dev/null); then
  {
    echo "Go syntax error after editing $FILE:"
    echo "$ERR"
  } >&2
  exit 2
fi

# Imports first, then the stricter formatter if present, then plain gofmt.
if command -v goimports >/dev/null 2>&1; then
  goimports -w "$FILE" >/dev/null 2>&1
fi
if command -v gofumpt >/dev/null 2>&1; then
  gofumpt -w "$FILE" >/dev/null 2>&1
else
  gofmt -w "$FILE" >/dev/null 2>&1
fi

exit 0
