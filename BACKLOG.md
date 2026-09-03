# Backlog

One line each, ≤200 chars. Cleared continuously: when an item ships it moves
to DECISIONS.md with the choice that was made and why. Nothing lives here
forever — an item that stops mattering is deleted, not parked.

- now-playing on macOS: no omarchy-shell there; backend undecided until macbook1 is reachable to verify what it has. Widget hides itself when the shell IPC is absent.
- cliamp has no MPRIS, so Omarchy's own bar can't see it either. Add MPRIS to cliamp (fixes it everywhere) vs a sessui special case. Willy decides.
- Settings form for theme roles / palette / icons (data layer + tests shipped; picker UI not built).
- `v0.1.0` tag — release workflow is unproven until a tag fires it. Willy's word.
- `sessui.tmux`: fall back to a prebuilt release binary when Go is absent or older than go.mod (macbook1: Go 1.25.5 vs 1.27). Willy's word.
