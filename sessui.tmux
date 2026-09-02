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
width="$(opt @sessui-width 112)"
height="$(opt @sessui-height 26)"

tmux bind-key "$key" display-popup -E -w "$width" -h "$height" "$BIN"
