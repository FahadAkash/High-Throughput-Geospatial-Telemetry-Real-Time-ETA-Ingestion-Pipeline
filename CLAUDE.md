@AGENTS.md

## Claude Code

- Hooks are configured in `.claude/settings.json` and the scripts live in `.claude/hooks/`. Run `/hooks` to confirm they loaded. They need `bash` and `jq` on PATH (on Windows, Git Bash).
- Go files are formatted automatically after every edit, so do not hand-format them.
- A turn that changes Go code ends with `go build`, `go vet` and `go test -short`. If that gate blocks, fix the reported errors and continue instead of stopping.
- `git commit` runs the same checks first. If it blocks, fix the cause. Do not retry with `--no-verify`.
- Use plan mode for changes under `internal/store/redis/lua/`, `internal/match/` (claim logic) and any change to the Redis key schema.
- Personal overrides go in `CLAUDE.local.md` (gitignored), not in this file.
