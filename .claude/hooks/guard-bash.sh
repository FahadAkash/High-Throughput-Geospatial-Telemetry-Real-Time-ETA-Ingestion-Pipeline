#!/usr/bin/env bash
# PreToolUse (Bash): block destructive commands before they run.
# This is a guardrail, not a security boundary. Hard rules belong in permissions.deny.
# Fails closed: without jq the hook cannot inspect the command, so it blocks it.
set -u

if ! command -v jq >/dev/null 2>&1; then
  echo "Blocked: jq is required by .claude/hooks but was not found on PATH. Install jq." >&2
  exit 2
fi

INPUT=$(cat)
CMD=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty')
[ -z "$CMD" ] && exit 0

deny() {
  echo "Blocked by .claude/hooks/guard-bash.sh: $1 Ask the user to run it themselves if it is really needed." >&2
  exit 2
}

has() {
  printf '%s' "$CMD" | grep -Eiq -- "$1"
}

# rm with both recursive and force flags, in any spelling (-rf, -fr, -r -f, --recursive --force).
RM_SEGS=$(printf '%s\n' "$CMD" | grep -oE '(^|[;&|(`[:space:]])rm[[:space:]]+[^;&|`)]*' || true)
if [ -n "$RM_SEGS" ]; then
  if printf '%s' "$RM_SEGS" | grep -Eq '[[:space:]](-[a-zA-Z]*[rR]|--recursive)' \
    && printf '%s' "$RM_SEGS" | grep -Eq '[[:space:]](-[a-zA-Z]*[fF]|--force)'; then
    deny "'rm -rf' style deletion. Use 'rm <file>' or 'rm -r <dir>' on specific paths."
  fi
fi

# git
has 'git[[:space:]]+push[^;&|]*([[:space:]]--force([[:space:]]|$)|[[:space:]]-f([[:space:]]|$))' \
  && deny "force push. Use --force-with-lease only when the user asks."
has 'git[[:space:]]+reset[[:space:]]+[^;&|]*--hard' \
  && deny "'git reset --hard' discards work."
has 'git[[:space:]]+clean[[:space:]]+[^;&|]*-[a-zA-Z]*f' \
  && deny "'git clean -f' deletes untracked files."
has 'git[[:space:]]+(checkout|restore)[[:space:]]+(--[[:space:]]+)?\.([[:space:]]|$)' \
  && deny "discarding all working-tree changes."
has '(^|[[:space:]])--no-verify([[:space:]]|$)' \
  && deny "--no-verify skips the pre-commit checks."

# docker
has 'docker([[:space:]]+|-)compose[^;&|]*[[:space:]]down[^;&|]*([[:space:]]-v([[:space:]]|$)|--volumes)' \
  && deny "'docker compose down -v' deletes volumes (Redis, Postgres and Kafka data)."
has 'docker[[:space:]]+(volume[[:space:]]+(rm|prune)|system[[:space:]]+prune)' \
  && deny "removing Docker volumes or pruning the system."

# data stores
has '(^|[^[:alnum:]_])(flushall|flushdb)([^[:alnum:]_]|$)' \
  && deny "FLUSHALL/FLUSHDB wipes Redis."
has '(drop[[:space:]]+(database|table|schema)|truncate[[:space:]]+table)' \
  && deny "destructive SQL (DROP/TRUNCATE). Write a migration instead."

# supply chain and privilege
has '(curl|wget)[^|;&]*\|[[:space:]]*(sudo[[:space:]]+)?(ba|z)?sh([[:space:]]|$)' \
  && deny "piping a download into a shell."
has '(^|[;&|(`[:space:]])sudo[[:space:]]' \
  && deny "sudo."

# reading secrets through the shell
has '(cat|less|more|head|tail|bat|type|xxd|strings)[[:space:]]+[^;&|]*\.env([[:space:]]|$)' \
  && deny "reading a .env file. Use .env.example for variable names."

exit 0
