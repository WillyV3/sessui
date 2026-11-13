// Package session is the data layer: it shells out to tmux and cp3, parses
// their output, and exposes a plain Session struct. It has no UI dependency.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Session is one tmux session with its working directory, the non-shell
// commands running in it, and its cp3 peer binding when the cwd has one.
type Session struct {
	Name     string
	Created  time.Time
	Activity time.Time
	Attached bool
	Windows  int
	// Host is the ssh alias this session lives on; "" for local. Local is the
	// unlabelled default, so a local-only user never sees a location column.
	Host string
	// LastAttached is when a client last switched into this session, zero if
	// never. The list is ordered by it, not by Activity: an agent produces
	// output constantly and would reshuffle the list under the cursor.
	LastAttached time.Time
	CWD          string
	Apps         []string   // distinct non-shell commands, first-seen order
	Agent        bool       // Apps includes an AI coding agent (see IsAgentApp)
	State        AgentState // the agent's live activity; StateNone if !Agent

	// PeerUp is whether this session's cwd joins to a cp3 peer that is up.
	PeerUp bool

	// PeerName is the cwd-matched peer's name or, when down, the name in the
	// workspace's marker file. "" when the workspace is not a peer workspace.
	PeerName string

	// OwedMail is whether PeerName has unread cp3 mail, live or not.
	OwedMail bool

	// PeerSummary is the peer's own authored cp3 summary, preferred over
	// Summary because a summary the peer chose beats a scraped title.
	PeerSummary string

	// Summary is the agent pane's title with its marker stripped, "" when the
	// title is a shell prompt or merely echoes the session or peer name.
	Summary string

	// WorkingVerb and WorkingElapsed are the agent's live status ("Concocting",
	// "4m 34s"); empty unless State is StateWorking and the capture matched.
	WorkingVerb    string
	WorkingElapsed string
}

// IsPeer reports whether this session is a live cp3 peer workspace.
func (s Session) IsPeer() bool { return s.PeerUp }

// AgentExited reports a known peer workspace whose agent is not live: a
// session left behind after the agent exited, or a peer not reopened yet.
func (s Session) AgentExited() bool { return s.PeerName != "" && !s.PeerUp }

// EffectiveSummary is what the status column shows: the peer's own summary
// when it has one, else the pane-title Summary, else "".
func (s Session) EffectiveSummary() string {
	if s.PeerSummary != "" {
		return s.PeerSummary
	}
	return s.Summary
}

var shellCommands = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true,
}

const (
	// Records are "|"-separated with the one free-text field LAST, read with
	// a bounded SplitN, so nothing a user typed can split a record: a session
	// called "a|b" used to vanish from the list. Panes name their session by
	// tmux's own id for the same reason. A control-character separator would
	// be simpler and does not survive: tmux 3.4 prints one as "\037" and 3.8,
	// without a UTF-8 locale, as "_".
	sessionFormat = "#{session_id}|#{session_created}|#{session_activity}|#{session_attached}|#{session_windows}|#{session_last_attached}|#{session_name}"
	// pane_height is carried so agent panes can be captured in one batch: see
	// capturePanes.
	// ponytail: a working directory containing "|" spills into the title of
	// that one pane. A second record type per pane would fix it; nothing has
	// asked for it.
	paneFormat = "#{session_id}|#{window_active}|#{pane_active}|#{window_bell_flag}|#{pane_id}|#{pane_height}|#{pane_current_command}|#{pane_current_path}|#{pane_title}"

	// capturePaneLines is how much of an agent pane's tail is read to tell
	// Working from Idle.
	capturePaneLines = 20
)

// List builds the current session list from tmux and, best-effort, cp3,
// excluding the session the caller is attached to.
func List() ([]Session, error) {
	reading, err := readTmux(lastPanes.get())
	if err != nil {
		return nil, err
	}
	sessions, agentPane := Build(reading.sessOut, reading.paneOut, roster.get(), reading.current)
	classifyAgents(sessions, agentPane, reading.captures)
	return sessions, nil
}

