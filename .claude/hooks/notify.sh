#!/usr/bin/env bash
# Notification (permission_prompt|idle_prompt): desktop notification when Claude needs you.
# Always exits 0. Background processes have their output redirected, as the hooks docs require.
set -u

INPUT=$(cat)
MSG="Claude Code needs your attention"
if command -v jq >/dev/null 2>&1; then
  MSG=$(printf '%s' "$INPUT" | jq -r '.message // "Claude Code needs your attention"')
fi

# Keep quotes out of the message so it cannot break the notification command.
SAFE=${MSG//\"/}
SAFE=${SAFE//\'/}
SAFE=${SAFE//\`/}

notify_windows() {
  powershell.exe -NoProfile -Command \
    "Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show('$SAFE','Claude Code') | Out-Null" \
    >/dev/null 2>&1 &
}

case "$(uname -s 2>/dev/null)" in
  Darwin)
    osascript -e "display notification \"$SAFE\" with title \"Claude Code\"" >/dev/null 2>&1
    ;;
  Linux)
    if command -v notify-send >/dev/null 2>&1; then
      notify-send "Claude Code" "$SAFE" >/dev/null 2>&1
    elif grep -qi microsoft /proc/version 2>/dev/null && command -v powershell.exe >/dev/null 2>&1; then
      notify_windows
    fi
    ;;
  MINGW*|MSYS*|CYGWIN*)
    notify_windows
    ;;
esac

exit 0
