#!/usr/bin/env bash
# sessui.tmux -- TPM entry point.
#
# Builds the Go binary on install/update (needs Go on PATH) and binds the
# session-switcher popup. Configure in tmux.conf, before `run '~/.tmux/plugins/tpm/tpm'`:
#
#   set -g @sessui-key    's'     # prefix + <key> opens it   (default: s)
#   set -g @sessui-width  '112'   # popup width               (default: 112 -- the table needs ~112)
#   set -g @sessui-height '26'    # popup height              (default: 26)
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/sessui"

# tmux 3.8+: the popup only redraws live (spinners, counters, marquee) from
# 3.8. Say so once at install rather than let an older tmux look broken.
# awk, not sort -V: BSD sort on macOS is not guaranteed to have -V.
tmux_version="$(tmux -V | sed 's/[^0-9.]//g')"
if ! printf '%s\n' "$tmux_version" | awk -F. '{ exit !($1 > 3 || ($1 == 3 && $2 >= 8)) }'; then
	tmux display-message "sessui: needs tmux 3.8+ (this is $tmux_version) — the popup will not redraw live"
fi

# Build on first install and whenever a source file is newer than the binary
# (covers `prefix + U` in-place updates; a fresh TPM clone has no binary at all).
if command -v go >/dev/null 2>&1; then
	if [ ! -x "$BIN" ] || [ -n "$(find "$DIR" -name '*.go' -newer "$BIN" -print -quit 2>/dev/null)" ]; then
		(cd "$DIR" && go build -o "$BIN" .) || tmux display-message "sessui: go build failed"
	fi
elif [ ! -x "$BIN" ]; then
	tmux display-message "sessui: install Go (https://go.dev), then 'prefix + U' to build"
fi

opt() {
	local v
	v="$(tmux show-option -gqv "$1")"
	[ -n "$v" ] && echo "$v" || echo "$2"
}
key="$(opt @sessui-key s)"
height="$(opt @sessui-height 26)"

# Width is read at OPEN time, not bind time, so a change made from sessui's
# own settings (which writes @sessui-width) takes effect on the very next
# prefix+key without re-sourcing tmux.conf. display-popup -w rejects a
# format string, hence the run-shell substitution rather than '#{@sessui-width}'.
# Height stays bound once: the table is one row per session and there is no
# setting for it, by design.
tmux bind-key "$key" run-shell "tmux display-popup -E -w \"\$(tmux show-option -gqv @sessui-width)\" -h $height '$BIN' 2>/dev/null || tmux display-popup -E -w 112 -h $height '$BIN'"
