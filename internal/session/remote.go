package session

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// hostTimeout bounds one host's fetch. A sleeping laptop drops the SYN rather
// than refusing it, so without this the fetch hangs until the kernel gives up.
const hostTimeout = 6 * time.Second

// errNoTmux is a reachable machine with no tmux, which is not an empty one.
var errNoTmux = errors.New("tmux not found on host")

// remoteMarker separates the two tmux outputs of one ssh round trip. Every
// line on either side is a pipe-delimited record; this contains no pipe.
const remoteMarker = "@@sessui@@"

// noTmuxMarker is printed when the far end has no tmux, so that case reads as
// the error it is instead of an empty server.
const noTmuxMarker = "@@sessui-no-tmux@@"

// remotePrelude puts Homebrew on PATH before running anything. A
// non-interactive ssh command sources no profile, and on macOS that leaves
// tmux unreachable: a Mac with sessions open reports none. Prepending beats
// `bash -lc` -- no shell startup, no dependence on how the profile is written.
const remotePrelude = `export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"; ` +
	`command -v tmux >/dev/null 2>&1 || { echo ` + noTmuxMarker + `; exit 0; }; `

// RemoteHost is one watched machine and the last thing seen on it. Err and
// Seen let a host that is asleep keep its row and say when it last answered.
type RemoteHost struct {
	Alias    string
	Sessions []Session
	Err      error
	Seen     time.Time
}

// Watcher caches what each host last reported. The zero value is ready to use.
//
// Snapshot is a map read and never touches the network, so a render can never
// block on a sleeping laptop. Refresh does the I/O and belongs in a goroutine.
type Watcher struct {
	mu    sync.RWMutex
	hosts map[string]RemoteHost
}

// Snapshot returns what is known, in the order given. A host never polled
// comes back zero-valued rather than missing, so the UI can show it as pending.
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

// RefreshOne fetches a single host and updates the cache. It blocks for up to
// hostTimeout, so call it from a goroutine, never a render path. One host per
// call lets a caller report each answer as it lands instead of waiting on the
// slowest machine.
func (w *Watcher) RefreshOne(ctx context.Context, alias string) {
	w.store(fetchHost(ctx, alias))
}

// Refresh fetches every host concurrently and returns when all have answered.
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
	// A failed poll drops the host's sessions: the list is what you can switch
	// to, and a machine that just refused to answer is not that. Seen survives
	// so the UI can still say when it last answered.
	if h.Err != nil {
		if prev, ok := w.hosts[h.Alias]; ok {
			h.Seen = prev.Seen
		}
		h.Sessions = nil
	}
	w.hosts[h.Alias] = h
}

// fetchHost runs both tmux queries in one ssh round trip; the marker splits
// them again here.
func fetchHost(ctx context.Context, alias string) RemoteHost {
	ctx, cancel := context.WithTimeout(ctx, hostTimeout)
	defer cancel()

	// The trailing `exit 0` is load-bearing: tmux exits 1 when no server is
	// running, which would report a healthy machine with no sessions as
	// unreachable. Reachability is what ssh says about the connection.
	remote := remotePrelude +
		"tmux list-sessions -F '" + sessionFormat + "' 2>/dev/null; " +
		"echo " + remoteMarker + "; " +
		"tmux list-panes -a -F '" + paneFormat + "' 2>/dev/null; exit 0"

	out, err := exec.CommandContext(ctx, "ssh", append(sshArgs(alias), remote)...).Output()
	if err != nil {
		return RemoteHost{Alias: alias, Err: err}
	}

	if strings.Contains(string(out), noTmuxMarker) {
		return RemoteHost{Alias: alias, Err: errNoTmux}
	}

	sessOut, paneOut, _ := strings.Cut(string(out), remoteMarker)
	// No peer roster and no current session: cp3 is local, and we are not
	// attached over there.
	sessions, _ := Build(sessOut, paneOut, peerFleet{}, "")
	for i := range sessions {
		sessions[i].Host = alias
	}
	return RemoteHost{Alias: alias, Sessions: sessions, Seen: time.Now()}
}

