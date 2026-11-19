# Backlog

Things that actually went wrong, or debt taken knowingly. Nothing speculative:
if there is no moment where it hurt, it does not belong here.

## Bugs seen

**A cold remote create can leave you behind.** Once, with every ssh connection
cold, `enter` on a new remote session created it on the host and made the local
proxy, but the switch did not land — the client stayed put. Warm, it is
reliable; it has not reproduced deliberately. Wants a harness that forces the
cold path repeatedly rather than another guess.

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

**What a program reports needs a tmux that is not released yet.** tmux main
has carried OSC 7501 since 2026-11-12; 3.8 and older drop it, and there the
bell and the screen are all there is. It was run against a build of main with
Claude Code and pi. Codex, crush and the others that send it were not run.

**A finished turn reads as idle.** The protocol's `done` means the result is
ready and unseen. tmux keeps it until the program's next report, not until you
look, so it cannot yet mean "unread" and is shown as nothing.

## Worth considering

**Create on a machine you have not added yet.** Putting a session on another box
takes two steps today: watch it in `ctrl+e`, then create. The create bar only
offers watched hosts. Every machine discovery found could be offered there
instead — the cost is that picking one implies watching it.

**cp3 and the pane captures are still sequential.** `fetchPeers()` then
`classifyAgents()`. Concurrency would roughly halve the full load — but
`ListFast` already paints first, so nobody is waiting on this.

**A proxy session on another machine is listed there as a session.** Watch
machine B from machine A while A has `B/work` open, and B's list shows A's
`B/work` as a remote row. It is a view, not a session; opening it is a round
trip to where you already are. Refused from a nested view, still offered from
B's own terminal.

**A key bound by hand does not carry `SESSUI_NESTED`.** Only the binding
`sessui.tmux` makes says whether the client is inside another tmux, so a
hand-written `display-popup` binding gets no refusal and can still nest.

**No single key from a remote session to the local list.** It is detach, then
`prefix s`. Making it one key means taking that key from every program on the
far machine; an opt-in `@sessui-home-key` set as the proxy session's `prefix`
does it in one line (tried with F12, it works).

**Connection multiplexing is enabled without asking.** `ControlMaster=auto` with
`ControlPersist=60s` is what makes the fan-out affordable, but it leaves an
authenticated socket alive for a minute. Mode 0600, owned by you, namespaced
`sessui-`, so on a single-user machine it is the trade `ControlPersist` always
makes. On a box where root is not you, it is a real widening that sessui makes
on the user's behalf. Documented in SECURITY.md; a config key to turn it off
would be better than a paragraph.
