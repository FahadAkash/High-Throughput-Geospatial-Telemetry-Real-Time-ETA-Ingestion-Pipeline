#!/usr/bin/env bash
# SessionStart (startup|resume|clear|compact): plain stdout is added to Claude's context.
# Keep it short and factual. Static rules belong in AGENTS.md, not here.
set -u

cat >/dev/null 2>&1 || true

cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0

echo "Project: Go real-time ride-hailing POC (driver telemetry, H3 matching, ETA). Rules are in AGENTS.md."

if command -v go >/dev/null 2>&1; then
  echo "Go toolchain: $(go version)"
else
  echo "Go toolchain: not found on PATH."
fi

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Git branch: $(git rev-parse --abbrev-ref HEAD 2>/dev/null)"
  echo "Uncommitted Go files: $(git status --porcelain -- '*.go' 2>/dev/null | wc -l | tr -d ' ')"
  RECENT=$(git log --oneline -5 2>/dev/null)
  if [ -n "$RECENT" ]; then
    echo "Recent commits:"
    printf '%s\n' "$RECENT"
  fi
fi

MISSING=""
for tool in jq gofmt goimports gofumpt golangci-lint docker; do
  command -v "$tool" >/dev/null 2>&1 || MISSING="$MISSING $tool"
done
if [ -n "$MISSING" ]; then
  echo "Tools not found on PATH:$MISSING. Hooks skip the missing formatters and linters. Without jq the protection hooks block every edit and Bash call."
fi

cat <<'EOF'
Project invariants: pings never write to Postgres. Redis is written before Kafka on the hot path. Candidate search reads the rider's H3 cell plus its k-ring neighbors. Driver claims are atomic in Redis. Benchmark numbers come from bench/results, never from estimates.
EOF

exit 0
