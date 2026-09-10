// Package session is the data layer: it shells out to tmux and cp3, parses
// their output, and exposes a plain Session struct. It has no UI dependency.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	sessionFormat = "#{session_name}|#{session_created}|#{session_activity}|#{session_attached}|#{session_windows}|#{session_last_attached}"
	// pane_title is last because it is the only field that can contain "|";
	// parsePanes splits with a bounded SplitN for that reason.
	paneFormat = "#{session_name}|#{pane_current_path}|#{pane_current_command}|#{window_active}|#{pane_active}|#{window_bell_flag}|#{pane_id}|#{pane_title}"

	// capturePaneLines is how much of an agent pane's tail is read to tell
	// Working from Idle.
	capturePaneLines = 20
)

// List builds the current session list from tmux and, best-effort, cp3,
// excluding the session the caller is attached to.
func List() ([]Session, error) {
	current, err := currentSession()
	if err != nil {
		return nil, err
	}

	sessOut, err := runTmux("list-sessions", "-F", sessionFormat)
	if err != nil {
		return nil, err
	}

	paneOut, err := runTmux("list-panes", "-a", "-F", paneFormat)
	if err != nil {
		return nil, err
	}

	sessions, agentPane := Build(sessOut, paneOut, fetchPeers(), current)
	classifyAgents(sessions, agentPane)
	return sessions, nil
}

// ListFast is List without the slow parts: no cp3 roster, no pane captures.
// It paints rows in the time tmux takes; List fills in the rest behind it.
// Peer and agent state are absent until then, which is honest: unknown, not
// zero.
func ListFast() ([]Session, error) {
	// One tmux invocation, not three. tmux chains commands with `;` and prints
	// the results in order; process spawns and socket round trips are the cost,
	// not parsing. display-message -p doubles as the marker printer.
	out, err := runTmux(
		"display-message", "-p", "#{client_session}", ";",
		"display-message", "-p", listMarkerSessions, ";",
		"list-sessions", "-F", sessionFormat, ";",
		"display-message", "-p", listMarkerPanes, ";",
		"list-panes", "-a", "-F", paneFormat,
	)
	if err != nil {
		return nil, err
	}

	current, rest, ok := strings.Cut(out, "\n"+listMarkerSessions+"\n")
	if !ok {
		return nil, fmt.Errorf("tmux batch: session marker missing")
	}
	sessOut, paneOut, ok := strings.Cut(rest, listMarkerPanes+"\n")
	if !ok {
		return nil, fmt.Errorf("tmux batch: pane marker missing")
	}

	sessions, _ := Build(sessOut, paneOut, peerFleet{}, strings.TrimSpace(current))
	return sessions, nil
}

// Markers separating the three outputs of the batched call. They contain no
// "|", so they cannot be mistaken for a record.
const (
	listMarkerSessions = "@@sessui-sessions@@"
	listMarkerPanes    = "@@sessui-panes@@"
)

// classifyAgents captures each agent's pane, concurrently, to tell Working
// from Idle. Bounded by the number of agent sessions.
func classifyAgents(sessions []Session, agentPane map[string]string) {
	var wg sync.WaitGroup
	for i := range sessions {
		s := &sessions[i]
		if !s.Agent || s.State == StateNotify {
			continue
		}
		paneID := agentPane[s.Name]
		if paneID == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			capture := capturePane(paneID, capturePaneLines)
			s.State = Classify(true, false, capture)
			if s.State == StateWorking {
				s.WorkingVerb, s.WorkingElapsed = extractWorkingStatus(capture)
			}
		}()
	}
	wg.Wait()
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
}

// fetchPeers is cp3's roster, best-effort: `cp3 peers --json`, then the older
// plain table for a cp3 that predates --json. Any failure yields the zero
// peerFleet; a cp3 error never reaches the user.
func fetchPeers() peerFleet {
	if out, err := exec.Command("cp3", "peers", "--json").Output(); err == nil {
		if rows, err := parsePeersJSON(string(out)); err == nil {
			return peerFleet{Rows: rows, Reachable: true}
		}
	}
	if out, err := exec.Command("cp3", "peers").Output(); err == nil {
		return peerFleet{Rows: parsePeersPlain(string(out)), Reachable: true}
	}
	return peerFleet{}
}

