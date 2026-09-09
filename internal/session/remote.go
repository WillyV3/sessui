package session

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// hostTimeout bounds one host's fetch. A laptop that is asleep does not refuse
// the connection, it silently drops the SYN, so without this the fetch hangs
// until the kernel gives up minutes later.
const hostTimeout = 6 * time.Second

// errNoTmux is a reachable machine with no tmux, which the UI should not
// present as an empty one.
var errNoTmux = errors.New("tmux not found on host")

// remoteMarker separates the two tmux outputs in a single ssh round trip. It
// cannot collide with tmux output: every line on either side of it is a
// pipe-delimited record, and this contains no pipe.
const remoteMarker = "@@sessui@@"

// noTmuxMarker is printed when the far end cannot find tmux at all, so that
// case is reported as the error it is instead of looking like an empty server.
const noTmuxMarker = "@@sessui-no-tmux@@"

// remotePrelude puts Homebrew on PATH before running anything.
//
// A NON-INTERACTIVE ssh command gets a minimal PATH -- no profile is sourced --
// and on macOS that means Homebrew is missing. Measured on a real Mac:
//
//	PATH=/Users/x/.cargo/bin:/usr/bin:/bin:/usr/sbin:/sbin
//	command -v tmux  ->  not found
//	/opt/homebrew/bin/tmux list-sessions  ->  8 sessions
//
// So a Mac with eight sessions open reported zero, and looked exactly like a
// machine with nothing running. Prepending is deliberate over `bash -lc`: it
// costs no shell startup and does not depend on how the user's profile is
// written. Both Homebrew prefixes are covered -- /opt/homebrew on Apple
// Silicon, /usr/local on Intel.
const remotePrelude = `export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"; ` +
	`command -v tmux >/dev/null 2>&1 || { echo ` + noTmuxMarker + `; exit 0; }; `

// RemoteHost is one watched machine and the last thing we saw on it.
//
// Err and Seen are the honest half: a host that is asleep still occupies a row
// and says when it last answered, rather than vanishing or blocking the popup.
type RemoteHost struct {
	Alias    string
	Sessions []Session
	Err      error
	Seen     time.Time
}

// Watcher caches what each host last reported. The zero value is ready to use.
//
// The split is the whole point: Snapshot is a map read and never touches the
// network, so rendering a frame cannot block on a sleeping laptop. Refresh does
// the I/O and is meant to be called from a goroutine.
type Watcher struct {
	mu    sync.RWMutex
	hosts map[string]RemoteHost
}

// Snapshot returns what is currently known, in the order given. Hosts never
// polled yet come back zero-valued rather than missing, so the UI can show them
// as pending instead of pretending they do not exist.
func (w *Watcher) Snapshot(aliases []string) []RemoteHost {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]RemoteHost, 0, len(aliases))
	for _, a := range aliases {
		h, ok := w.hosts[a]
		if !ok {
			h = RemoteHost{Alias: a}
		}
		out = append(out, h)
	}
	return out
}

// RefreshOne fetches a single host and updates the cache. It blocks for as long
// as that host takes (bounded by hostTimeout), so call it from a goroutine --
// never from a render path.
//
// One host at a time is the useful unit: the caller runs these concurrently and
// can report each answer as it lands, instead of holding every result hostage to
// the slowest machine in the fleet.
func (w *Watcher) RefreshOne(ctx context.Context, alias string) {
	w.store(fetchHost(ctx, alias))
}

// Refresh fetches every host concurrently and returns when all have answered.
// Prefer RefreshOne per host where the caller can show partial results.
func (w *Watcher) Refresh(ctx context.Context, aliases []string) {
	var wg sync.WaitGroup
	for _, alias := range aliases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.RefreshOne(ctx, alias)
		}()
	}
	wg.Wait()
}

func (w *Watcher) store(h RemoteHost) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hosts == nil {
		w.hosts = make(map[string]RemoteHost)
	}
	// A failed poll DROPS the host's sessions. The list is what you can switch
	// to, and a session on a machine that just refused to answer is not that --
	// pressing enter would ssh to something unreachable.
	//
	// This used to keep them, on the theory that a host going quiet for one
	// cycle should dim its rows rather than lose them. The dimming was never
	// built, so they simply rendered as ordinary switchable rows: the failure
	// was invisible and the offer was a lie. Erasing is the honest version, and
	// a "blip" here means a host did not answer within hostTimeout, which is
	// not a blip.
	//
	// Seen is preserved so the editor can still say when the machine last
	// answered -- knowing it is gone is different from forgetting it existed.
	if h.Err != nil {
		if prev, ok := w.hosts[h.Alias]; ok {
			h.Seen = prev.Seen
		}
		h.Sessions = nil
	}
	w.hosts[h.Alias] = h
}

