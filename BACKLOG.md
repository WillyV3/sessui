# Backlog

One line each, ≤200 chars. Cleared continuously: when an item ships it moves
to DECISIONS.md with the choice that was made and why. Nothing lives here
forever — an item that stops mattering is deleted, not parked.

- Header widget system: `widgetCatalog` + `header.left/right` config, defaults byte-identical to today. Widgets: sessions, attention, now-playing, host, shell.
- now-playing widget: follow Omarchy 4's actual media stack (investigating — manual + installed system, not memory) and the Mac's native equivalent. Controls via one modifier key + footer overlay.
- cliamp visibility to a system-wide now-playing widget: needs MPRIS in cliamp, or a special case. Willy decides.
- Settings form for theme roles / palette / icons (data layer + tests shipped; picker UI not built).
- `v0.1.0` tag — release workflow is unproven until a tag fires it. Willy's word.
- `sessui.tmux`: fall back to a prebuilt release binary when Go is absent or older than go.mod (macbook1: Go 1.25.5 vs 1.27). Willy's word.
- Column editor: react to `tea.WindowSizeMsg` while open (currently fixed at open-time width).