// ListFast is List without the parts that can lag: no cp3 roster, no pane
// captures. It paints rows in the time tmux takes; List fills in the rest
// behind it. Peer and agent state are absent until then, which is honest:
// unknown, not zero.
func ListFast() ([]Session, error) {
	reading, err := readTmux(nil)
	if err != nil {
		return nil, err
	}
	sessions, _ := Build(reading.sessOut, reading.paneOut, peerFleet{}, reading.current)
	return sessions, nil
}

// tmuxReading is one read of the tmux server: everything a list build needs
// from it, out of one process.
type tmuxReading struct {
	current  string
	sessOut  string
	paneOut  string
	captures map[string]string // by pane id; nil when none were asked for, or the batch did not add up
}

// readTmux asks tmux for the attached client's session, every session, every
// pane and — speculatively — the contents of the panes the PREVIOUS build
// read, in a SINGLE invocation. tmux chains commands with `;` and prints their
// results in order; the process spawn and socket round trip dominate, so one
// chained call costs what its first command costs.
//
// The speculation is what keeps a refresh at one process rather than two: an
// agent pane cannot be named before the pane list comes back, so the last
// build's panes are read on the assumption the list has not changed, which on
// a settled session list is exactly right. A wrong guess costs one extra call
// for the panes that were missed, never a wrong answer — and it cannot corrupt
// the session list, because every session and pane record is printed before
// the first capture runs. A pane that closes mid-call truncates only the
// captures after it, which the height arithmetic then rejects wholesale.
func readTmux(speculate []agentPaneRef) (tmuxReading, error) {
	args := []string{
		"display-message", "-p", "#{client_session}", ";",
		"display-message", "-p", listMarkerSessions, ";",
		"list-sessions", "-F", sessionFormat, ";",
		"display-message", "-p", listMarkerPanes, ";",
		"list-panes", "-a", "-F", paneFormat,
	}
	if len(speculate) > 0 {
		args = append(args, ";", "display-message", "-p", listMarkerCaptures)
		for _, ref := range speculate {
			args = append(args, ";", "capture-pane", "-p", "-t", ref.ID)
		}
	}

	// Partial output is an ordinary outcome here, not a failure: a chained
	// command stops at its first error, and a speculated pane may have closed
	// since the last build — tmux then exits non-zero having already printed
	// every session and every pane. Discarding that would be worse than
	// useless. The build would not get far enough to forget the dead pane, so
	// every refresh after it would fail the same way, forever.
	out, runErr := runTmuxPartial(args...)

	current, rest, ok := strings.Cut(out, "\n"+listMarkerSessions+"\n")
	if !ok {
		return tmuxReading{}, errors.Join(errors.New("tmux batch: session marker missing"), runErr)
	}
	sessOut, rest, ok := strings.Cut(rest, listMarkerPanes+"\n")
	if !ok {
		return tmuxReading{}, errors.Join(errors.New("tmux batch: pane marker missing"), runErr)
	}

	reading := tmuxReading{current: strings.TrimSpace(current), sessOut: sessOut, paneOut: rest}
	if len(speculate) > 0 {
		// The marker is printed before the first capture runs, so the panes end
		// where it appears whether or not the captures that follow survived.
		paneOut, capOut, ok := strings.Cut(rest, listMarkerCaptures+"\n")
		if !ok {
			paneOut, _, _ = strings.Cut(rest, listMarkerCaptures)
		}
		reading.paneOut = paneOut
		if captures, ok := splitCaptures(capOut, speculate, capturePaneLines); ok {
			reading.captures = captures
		}
	}
	return reading, nil
}

// lastPanes is the pane set the previous build read: the basis for readTmux's
// speculative capture. Package state because List is a package function, and
// safe to be: a stale guess costs one extra tmux call, never a wrong row.
var lastPanes paneMemory