func runTmux(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// currentSession is the session the popup was opened from, which the
// switcher must not offer.
//
// It asks for #{client_session}, not #S. Inside a popup #S resolves against a
// target that is not the attached client and answers with an unrelated
// session, which both lists the session you are in and hides an innocent one.
func currentSession() (string, error) {
	out, err := runTmux("display-message", "-p", "#{client_session}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Switch makes tmux switch the client to the named session.
func Switch(name string) error {
	return exec.Command("tmux", "switch-client", "-t", name).Run()
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
	return exec.Command("tmux", "rename-session", "-t", oldName, newName).Run()
}

// Kill kills a tmux session.
func Kill(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
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
func Build(sessOut, paneOut string, fleet peerFleet, current string) ([]Session, map[string]string) {
	metas := parseSessions(sessOut)
	cwds, apps, bell, agentPane, titles := parsePanes(paneOut)
	peersByCWD, peersByName := indexPeers(fleet.Rows)

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
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].LastAttached.After(sessions[j].LastAttached)
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
func indexPeers(peers []peerRow) (byCWD map[string]string, byName map[string]peerRow) {
	byCWD = map[string]string{}
	byName = map[string]peerRow{}
	for _, r := range peers {
		if r.Up && r.Cwd != "" {
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

// parsePeers reduces the plain table to a name -> machine map.
func parsePeers(raw string) map[string]string {
	peers := map[string]string{}
	for _, r := range parsePeersPlain(raw) {
		peers[r.Name] = r.Machine
	}
	return peers
}

// spawnPeerMarker is the file a peer launcher drops in a workspace it
// creates, identifying it as an agent workspace after the agent exits.
const spawnPeerMarker = ".claude-peers-agent"

// hasSpawnPeerMarker reports whether cwd is a peer workspace.
func hasSpawnPeerMarker(cwd string) bool {
	if cwd == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(cwd, spawnPeerMarker))
	return err == nil
}

// peerMarkerName is the first line of a workspace's marker file -- the peer
// it was created for -- or "" when there is none.
func peerMarkerName(cwd string) string {
	if cwd == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(cwd, spawnPeerMarker))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
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
	name         string
	created      time.Time
	activity     time.Time
	attached     bool
	windows      int
	lastAttached time.Time // zero when the session has never been attached
}

// parseSessions parses `tmux list-sessions -F sessionFormat`.
func parseSessions(raw string) []sessionMeta {
	var metas []sessionMeta
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) != 6 {
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
			name:         f[0],
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
func parsePanes(raw string) (cwds map[string]string, apps map[string][]string, bell map[string]bool, agentPane map[string]string, titles map[string]string) {
	cwds = map[string]string{}
	apps = map[string][]string{}
	bell = map[string]bool{}
	agentPane = map[string]string{}
	titles = map[string]string{}

	fallbackCWD := map[string]string{}
	activeCWD := map[string]bool{}
	seenApp := map[string]map[string]bool{}
	fallbackAgentPane := map[string]string{}
	fallbackAgentTitle := map[string]string{}

	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "|", 8)
		if len(f) != 8 {
			continue
		}
		name, path, cmd := f[0], f[1], f[2]
		windowActive, paneActive := f[3] == "1", f[4] == "1"
		bellFlag, paneID, title := f[5] == "1", f[6], f[7]

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
				fallbackAgentPane[name] = paneID
				fallbackAgentTitle[name] = title
			}
			if windowActive && paneActive {
				agentPane[name] = paneID
				titles[name] = title
			}
		}
	}

	for name, path := range fallbackCWD {
		if _, ok := cwds[name]; !ok {
			cwds[name] = path
		}
	}
	for name, id := range fallbackAgentPane {
		if _, ok := agentPane[name]; !ok {
			agentPane[name] = id
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
