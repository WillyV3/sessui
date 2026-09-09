# sessui

A tmux session switcher that opens in a popup, shows what each session is
actually doing, and gets out of the way.

```
── 4 sessions ──────────────────────────── willy@omarchy ──────────────────────────── S E S S U I ──


    apps    session           ⧗     cwd             status
  >       api-gateway         4s   ~/p/sessui
           dashboard           4s   ~/p/sessui
           docs-site           4s   ~/p/sessui
           worker-queue        4s   ~/p/sessui
```

Sessions are ordered most-recently-used, so the one you were just in is at the
top. Each row carries the apps running in it, how long since it was last
active, its working directory, and — when an AI coding agent is running there —
whether it is working, idle, or waiting on you.

## Install

**With [TPM](https://github.com/tmux-plugins/tpm)** — add to `~/.config/tmux/tmux.conf`:

```tmux
set -g @plugin 'WillyV3/sessui'
```

Then `prefix + I`. The plugin builds the binary on first load if Go is present,
otherwise install one of the two ways below first.

**With Go:**

```sh
go install github.com/WillyV3/sessui@latest
```

**Without Go** — download a release binary from the
[releases page](https://github.com/WillyV3/sessui/releases/latest) and put it on
your PATH:

```sh
tar -xzf sessui_linux_amd64.tar.gz -C ~/.local/bin sessui
```

Archives are published for `linux_amd64`, `linux_arm64`, `darwin_amd64` and
`darwin_arm64`.

Bind it yourself if you are not using TPM:

```tmux
bind-key s display-popup -E -w 112 -h 26 sessui
```

## Keys

| Key           | Action |
|---------------|--------|
| `↑` / `↓`     | move |
| type any text | filter — no `/` needed |
| `enter`       | switch to the highlighted session, or create one named by the filter |
| `ctrl+r`      | rename the highlighted session |
| `ctrl+x`      | kill the highlighted session |
| `ctrl+e`      | settings: columns, popup width, theme, icons |
| `esc`         | close |

Letters feed the filter, so there are no `j`/`k` nav keys — the arrows are the nav.

## Configure

Popup geometry lives in tmux options, because tmux needs them before the binary
runs:

| Option           | Default | |
|------------------|---------|--|
| `@sessui-key`    | `s`     | `prefix + <key>` opens the switcher |
| `@sessui-width`  | `112`   | popup width — the table wants ~112 columns |
| `@sessui-height` | `26`    | popup height |

Everything else is `~/.config/sessui/config.json`, and `ctrl+e` writes it for you:

| Key             | Values | Default | |
|-----------------|--------|---------|--|
| `icons`         | `"nerd"` \| `"ascii"` | `nerd` | `ascii` swaps every Nerd Font glyph for a plain stand-in — use it on a terminal without a Nerd Font |
| `theme.palette` | `"auto"` \| `"dark"` \| `"light"` | `auto` | `auto` follows the [Omarchy](https://omarchy.org) theme when present, else the built-in dark palette |
| `columns`       | array of `{"id", "width"}` | six shipped columns | which columns show, in what order |

## Several machines, one list

`ctrl+e` has a **hosts** row listing the machines in your `~/.ssh/config`. Arrow
across, `space` to watch one, and its tmux sessions join the list:

```
session          host        ago   cwd
local-work                   6s    ~/projects/api
0                inspiron    10h   ~
oc               inspiron    10h   ~
```

Chips show reachability as answers arrive — `●` up, `○` down, `◌` still asking —
and reachable machines float to the front. Nothing is polled until you pick a
host, and a machine that is asleep stays listed rather than disappearing.

`enter` on a remote session attaches to it over ssh, wrapped in a local tmux
session named `host/name`. After that it is an ordinary local session, so every
later switch is instant and it stops appearing twice.

Requires only that `ssh -o BatchMode=yes <host> true` works — key auth, no
prompt. sessui shells out to `ssh`, so `~/.ssh/config` governs everything:
ProxyJump, certificates, agent forwarding, Tailscale names. Nothing is
installed on the remote; it just needs tmux.

## Pairs well with cp3

If you run [claude-peers](https://github.com/WillyV3/claude-peers) (`cp3`),
sessui joins each session to its peer by working directory and adds a `peer`
column: whether the agent is up, what it is working on, and a `✉` when it owes
you a reply. Nothing to configure — it reads `cp3 peers` if `cp3` is on the PATH.

Without `cp3`, the peer column disappears and everything else works the same.

## Requirements

- **tmux 3.7+** — earlier versions open the popup but will not redraw it live.
- A **Nerd Font**, or set `"icons": "ascii"`.
- Go 1.27+ only if you are building from source.

## Docs

- [Architecture](docs/ARCHITECTURE.md) — how the data and UI halves split.
- [Gotchas](docs/GOTCHAS.md) — the things that cost real time.

## License

MIT
