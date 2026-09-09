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

Seen on ubuntu-homelab. Harmless in the normal path — sessui launches as a tmux
popup, so a server always exists — but it is the same conflation that had a
healthy host reporting UNREACHABLE (fixed in the remote path, not this one).
"No sessions" and "no server" are different from "broken", and only the last
deserves an error.

**A cold remote create can leave you behind.** Once, with every ssh connection
cold, `enter` on a new remote session created it on the host and made the local
proxy, but the switch did not land — the client stayed put. Warm, it is
reliable; I could not reproduce it deliberately. Wants a harness that forces the
cold path repeatedly rather than another guess.

**An unreachable host will not say why.** The chip shows `○` and stops there.
`RemoteHost.Err` is captured and then thrown away by the UI. When inspiron went
dark it took manual ssh to discover it was a captive-portal network with DNS but
no route — the error had that in it the whole time. Surfacing it (on the focused
chip, or in the footer) turns a dead dot into a diagnosis.

## Debt taken on purpose

**A host without tmux reads as "0 sessions".** The remote command ends `exit 0`
so a host with no *server* is not called unreachable — but that also swallows
"tmux is not installed here". Fair as a description of how many tmux sessions
the machine has; still two different facts wearing one face.

**`hostTimeout` is a hardcoded 6s.** I twice nearly changed it on a hunch and
twice found the real cause elsewhere. A genuinely distant host has no way to ask
for longer, and there is no evidence yet about what it should be — measure
before exposing it.

**The theme watcher is verified in a pane, not a popup.** Popups cannot be read
with `capture-pane`, so that half rests on the code path being identical rather
than on a measurement. If theme following ever misbehaves, this is the untested
seam.

**Proxy cleanup is reasoned, not tested.** Killing a remote session should end
its local proxy: the ssh exits, the pane closes, tmux drops the session with its
last window. That chain is argued in a comment and never actually watched.

## Worth considering

**Create on a machine you have not added yet.** Putting a session on another box
takes two steps today: watch it in `ctrl+e`, then create. The create bar only
offers watched hosts. Every machine discovery found could be offered there
instead — the cost is that picking one implies watching it.

**cp3 and the pane captures are still sequential.** `fetchPeers()` then
`classifyAgents()`, 87ms and the rest of a 99ms load. Concurrency would roughly
halve it — but `ListFast` already paints in 5ms, so nobody is waiting on this.
Recorded because it is real, not because it matters yet.

**`-hosts` is undocumented.** It exists, it is how the remote half was built and
verified, and the README does not mention it.