type paneMemory struct {
	mu   sync.Mutex
	refs []agentPaneRef
}

func (p *paneMemory) get() []agentPaneRef {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refs
}

func (p *paneMemory) put(refs []agentPaneRef) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refs = refs
}

// Markers separating the outputs of the batched call. They contain no "|", so
// they cannot be mistaken for a record, and no pane content follows a marker
// that a record could be read out of.
const (
	listMarkerSessions = "@@sessui-sessions@@"
	listMarkerPanes    = "@@sessui-panes@@"
	listMarkerCaptures = "@@sessui-captures@@"
)

// classifyAgents tells Working from Idle for every agent session.
//
// have holds whatever the tmux read already captured speculatively; only the
// panes it does not cover are read again. On a settled session list that is
// none of them, which is the point.
func classifyAgents(sessions []Session, agentPane map[string]agentPaneRef, have map[string]string) {
	refs := make([]agentPaneRef, 0, len(sessions))
	row := make(map[string]int, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		if !s.Agent || s.State == StateNotify {
			continue
		}
		ref, ok := agentPane[s.Name]
		if !ok || ref.ID == "" {
			continue
		}
		refs = append(refs, ref)
		row[ref.ID] = i
	}

	// The speculation was split on the heights of the PREVIOUS build, so a pane
	// resized since then was cut at the wrong offset. The sum check in
	// splitCaptures catches that unless two panes resized by equal and opposite
	// amounts, which it cannot see; comparing the heights themselves can.
	priorHeight := make(map[string]int, len(refs))
	for _, ref := range lastPanes.get() {
		priorHeight[ref.ID] = ref.Height
	}
	lastPanes.put(refs)

	missing := make([]agentPaneRef, 0, len(refs))
	for _, ref := range refs {
		if _, ok := have[ref.ID]; !ok || priorHeight[ref.ID] != ref.Height {
			missing = append(missing, ref)
		}
	}
	captures := have
	if len(missing) > 0 {
		captures = make(map[string]string, len(refs))
		for id, capture := range have {
			captures[id] = capture
		}
		for id, capture := range capturePanes(missing, capturePaneLines) {
			captures[id] = capture
		}
	}

	for _, ref := range refs {
		s := &sessions[row[ref.ID]]
		capture := captures[ref.ID]
		s.State = Classify(true, false, capture)
		if s.State == StateWorking {
			s.WorkingVerb, s.WorkingElapsed = extractWorkingStatus(capture)
		}
	}
}

// peerFleet is what cp3 reported about the fleet.
//
// Reachable separates two facts a bare []peerRow cannot: a peer absent from a
// roster we received is down, but absent from a roster we never received is
// unknown. Treating the second as down tells a user without cp3 that their
// whole fleet is dead. The zero value is the honest default: not reachable.
type peerFleet struct {
	Rows      []peerRow
	Reachable bool
	// Machine is this machine as cp3 names it. A session only joins a peer
	// that runs here: the same path exists on every box that syncs a home
	// directory. "" (not known) joins by path alone.
	Machine string
}

// roster serves the cp3 fleet to every list build.
var roster peerCache

// peerCacheTTL is how long a fetched roster is served before a refresh is
// started behind it. Well under a human's notice of a peer going down, and far
// above the list's own refresh, which is the point.
const peerCacheTTL = 3 * time.Second

// peerCache keeps the last roster so that refreshing the list never waits on
// cp3 — a subprocess that costs more than every tmux read of a list build put
// together. Only a cold cache blocks; once there is something to show, a stale
// roster is served and replaced behind the caller.
//
// A failed fetch still counts as an attempt, so a missing or wedged cp3 is
// paid for once per TTL rather than on every refresh. The zero value is an
// empty cache that fetches on first use.
type peerCache struct {
	mu       sync.Mutex
	fleet    peerFleet
	attempt  time.Time
	fetching bool
}