// fetchHost runs both tmux queries in ONE ssh round trip. Two calls would
// double the handshake cost for no benefit; the marker splits them again here.
func fetchHost(ctx context.Context, alias string) RemoteHost {
	ctx, cancel := context.WithTimeout(ctx, hostTimeout)
	defer cancel()

	// The trailing `exit 0` is load-bearing. tmux exits 1 when no server is
	// running, so without it a perfectly healthy machine that simply has no
	// sessions open comes back as an ssh failure and gets reported UNREACHABLE
	// -- measured on a live host: ubuntu-homelab answers in 1.3s and was shown
	// as down purely because nobody had started tmux on it.
	//
	// Reachability is now what ssh says about the CONNECTION, and an empty
	// session list is allowed to mean an empty session list. The trade: a host
	// without tmux installed also reads as zero sessions rather than an error,
	// which is a fair description of how many tmux sessions it has.
	remote := remotePrelude +
		"tmux list-sessions -F '" + sessionFormat + "' 2>/dev/null; " +
		"echo " + remoteMarker + "; " +
		"tmux list-panes -a -F '" + paneFormat + "' 2>/dev/null; exit 0"

	out, err := exec.CommandContext(ctx, "ssh", append(sshArgs(alias), remote)...).Output()
	if err != nil {
		return RemoteHost{Alias: alias, Err: err}
	}

	// "tmux is not installed" is a different fact from "no sessions", and only
	// the first is worth telling the user about.
	if strings.Contains(string(out), noTmuxMarker) {
		return RemoteHost{Alias: alias, Err: errNoTmux}
	}

	sessOut, paneOut, _ := strings.Cut(string(out), remoteMarker)
	// No peers and no current session: cp3 is a local roster, and we are not
	// attached over there, so nothing is excluded.
	sessions, _ := Build(sessOut, paneOut, peerFleet{}, "")
	for i := range sessions {
		sessions[i].Host = alias
	}
	return RemoteHost{Alias: alias, Sessions: sessions, Seen: time.Now()}
}

// sshArgs is the transport decision in one place: shell out to ssh rather than
// dial with a Go client. The user's ~/.ssh/config then governs everything --
// ProxyJump, certificates, agent forwarding, Match blocks, Include, Tailscale
// names -- because this IS ssh. A Go client would reimplement that, partially.
//
// ControlMaster is the reason a fan-out is affordable at all: measured against
// a fleet host, a cold connection is 385ms and a multiplexed one is 36-64ms.
func sshArgs(alias string) []string {
	return []string{
		"-o", "BatchMode=yes", // never prompt; a prompt in a popup is a hang
		"-o", "ConnectTimeout=3",
		// Notice a machine that went away. Without these, ssh waits on a dead
		// TCP connection forever: a laptop that sleeps or drops wifi leaves the
		// proxy session sitting in the list with a live `ssh` process behind it,
		// looking like a session you can still switch to. 15s x 3 gives up after
		// ~45s, the pane closes, and tmux drops the session with its last window.
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=~/.ssh/sessui-%r@%h:%p",
		"-o", "ControlPersist=60s",
		alias,
	}
}

