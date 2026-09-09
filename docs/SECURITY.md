# Security

What sessui does over the network, and what it assumes about your machine.

## It is ssh, so your ssh config is the policy

sessui shells out to the `ssh` binary. It does not implement a client, parse
keys, or manage credentials. Everything about how a connection is made and
trusted — host key verification, `ProxyJump`, certificates, agent forwarding,
`Match` blocks, `Include` — comes from `~/.ssh/config` and is untouched here.

That is the security argument for shelling out rather than dialling with a Go
library: there is no second, weaker implementation of any of it.

**Nothing is weakened.** sessui sets no `StrictHostKeyChecking`, writes no
`known_hosts`, and disables no verification. A host you have not accepted will
fail to connect, exactly as it would from your shell.

## What it does add

```
-o BatchMode=yes        never prompt
-o ConnectTimeout=3
-o ControlMaster=auto
-o ControlPath=~/.ssh/sessui-%r@%h:%p
-o ControlPersist=60s
```

**`BatchMode=yes`** means key auth only. sessui will never sit at a password
prompt — a prompt inside a popup is an invisible hang. This makes the
requirement stricter, not looser: a host that only accepts passwords simply does
not work.

**Connection multiplexing is a posture change, and it is the one thing here
worth a decision.** `ControlMaster` keeps an authenticated connection alive for
60 seconds after last use, so the next query costs ~40ms instead of ~385ms. It
is also, for those 60 seconds, an authenticated channel that anyone able to open
that socket can use without authenticating again.

In practice that means you, and root on your own machine. The socket is mode
`0600`, owned by you, inside `~/.ssh` (`0700`), and namespaced `sessui-` so it
never shares a control path with your own connections. On a single-user machine
this is the same trade `ControlPersist` always makes and most people already opt
into. On a shared box where you do not trust root, it is a real widening, and
you should set `ControlMaster no` for those hosts in your ssh config — sessui
passes its options after yours are read, but a per-host `ControlPath none` will
keep it from persisting.

## Remote commands and session names

sessui sends tmux commands over ssh, and tmux session names appear in them.
Names come from tmux, which accepts nearly any string, so they cross a remote
shell and are quoted with the POSIX single-quote idiom (`'` closed, escaped,
reopened) rather than interpolated.

Verified against a live host rather than argued: killing a session named

```
x'; touch /tmp/SESSUI_INJECTION; echo '
```

returns "no such session" and creates no file on the far end.

## What it never does

- No daemon, on either end. The remote needs tmux and nothing else installed.
- No credentials read, written, stored or transmitted by sessui.
- No network access at all until you add a host in `ctrl+e`. A fresh install
  talks to the local tmux server and nothing else.
- No telemetry.

## Tailscale and friends

sessui has no notion of Tailscale, and that is deliberate. A tailnet machine is
an ordinary ssh host, so it works through the same path as a LAN box, a bastion
behind `ProxyJump`, or anything else `ssh` can reach. Whatever authentication
and encryption your transport provides is what you get; sessui neither requires
nor bypasses it.