func (c *peerCache) get() peerFleet {
	c.mu.Lock()
	cold := c.attempt.IsZero()
	refresh := !c.fetching && (cold || time.Since(c.attempt) > peerCacheTTL)
	if refresh {
		c.fetching = true
	}
	cached := c.fleet
	c.mu.Unlock()

	switch {
	case !refresh:
		return cached
	case cold:
		return c.store(fetchPeers()) // nothing to show yet, so wait for it once
	default:
		go c.store(fetchPeers())
		return cached
	}
}

func (c *peerCache) store(f peerFleet) peerFleet {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fleet, c.attempt, c.fetching = f, time.Now(), false
	return f
}

// fetchPeers is cp3's roster, best-effort: `cp3 peers --json`, then the older
// plain table for a cp3 that predates --json. Any failure yields the zero
// peerFleet; a cp3 error never reaches the user.
// A deadline because cp3 talks to a broker over the network: without one, a
// wedged broker holds the roster fetch — and the cold caller waiting on it —
// open forever. Generous against a healthy cp3, which answers in tens of
// milliseconds.
const peerTimeout = 2 * time.Second

func fetchPeers() peerFleet {
	ctx, cancel := context.WithTimeout(context.Background(), peerTimeout)
	defer cancel()

	// cp3 names a machine by its hostname, and so does this.
	// ponytail: a peer that registered under a hostname the box has since
	// changed (a Mac renamed by DHCP) loses its column until it restarts.
	machine, _ := os.Hostname()
	if out, err := exec.CommandContext(ctx, "cp3", "peers", "--json").Output(); err == nil {
		if rows, err := parsePeersJSON(string(out)); err == nil {
			return peerFleet{Rows: rows, Reachable: true, Machine: machine}
		}
	}
	if out, err := exec.CommandContext(ctx, "cp3", "peers").Output(); err == nil {
		return peerFleet{Rows: parsePeersPlain(string(out)), Reachable: true, Machine: machine}
	}
	return peerFleet{}
}