// Merge appends the watcher's cached remote sessions to a local list.
//
// Local first, then remote grouped by host, each already most-recently-used
// within its group. Deliberately NOT one MRU sort across the whole list: that
// would compare timestamps from different machines' clocks, and a few seconds
// of skew would shuffle rows for no reason the user could see.
//
// Reads the cache only, so this is safe on a render path.
func Merge(local []Session, w *Watcher, aliases []string) []Session {
	// A remote session already attached through a local proxy is ONE session,
	// not two rows saying the same thing: once "oc" on inspiron is held by the
	// local session "inspiron/oc", the remote row for it is the stale copy.
	attached := make(map[string]bool, len(local))
	for _, s := range local {
		attached[s.Name] = true
	}
	out := local
	for _, h := range w.Snapshot(aliases) {
		for _, s := range h.Sessions {
			if attached[ProxyName(h.Alias, s.Name)] {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

// ProxySep joins a host and a session name into the local proxy session's name.
//
// NOT ":". tmux accepts a session named "inspiron:oc" and then cannot target
// it -- ":" is its session:window separator, so `has-session -t inspiron:oc`
// answers "can't find window: oc". A proxy you can create but never switch to
// is worse than no proxy. "/" is unambiguous and sorts the same way.
const ProxySep = "/"

// ProxyName is the local session that holds an ssh attachment to a remote one.
func ProxyName(host, name string) string { return host + ProxySep + name }

// AttachRemote switches to a remote session by proxying it through a LOCAL one.
//
// tmux cannot switch a client across machines, so "switch to a remote session"
// is really "attach over ssh". Wrapping that attachment in a local session is
// what makes it behave like everything else afterwards: the row becomes an
// ordinary local session, and every later switch is an instant switch-client
// with no remote round trip and no special case anywhere in the UI.
//
// Idempotent -- a second enter on the same row re-uses the existing proxy
// rather than stacking another ssh on top of it.
func AttachRemote(host, name string) error {
	proxy := ProxyName(host, name)
	if err := exec.Command("tmux", "has-session", "-t", proxy).Run(); err != nil {
		remote := remotePrelude + "tmux attach -t " + quote(name)
		cmd := "ssh " + strings.Join(sshArgs(host), " ") + " -t " + quote(remote)
		if err := exec.Command("tmux", "new-session", "-d", "-s", proxy, cmd).Run(); err != nil {
			return err
		}
	}
	return Switch(proxy)
}

// quote wraps a value for the shell tmux hands the command to. Session names
// come from tmux itself and can contain spaces.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// NewRemote creates a session on a remote host and attaches to it, so creating
// somewhere else lands you there the same way creating locally does.
//
// Two steps rather than one `ssh -t tmux new-session`: the session is made
// DETACHED first, so it survives if the attach that follows fails or the user
// backs out. A session that exists is recoverable; one that was never created
// is a silent no-op.
func NewRemote(host, name string) error {
	// `tmux new-session` fails if the name is taken, and "it is already there"
	// is not a reason to refuse to take the user to it -- the same reasoning
	// that makes AttachRemote reuse an existing proxy.
	remote := remotePrelude + "tmux has-session -t " + quote(name) + " 2>/dev/null || tmux new-session -d -s " + quote(name)
	if err := exec.Command("ssh", append(sshArgs(host), remote)...).Run(); err != nil {
		return err
	}
	return AttachRemote(host, name)
}

// runOn executes a tmux command on host, or locally when host is "".
//
// Every action a row offers has to know which machine the row is on. Kill and
// Rename shipped local-only while the list already showed remote sessions, so
// killing a session that lived on another box ran `tmux kill-session` here,
// found nothing by that name, and failed with exit status 1.
func runOn(host string, args ...string) error {
	if host == "" {
		return exec.Command("tmux", args...).Run()
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quote(a)
	}
	return exec.Command("ssh", append(sshArgs(host), remotePrelude+"tmux "+strings.Join(quoted, " "))...).Run()
}

// KillOn kills a session on host ("" = local).
//
// The local proxy for a killed remote session needs no cleanup: its ssh exits
// when the remote session goes, the pane closes, and tmux drops the session
// with its last window.
func KillOn(host, name string) error {
	return runOn(host, "kill-session", "-t", name)
}

// RenameOn renames a session on host ("" = local).
func RenameOn(host, oldName, newName string) error {
	return runOn(host, "rename-session", "-t", oldName, newName)
}

// Forget drops one session from a host's cached list.
//
// Killing a remote session succeeds instantly, but the row survived until the
// next 15s poll: Merge reads the cache, and nothing told the cache what we had
// just done. The session was gone from the host and still on screen -- which
// reads as the kill having failed. An action we performed ourselves is the one
// case where the cache can be corrected without asking the network.
func (w *Watcher) Forget(alias, name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	h, ok := w.hosts[alias]
	if !ok {
		return
	}
	kept := h.Sessions[:0:0] // fresh backing array: Merge hands these out
	for _, s := range h.Sessions {
		if s.Name != name {
			kept = append(kept, s)
		}
	}
	h.Sessions = kept
	w.hosts[alias] = h
}

// RenameCached renames a session in a host's cached list, for the same reason:
// otherwise the row keeps its old name until the next poll.
func (w *Watcher) RenameCached(alias, oldName, newName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	h, ok := w.hosts[alias]
	if !ok {
		return
	}
	sessions := make([]Session, len(h.Sessions))
	copy(sessions, h.Sessions)
	for i := range sessions {
		if sessions[i].Name == oldName {
			sessions[i].Name = newName
		}
	}
	h.Sessions = sessions
	w.hosts[alias] = h
}