// sshArgs is the transport decision in one place: shell out to ssh rather
// than dial with a Go client, so ~/.ssh/config governs everything --
// ProxyJump, certificates, agent forwarding, Match, Include.
//
// ControlMaster is what makes a fan-out affordable: a cold connection costs
// ~385ms, a multiplexed one ~40ms.
func sshArgs(alias string) []string {
	return []string{
		"-o", "BatchMode=yes", // never prompt; a prompt in a popup is a hang
		"-o", "ConnectTimeout=3",
		// Without keepalives ssh waits on a dead TCP connection forever, and a
		// proxy session for a laptop that went to sleep stays in the list with
		// a live ssh behind it. 15s x 3 gives up after ~45s and the pane closes.
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=~/.ssh/sessui-%r@%h:%p",
		"-o", "ControlPersist=60s",
		alias,
	}
}

// Merge appends the watcher's cached remote sessions to a local list: local
// first, then remote grouped by host. Not one MRU sort across the whole list
// -- that would compare timestamps from different clocks and shuffle rows on
// skew. Reads the cache only, so it is safe on a render path.
func Merge(local []Session, w *Watcher, aliases []string) []Session {
	// A remote session already attached through a local proxy is one session,
	// not two rows; the remote row is the stale copy.
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

// ProxySep joins a host and a session name into the local proxy's name.
//
// Not ":": tmux accepts a session named "host:name" and then cannot target
// it, because ":" is its session:window separator.
const ProxySep = "/"

// ProxyName is the local session that holds an ssh attachment to a remote one.
func ProxyName(host, name string) string { return host + ProxySep + name }

// AttachRemote switches to a remote session by proxying it through a local one.
//
// tmux cannot switch a client across machines, so the attachment is wrapped
// in a local session. After that the row is an ordinary local session: every
// later switch is an instant switch-client with no special case in the UI.
// Idempotent: a second enter reuses the existing proxy.
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

// quote wraps a value for the shell tmux hands the command to.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// NewRemote creates a session on host and attaches to it. The session is
// created detached first, so it survives if the attach fails or the user
// backs out: a session that exists is recoverable.
func NewRemote(host, name string) error {
	// `new-session` fails when the name is taken, and "already there" is no
	// reason to refuse to take the user to it.
	remote := remotePrelude + "tmux has-session -t " + quote(name) + " 2>/dev/null || tmux new-session -d -s " + quote(name)
	if err := exec.Command("ssh", append(sshArgs(host), remote)...).Run(); err != nil {
		return err
	}
	return AttachRemote(host, name)
}

// runOn executes a tmux command on host, or locally when host is "". Every
// action a row offers goes through here: it is the one place that decides
// which machine a row is on.
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

// KillOn kills a session on host ("" = local). A killed remote session's
// local proxy needs no cleanup: its ssh exits, the pane closes, and tmux
// drops the session with its last window.
func KillOn(host, name string) error {
	return runOn(host, "kill-session", "-t", name)
}

// RenameOn renames a session on host ("" = local).
func RenameOn(host, oldName, newName string) error {
	return runOn(host, "rename-session", "-t", oldName, newName)
}

// Forget drops one session from a host's cached list. The next poll is up to
// 15s away; an action we performed ourselves is the one case where the cache
// can be corrected without asking the network.
func (w *Watcher) Forget(alias, name string) {
	w.editSessions(alias, func(sessions []Session) []Session {
		kept := sessions[:0:0] // fresh backing array: Merge hands these out
		for _, s := range sessions {
			if s.Name != name {
				kept = append(kept, s)
			}
		}
		return kept
	})
}

// editSessions replaces one host's cached session list under the lock.
// RemoteHost is a value in the map, so the write-back is what makes an edit
// real; taking a func means a caller cannot forget it.
func (w *Watcher) editSessions(alias string, edit func([]Session) []Session) {
	w.mu.Lock()
	defer w.mu.Unlock()
	h, ok := w.hosts[alias]
	if !ok {
		return
	}
	h.Sessions = edit(h.Sessions)
	w.hosts[alias] = h
}

// RenameCached renames a session in a host's cached list, for the same reason
// Forget exists.
func (w *Watcher) RenameCached(alias, oldName, newName string) {
	w.editSessions(alias, func(sessions []Session) []Session {
		renamed := make([]Session, len(sessions))
		copy(renamed, sessions)
		for i := range renamed {
			if renamed[i].Name == oldName {
				renamed[i].Name = newName
			}
		}
		return renamed
	})
}
