package session

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// hostTimeout bounds one host's fetch. A laptop that is asleep does not refuse
// the connection, it silently drops the SYN, so without this the fetch hangs
// until the kernel gives up minutes later.
const hostTimeout = 6 * time.Second

// remoteMarker separates the two tmux outputs in a single ssh round trip. It
// cannot collide with tmux output: every line on either side of it is a
// pipe-delimited record, and this contains no pipe.
const remoteMarker = "@@sessui@@"

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

// Refresh fetches every host concurrently and updates the cache. It blocks for
// as long as the slowest host takes (bounded by hostTimeout), so call it from a
// goroutine -- never from a render path.
func (w *Watcher) Refresh(ctx context.Context, aliases []string) {
	var wg sync.WaitGroup
	for _, alias := range aliases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.store(fetchHost(ctx, alias))
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
	// A failed poll keeps the sessions we already had and records why: a host
	// going quiet for one cycle should dim its rows, not erase them.
	if prev, ok := w.hosts[h.Alias]; ok && h.Err != nil {
		h.Sessions = prev.Sessions
		h.Seen = prev.Seen
	}
	w.hosts[h.Alias] = h
}

// fetchHost runs both tmux queries in ONE ssh round trip. Two calls would
// double the handshake cost for no benefit; the marker splits them again here.
func fetchHost(ctx context.Context, alias string) RemoteHost {
	ctx, cancel := context.WithTimeout(ctx, hostTimeout)
	defer cancel()

	remote := "tmux list-sessions -F '" + sessionFormat + "' 2>/dev/null; " +
		"echo " + remoteMarker + "; " +
		"tmux list-panes -a -F '" + paneFormat + "' 2>/dev/null"

	out, err := exec.CommandContext(ctx, "ssh", append(sshArgs(alias), remote)...).Output()
	if err != nil {
		return RemoteHost{Alias: alias, Err: err}
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
		remote := "tmux attach -t " + quote(name)
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
