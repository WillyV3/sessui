#!/usr/bin/env bash
# sessui.tmux -- TPM entry point.
#
# Fetches the release binary for this checkout (no Go toolchain needed) and
# binds the session-switcher popup. Configure in tmux.conf, before `run '~/.tmux/plugins/tpm/tpm'`:
#
#   set -g @sessui-key    's'     # prefix + <key> opens it   (default: s)
#   set -g @sessui-width  '112'   # popup width               (default: 112 -- the table needs ~112)
#   set -g @sessui-height '26'    # popup height              (default: 26)
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/sessui"
REPO="https://github.com/WillyV3/sessui"

# tmux 3.7+: the popup only redraws live (spinners, counters, marquee) from
# 3.7 (issue 4920, in the 3.6b→3.7 changelog). Say so once at install.
# awk, not sort -V: BSD sort on macOS is not guaranteed to have -V.
tmux_version="$(tmux -V | sed 's/[^0-9.]//g')"
if ! printf '%s\n' "$tmux_version" | awk -F. '{ exit !($1 > 3 || ($1 == 3 && $2 >= 7)) }'; then
	tmux display-message "sessui: needs tmux 3.7+ (this is $tmux_version) — the popup will not redraw live"
fi

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}

# fetch <tag>: put that release's binary for this machine at $BIN. An archive
# whose sha256 is not the one the release published is never unpacked.
fetch() {
	local arch name tmp ok=1
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) return 1 ;;
	esac
	name="sessui_$(uname -s | tr '[:upper:]' '[:lower:]')_$arch.tar.gz"
	tmp="$(mktemp -d)"
	# One && chain: errexit is off inside a tested subshell, so each step must
	# gate the next by hand or a failed checksum would still reach tar.
	(
		cd "$tmp" &&
			curl -fsSL -O "$REPO/releases/download/$1/$name" -O "$REPO/releases/download/$1/checksums.txt" &&
			grep " $name\$" checksums.txt >sum &&
			sha256 -c sum &&
			tar -xzf "$name" sessui
	) >/dev/null 2>&1 && mv "$tmp/sessui" "$BIN" && ok=0
	rm -rf "$tmp"
	return $ok
}

# The binary is the release this checkout is at, or the last one before it.
# Fetch it when the one on disk is another version: a fresh install, or after
# `prefix + U` pulled a newer tag. A failed update keeps the binary it had.
want="$(git -C "$DIR" describe --tags --abbrev=0 2>/dev/null || true)"
have="$("$BIN" -version 2>/dev/null || true)"
if [ -n "$want" ] && [ "$have" != "$want" ]; then
	fetch "$want" || true
fi
if [ ! -x "$BIN" ]; then
	# Bind nothing: a key that opens a popup onto a missing binary is worse
	# than a key that keeps doing what it did before.
	tmux display-message "sessui: could not install ${want:-a release} for $(uname -sm) (needs curl, tar and network) — $REPO/releases"
	exit 0
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
