# Backlog

Things that actually went wrong, or debt taken knowingly. Nothing speculative:
if there is no moment where it hurt, it does not belong here.

## Bugs seen

**No tmux server is a hard error.** On a machine with tmux installed but nothing
running, `sessui` exits instead of showing an empty list:

```
$ sessui -dump
sessui: dump: tmux display-message -p #{client_session}: exit status 1
```

Harmless in the normal path — sessui launches as a tmux popup, so a server
always exists — but "no sessions" and "no server" are different from "broken",
and only the last deserves an error.

**A cold remote create can leave you behind.** Once, with every ssh connection
cold, `enter` on a new remote session created it on the host and made the local
proxy, but the switch did not land — the client stayed put. Warm, it is
reliable; it has not reproduced deliberately. Wants a harness that forces the
cold path repeatedly rather than another guess.

**An unreachable host will not say why.** The chip shows `○` and stops there.
`RemoteHost.Err` is captured and then thrown away by the UI. A captive-portal
network with DNS but no route took manual ssh to diagnose — the error had that
in it the whole time. Surfacing it (on the focused chip, or in the footer)
turns a dead dot into a diagnosis.

## Debt taken on purpose

**`hostTimeout` is a hardcoded 6s.** A genuinely distant host has no way to ask
for longer, and there is no evidence yet about what it should be — measure
before exposing it.

**The theme watcher is verified in a pane, not a popup.** Popups cannot be read
with `capture-pane`, so that half rests on the code path being identical rather
than on a measurement.

**Proxy cleanup is reasoned, not tested.** Killing a remote session should end
its local proxy: the ssh exits, the pane closes, tmux drops the session with its
last window. That chain is argued in a comment and never actually watched.

## Worth considering

**Create on a machine you have not added yet.** Putting a session on another box
takes two steps today: watch it in `ctrl+e`, then create. The create bar only
offers watched hosts. Every machine discovery found could be offered there
instead — the cost is that picking one implies watching it.

**cp3 and the pane captures are still sequential.** `fetchPeers()` then
`classifyAgents()`. Concurrency would roughly halve the full load — but
`ListFast` already paints first, so nobody is waiting on this.

**Connection multiplexing is enabled without asking.** `ControlMaster=auto` with
`ControlPersist=60s` is what makes the fan-out affordable, but it leaves an
authenticated socket alive for a minute. Mode 0600, owned by you, namespaced
`sessui-`, so on a single-user machine it is the trade `ControlPersist` always
makes. On a box where root is not you, it is a real widening that sessui makes
on the user's behalf. Documented in SECURITY.md; a config key to turn it off
would be better than a paragraph.

**`-hosts` is undocumented.** It exists, it is how the remote half was built and
verified, and the README does not mention it.