// runTmuxPartial runs tmux and hands back whatever it printed, error or not,
// so a caller that can use an incomplete answer gets to decide for itself. The
// error carries tmux's own words, which a chained command needs: the exit
// status alone does not say which link failed.
func runTmuxPartial(args ...string) (string, error) {
	var stdout, stderr strings.Builder
	cmd := tmuxRead(args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("tmux %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// tmuxRead is a tmux command whose output is parsed. -u makes tmux print
// names as they are: without a UTF-8 locale -- a non-interactive ssh has
// none -- "café" comes back as "caf_", a session that then cannot be found.
func tmuxRead(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-u"}, args...)...)
}

func runTmux(args ...string) (string, error) {
	out, err := tmuxRead(args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// exact is a tmux target that names one session and nothing else. A bare name
// is also tried as a pattern and then as a prefix, so with "work" gone,
// `kill-session -t work` kills "work-prod". The list is up to one reload
// stale, so that is a row away from happening; "=" turns the guessing off.
func exact(name string) string { return "=" + name }

// hasSession reports whether a session with exactly this name exists.
func hasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", exact(name)).Run() == nil
}

// Switch makes tmux switch the client to the named session.
func Switch(name string) error {
	return exec.Command("tmux", "switch-client", "-t", exact(name)).Run()
}

// New creates a detached session named name and switches the client to it.
func New(name string) error {
	if err := exec.Command("tmux", "new-session", "-d", "-s", name).Run(); err != nil {
		return err
	}
	return Switch(name)
}

// Rename renames a tmux session.
func Rename(oldName, newName string) error {
	return exec.Command("tmux", "rename-session", "-t", exact(oldName), newName).Run()
}

// Kill kills a tmux session.
func Kill(name string) error {
	return exec.Command("tmux", "kill-session", "-t", exact(name)).Run()
}

// popupWidthOption is the tmux user option sessui.tmux reads to size the
// popup. It lives in tmux because tmux needs it before this binary runs.
const popupWidthOption = "@sessui-width"

// PopupWidth is the configured popup width, or 0 when unset or unreadable;
// the caller applies the default.
func PopupWidth() int {
	out, err := exec.Command("tmux", "show-option", "-gqv", popupWidthOption).Output()
	if err != nil {
		return 0
	}
	w, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return w
}

// SetPopupWidth stores the width for the next open.
func SetPopupWidth(w int) error {
	return exec.Command("tmux", "set-option", "-g", popupWidthOption, strconv.Itoa(w)).Run()
}

// Build is the pure parse-and-combine step, split from List so it can be
// exercised against captured output. It also returns each agent session's
// pane id, which List uses to target its captures.
func Build(sessOut, paneOut string, fleet peerFleet, current string) ([]Session, map[string]agentPaneRef) {
	metas := parseSessions(sessOut)
	names := make(map[string]string, len(metas))
	for _, m := range metas {
		names[m.id] = m.name
	}
	cwds, apps, bell, agentPane, titles := parsePanes(paneOut, names)
	peersByCWD, peersByName := indexPeers(fleet)

	sessions := make([]Session, 0, len(metas))
	for _, m := range metas {
		if m.name == current {
			continue
		}
		agent := hasAgentApp(apps[m.name])
		cwd := cwds[m.name]

		// The marker names this workspace's peer when cp3 has no row for its
		// cwd -- but only once cp3 has answered. See peerFleet.
		peerName := peersByCWD[cwd]
		if peerName == "" && fleet.Reachable {
			peerName = peerMarkerName(cwd)
		}
		var peerSummary string
		var peerUp, owedMail bool
		if row, ok := peersByName[peerName]; ok && peerName != "" {
			peerUp = row.Up
			peerSummary = row.Summary
			owedMail = row.Pending > 0
		}

		sessions = append(sessions, Session{
			Name:         m.name,
			Created:      m.created,
			Activity:     m.activity,
			Attached:     m.attached,
			LastAttached: m.lastAttached,
			Windows:      m.windows,
			CWD:          cwd,
			Apps:         apps[m.name],
			Agent:        agent,
			State:        Classify(agent, bell[m.name], ""),
			PeerUp:       peerUp,
			PeerName:     peerName,
			OwedMail:     owedMail,
			PeerSummary:  peerSummary,
			Summary:      agentSummary(titles[m.name], m.name, peerName),
		})
	}

	// Most recently attached first: the top row is the session you were in
	// before this one. Stable, so never-attached sessions keep tmux's order.
	slices.SortStableFunc(sessions, func(a, b Session) int {
		return b.LastAttached.Compare(a.LastAttached)
	})
	return sessions, agentPane
}

// peerRow is one entry from `cp3 peers --json`, or one adapted from the
// older plain table (see parsePeersPlain).
type peerRow struct {
	Name         string `json:"name"`
	Up           bool   `json:"up"`
	Pending      int    `json:"pending"`
	Machine      string `json:"machine,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Summary      string `json:"summary,omitempty"`
	LastMailSecs int    `json:"last_mail_secs,omitempty"`
}

// indexPeers builds Build's two lookups: cwd -> the first up peer at that
// cwd, so two peers sharing one (a name and its contested-name fallback)
// resolve to one; and name -> the full row.
func indexPeers(fleet peerFleet) (byCWD map[string]string, byName map[string]peerRow) {
	byCWD = map[string]string{}
	byName = map[string]peerRow{}
	for _, r := range fleet.Rows {
		here := fleet.Machine == "" || r.Machine == "" || strings.EqualFold(r.Machine, fleet.Machine)
		if r.Up && r.Cwd != "" && here {
			if _, ok := byCWD[r.Cwd]; !ok {
				byCWD[r.Cwd] = r.Name
			}
		}
		if _, ok := byName[r.Name]; !ok {
			byName[r.Name] = r
		}
	}
	return byCWD, byName
}

// parsePeersJSON parses `cp3 peers --json`: a JSON array of peers that are
// up or carry pending mail.
func parsePeersJSON(raw string) ([]peerRow, error) {
	var rows []peerRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// parsePeersPlain parses the older `cp3 peers` table ("AGENT MACHINE CWD"
// header, one row per line). It only ever lists live peers, so every row is
// Up with no pending or summary data.
func parsePeersPlain(raw string) []peerRow {
	var rows []peerRow
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // header or blank
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		row := peerRow{Name: f[0], Up: true, Machine: f[1]}
		if len(f) >= 3 {
			row.Cwd = f[2]
		}
		rows = append(rows, row)
	}
	return rows
}

// peerMarkers are the files a peer launcher drops in a workspace it creates,
// naming its peer after the agent exits: cp3's current name first, then the
// one from before its rename.
var peerMarkers = []string{".cp3-peer-name", ".claude-peers-agent"}

// peerMarkerName is the first line of a workspace's marker file -- the peer
// it was created for -- or "" when there is none.
func peerMarkerName(cwd string) string {
	if cwd == "" {
		return ""
	}
	for _, marker := range peerMarkers {
		if data, err := os.ReadFile(filepath.Join(cwd, marker)); err == nil {
			line, _, _ := strings.Cut(string(data), "\n")
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// agentTitleMarker is the prefix an agent sets on its pane title to carry a
// live summary of what it is doing.
const agentTitleMarker = "✳ "

// stripAgentTitleMarker strips the marker from a pane title, returning ""
// when it is absent: a shell prompt title is not a summary.
func stripAgentTitleMarker(title string) string {
	if !strings.HasPrefix(title, agentTitleMarker) {
		return ""
	}
	return strings.TrimPrefix(title, agentTitleMarker)
}

// NormalizeName folds a name to a comparable form -- lowercase, spaces to
// hyphens -- so a title can be checked against a session or peer name
// regardless of casing or hyphen-vs-space style.
func NormalizeName(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), " ", "-")
}

// agentSummary turns a pane title into Session.Summary: "" when there is no
// marker, or when the text only echoes the session or peer name.
func agentSummary(title, sessionName, peerName string) string {
	stripped := stripAgentTitleMarker(title)
	if stripped == "" {
		return ""
	}
	norm := NormalizeName(stripped)
	if norm == NormalizeName(sessionName) {
		return ""
	}
	if peerName != "" && norm == NormalizeName(peerName) {
		return ""
	}
	return stripped
}

func hasAgentApp(apps []string) bool {
	for _, a := range apps {
		if IsAgentApp(a) {
			return true
		}
	}
	return false
}

type sessionMeta struct {
	id           string // tmux's session id ("$3"): how a pane record names its session
	name         string
	created      time.Time
	activity     time.Time
	attached     bool
	windows      int
	lastAttached time.Time // zero when the session has never been attached
}

// parseSessions parses `tmux list-sessions -F sessionFormat`.
func parseSessions(raw string) []sessionMeta {
	metas := make([]sessionMeta, 0, strings.Count(raw, "\n"))
	for line := range strings.SplitSeq(raw, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "|", 7)
		if len(f) != 7 {
			continue
		}
		created, err1 := strconv.ParseInt(f[1], 10, 64)
		activity, err2 := strconv.ParseInt(f[2], 10, 64)
		windows, err3 := strconv.Atoi(f[4])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		// session_last_attached is empty for a session never attached, so it
		// is parsed leniently; treating it like the others would silently
		// drop those sessions.
		var lastAttached time.Time
		if ts, err := strconv.ParseInt(f[5], 10, 64); err == nil {
			lastAttached = time.Unix(ts, 0)
		}
		metas = append(metas, sessionMeta{
			id:           f[0],
			name:         f[6],
			created:      time.Unix(created, 0),
			activity:     time.Unix(activity, 0),
			attached:     f[3] == "1",
			windows:      windows,
			lastAttached: lastAttached,
		})
	}
	return metas
}

// parsePanes parses `tmux list-panes -a -F paneFormat` into, per session:
// the cwd (active window's pane, else the first seen), the distinct non-shell
// commands in first-seen order, whether any window's bell is set, and the
// agent pane's id and title -- the focused pane if it runs the agent, else
// the first agent pane seen.
func parsePanes(raw string, names map[string]string) (cwds map[string]string, apps map[string][]string, bell map[string]bool, agentPane map[string]agentPaneRef, titles map[string]string) {
	cwds = map[string]string{}
	apps = map[string][]string{}
	bell = map[string]bool{}
	agentPane = map[string]agentPaneRef{}
	titles = map[string]string{}

	fallbackCWD := map[string]string{}
	activeCWD := map[string]bool{}
	seenApp := map[string]map[string]bool{}
	fallbackAgentPane := map[string]agentPaneRef{}
	fallbackAgentTitle := map[string]string{}

	for line := range strings.SplitSeq(raw, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "|", 9)
		if len(f) != 9 {
			continue
		}
		name, ok := names[f[0]]
		if !ok {
			continue // a session created between the two listings; the next reload has it
		}
		windowActive, paneActive, bellFlag := f[1] == "1", f[2] == "1", f[3] == "1"
		cmd, path, title := f[6], f[7], f[8]
		// A height that will not parse costs the batched capture, nothing else:
		// zero fails the batch's arithmetic and sends it down the per-pane path.
		// The row itself still carries this session's cwd, apps and agent, so
		// dropping it over a missing height would lose far more than it saved.
		height, _ := strconv.Atoi(f[5])
		ref := agentPaneRef{ID: f[4], Height: height}

		if _, ok := fallbackCWD[name]; !ok {
			fallbackCWD[name] = path
		}
		if windowActive && !activeCWD[name] {
			cwds[name] = path
			activeCWD[name] = true
		}
		if bellFlag {
			bell[name] = true
		}

		if !shellCommands[cmd] {
			if seenApp[name] == nil {
				seenApp[name] = map[string]bool{}
			}
			if !seenApp[name][cmd] {
				seenApp[name][cmd] = true
				apps[name] = append(apps[name], cmd)
			}
		}

		if IsAgentApp(cmd) {
			if _, ok := fallbackAgentPane[name]; !ok {
				fallbackAgentPane[name] = ref
				fallbackAgentTitle[name] = title
			}
			if windowActive && paneActive {
				agentPane[name] = ref
				titles[name] = title
			}
		}
	}

	for name, path := range fallbackCWD {
		if _, ok := cwds[name]; !ok {
			cwds[name] = path
		}
	}
	for name, ref := range fallbackAgentPane {
		if _, ok := agentPane[name]; !ok {
			agentPane[name] = ref
			titles[name] = fallbackAgentTitle[name]
		}
	}

	return cwds, apps, bell, agentPane, titles
}

// humanizeDuration renders a span compactly: "now", "8s", "3m", "2h", "5d", "3w".
func humanizeDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

// Age humanizes how long ago a session was created.
func Age(created, now time.Time) string { return humanizeDuration(now.Sub(created)) }

// LastActive humanizes the time since last activity.
func LastActive(activity, now time.Time) string { return humanizeDuration(now.Sub(activity)) }

// AbbreviatePath renders path fish-style: $HOME collapses to "~" and every
// component but the last shortens to its first rune, so
// "/home/u/projects/work/app" becomes "~/p/w/app".
func AbbreviatePath(path, home string) string {
	if path == "" || path == "/" {
		return path
	}

	prefix, rest := "/", strings.TrimPrefix(path, "/")
	if home != "" && home != "/" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			prefix, rest = "~/", strings.TrimPrefix(path, home+"/")
		}
	}

	parts := strings.Split(rest, "/")
	for i, p := range parts {
		if i == len(parts)-1 || p == "" {
			continue
		}
		r := []rune(p)
		parts[i] = string(r[0])
	}
	return prefix + strings.Join(parts, "/")
}
