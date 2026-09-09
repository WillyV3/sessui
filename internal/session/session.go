// Package session is the pure data layer for sessui: it shells out to tmux
// and cp3, parses their output, and exposes a plain Session struct plus a
// few display-formatting helpers. It has no dependency on any UI framework,
// which is what makes it cheap to unit test.
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

// Session is one tmux session enriched with its working directory, the
// non-shell commands running in it, and (if it joins to a live cp3 peer by
// cwd) that peer's identity.
type Session struct {
	Name     string
	Created  time.Time
	Activity time.Time
	Attached bool
	Windows  int
	// Host is the ssh alias this session lives on, empty for local. Empty
	// rather than "local" on purpose: local is the default and unlabelled, so
	// a local-only user never sees a column telling them where they are.
	Host string
	// LastAttached is when the client last switched INTO this session; the
	// zero time if it never has been. The list is ordered by it, most recent
	// first, so the top row is the session you were in before this one.
	// Deliberately not Activity: a session running an agent produces output
	// constantly, so ordering by activity would reshuffle the list under the
	// cursor every reload.
	LastAttached time.Time
	CWD          string
	Apps         []string   // distinct non-shell commands, first-seen order
	Agent        bool       // Apps includes an AI coding agent (see IsAgentApp)
	State        AgentState // the agent's live activity; StateNone if !Agent

	// PeerUp is whether this session's cwd joins to a cp3 peer row that is up.
	//
	// This was a `Machine string` holding the peer's machine name, and it was
	// only ever tested for emptiness -- a boolean wearing a string. It also had
	// a column rendering it as though it were the session's location, which
	// labelled a proxy for a Mac session "omarchy". As a bool it cannot be
	// mistaken for a place again.
	PeerUp bool

	// PeerName is the workspace's peer identity: the cwd-matched up peer's
	// name, or, when down, the name recorded in its .claude-peers-agent
	// marker (see peerMarkerName). "" when the workspace isn't a peer
	// workspace at all.
	PeerName string

	// OwedMail is true when PeerName has unread cp3 mail waiting (pending >
	// 0) -- true for both a live peer and a down workspace whose peer still
	// has mail queued (the "dead session holding unread mail" case).
	OwedMail bool

	// PeerSummary is the peer's own deliberately-authored cp3 summary
	// (set_summary), when it has one. It's preferred over Summary -- a
	// summary the peer chose to say beats a static terminal title. See
	// EffectiveSummary.
	PeerSummary string

	// Summary is the session's AI agent pane title, stripped of claude's
	// "✳ " marker, with two cases filtered to "": a shell prompt title (no
	// marker at all -- see stripAgentTitleMarker) and a title that merely
	// echoes the session or peer name back (see agentSummary). What's left
	// is a genuine live task summary.
	Summary string

	// WorkingVerb and WorkingElapsed are claude's live status, e.g.
	// "Concocting" and "4m 34s" out of "✽ Concocting… (4m 34s · ↓ 18.6k
	// tokens)". Both empty unless State is StateWorking and the capture
	// matched that shape -- see extractWorkingStatus.
	WorkingVerb    string
	WorkingElapsed string
}

// IsPeer reports whether this session is a live (up) cp3 peer workspace.
func (s Session) IsPeer() bool { return s.PeerUp }

// AgentExited reports whether this is a known peer workspace (PeerName is
// set) whose agent isn't currently live (Machine is ""): a down spawn-peer
// workspace -- a zombie tmux session left over after claude exited, or
// simply a peer that hasn't been reopened yet.
func (s Session) AgentExited() bool { return s.PeerName != "" && !s.PeerUp }

// EffectiveSummary is what the status column shows for this session's
// current work: the peer's own authored cp3 summary when it has one (a
// deliberate set_summary beats a static terminal title), else the filtered
// native agent-pane-title Summary. "" when neither is available.
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
	// pane_active and window_active together pick out the pane an agent
	// is actually running in, so State detection captures the right one
	// even when a session has split panes; window_bell_flag drives Notify;
	// pane_id is the capture-pane target; pane_title is claude's live
	// semantic summary of what it's doing ("✳ Report to caretaker") when
	// the pane runs an agent, else just the shell's prompt title -- it's
	// last because it's the only field that can itself contain "|" (see
	// parsePanes).
	paneFormat = "#{session_name}|#{pane_current_path}|#{pane_current_command}|#{window_active}|#{pane_active}|#{window_bell_flag}|#{pane_id}|#{pane_title}"

	// capturePaneLines is how much of an agent pane's tail List reads to
	// classify Working vs Idle.
	capturePaneLines = 20
)

// List shells out to tmux (and cp3, best-effort) to build the current
// session list, excluding whichever session the caller is attached to.
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

// ListFast is List without the two slow parts: no cp3 roster and no pane
// captures, so it is tmux alone.
//
// Measured on a real 17-session server: cp3 is 268ms of a 470ms load and the
// tmux calls are 9ms of it. Blocking the first paint on the roster meant the
// popup showed an empty table reading "0 sessions" for most of half a second --
// stating a falsehood while it worked. This returns rows in roughly the time
// tmux takes, and Enrich fills in the rest underneath them.
//
// The peer column and agent state are simply absent until then, which is
// honest: they are unknown, not zero.
func ListFast() ([]Session, error) {
	// ONE tmux invocation, not three. tmux chains commands with `;` and prints
	// the results in order, so the current session, the session list and the
	// pane list arrive together.
	//
	// This is where the remaining time actually was. Measured from a plain
	// shell, with no Go involved at all: three separate `tmux` calls cost 10ms
	// and the same data in one call costs 4ms. The cost is process spawning and
	// socket round trips -- rewriting the parsing in a faster language would
	// have moved none of it, because the parsing was never the expense.
	//
	// display-message -p doubles as the marker printer; tmux has no echo.
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
// "|", so they can never be mistaken for one of the pipe-delimited records on
// either side of them.
const (
	listMarkerSessions = "@@sessui-sessions@@"
	listMarkerPanes    = "@@sessui-panes@@"
)

// classifyAgents captures each agent's pane to tell Working from Idle.
//
// Concurrent: one capture is 6ms and they are independent, so a box with a
// dozen agents paid a dozen round trips in a row for no reason. Bounded by the
// number of agent sessions, which is bounded by what a person can run.
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

// peerFleet is what cp3 was able to tell us about the fleet.
//
// Reachable separates two facts that look identical in a bare []peerRow but
// mean opposite things at a workspace carrying a .claude-peers-agent marker:
//
//   - Reachable, and the marker's peer isn't in Rows -> that peer really is
//     down. The marker is evidence, and the workspace renders as a zombie.
//   - Not reachable -> cp3 isn't installed or didn't answer. We know nothing
//     about liveness, and the marker is evidence of nothing. Treating it as
//     "down" tells the user their whole fleet is dead when in fact we simply
//     never asked (see TestBuild_PeerJoin_CP3Unreachable).
//
// The zero value is the honest default for a machine with no cp3: not
// reachable, no rows.
type peerFleet struct {
	Rows      []peerRow
	Reachable bool
}

// fetchPeers is cp3's peer roster, best-effort: `cp3 peers --json` first,
// falling back to the older plain `cp3 peers` table for a peer elsewhere on
// the fleet still running a cp3 build without --json. Any failure of either
// -- missing binary, non-zero exit, malformed JSON -- yields the zero
// peerFleet (not reachable); List never surfaces a cp3 error to the user.
//
// An empty-but-reachable roster is deliberately distinct from an unreachable
// one: cp3 answering "no peers are up" is real information about liveness,
// and markers are read against it.
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

// currentSession is the session the popup was opened FROM, which is the one
// session the switcher must not offer.
//
// It asks for #{client_session}, NOT #S. Inside a tmux popup #S resolves
// against a target that is not the attached client, and it answers with an
// unrelated session: measured on 2026-09-07 with api-gateway attached, #S returned
// "plugin-dev" from a popup while #{client_session} returned "api-gateway". Using
// #S therefore got this exactly wrong in both directions -- the session you
// were sitting in was listed, and an innocent one was hidden. #{client_session}
// is correct even with TMUX unset, since it resolves through the client rather
// than the caller's environment.
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

// popupWidthOption is the tmux user option sessui.tmux reads at open time to
// size the popup. It lives in tmux rather than sessui's own config because
// tmux needs it before this binary runs.
const popupWidthOption = "@sessui-width"

// PopupWidth is the configured popup width, or 0 when unset or unreadable --
// the caller applies the default, so a missing option and a missing tmux
// look the same.
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

// SetPopupWidth stores the width for the next open. It writes the global
// option, which is what sessui.tmux's run-shell substitution reads.
func SetPopupWidth(w int) error {
	return exec.Command("tmux", "set-option", "-g", popupWidthOption, strconv.Itoa(w)).Run()
}

// Build is the pure parse-and-combine step, split out from List so it can
// be exercised with table tests against captured tmux/cp3 output. Alongside
// the sessions it returns each agent session's pane id (name -> pane_id),
// which List uses to target its capture-pane calls; Classify itself is
// tested directly, so Build's own tests only need to cover the bell-only
// classification (State is Classify(agent, bell, "")).
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
		// cwd -- but only once cp3 has actually answered, since "absent from
		// a roster we never received" is not evidence that a peer is down.
		// See peerFleet.
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

	// Most recently attached first, so the top row is the session you were in
	// before this one -- the overwhelmingly common switch target. Stable, so
	// sessions that have never been attached (zero time) keep tmux's own
	// ordering among themselves at the bottom rather than shuffling per call.
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].LastAttached.After(sessions[j].LastAttached)
	})
	return sessions, agentPane
}

// peerRow is one entry from `cp3 peers --json` (or, missing Pending/Summary/
// LastMailSecs, one adapted from the older plain `cp3 peers` table -- see
// parsePeersPlain).
type peerRow struct {
	Name         string `json:"name"`
	Up           bool   `json:"up"`
	Pending      int    `json:"pending"`
	Machine      string `json:"machine,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Summary      string `json:"summary,omitempty"`
	LastMailSecs int    `json:"last_mail_secs,omitempty"`
}

// indexPeers builds the two lookups Build's peer join needs: cwd -> the
// first up peer's name at that cwd (so two peers sharing a cwd, e.g. a
// ClaimWithFallback suffix pair like deploy-bot/deploy-bot-laptop, both
// resolve to one canonical peer name), and name -> the full row (first
// name wins on a duplicate, which cp3 shouldn't produce).
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

// parsePeersJSON parses `cp3 peers --json`'s output: a JSON array,
// server-filtered to peers that are either up or carry pending mail.
func parsePeersJSON(raw string) ([]peerRow, error) {
	var rows []peerRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// parsePeersPlain parses the older `cp3 peers` table format ("AGENT
// MACHINE CWD" header, one row per line), used as a best-effort fallback
// when a cp3 elsewhere on the fleet predates --json. The plain table only
// ever lists live peers, so every row is implicitly Up with no
// Pending/Summary data.
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

// parsePeers parses the plain `cp3 peers` table into the simple name ->
// machine map some callers still just want, on top of parsePeersPlain so
// the two formats share one parse.
func parsePeers(raw string) map[string]string {
	peers := map[string]string{}
	for _, r := range parsePeersPlain(raw) {
		peers[r.Name] = r.Machine
	}
	return peers
}

// spawnPeerMarker is the file spawn-peer drops in a workspace it creates,
// identifying it as an agent workspace even after the agent itself exits.
const spawnPeerMarker = ".claude-peers-agent"

// hasSpawnPeerMarker reports whether cwd is a spawn-peer workspace.
func hasSpawnPeerMarker(cwd string) bool {
	if cwd == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(cwd, spawnPeerMarker))
	return err == nil
}

// peerMarkerName reads the first line of a spawn-peer workspace's marker
// file -- the peer name it was created for -- so a down workspace (no live
// claude pane, no cwd-matched cp3 peer) can still surface which peer it
// belongs to. "" when cwd has no marker, or it can't be read.
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

// agentTitleMarker is the prefix claude sets on its pane's terminal title
// to carry a live semantic summary of what it's doing, e.g. "✳ Report to
// caretaker".
const agentTitleMarker = "✳ "

// stripAgentTitleMarker strips claude's leading "✳ " marker from an agent
// pane's raw title, returning "" when the marker isn't present -- a shell
// prompt title ("willy@omarchy:~/x") is not a summary.
func stripAgentTitleMarker(title string) string {
	if !strings.HasPrefix(title, agentTitleMarker) {
		return ""
	}
	return strings.TrimPrefix(title, agentTitleMarker)
}

// NormalizeName folds a name to a comparable form -- lowercase, spaces
// turned to hyphens -- so title-derived text can be checked against a
// session or peer name regardless of casing or hyphen-vs-space style.
func NormalizeName(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), " ", "-")
}

// agentSummary turns an agent pane's raw title into Session.Summary: ""
// when there's no "✳ " marker at all (a shell title), or when the marker
// text is just an echo of the workspace's own identity -- the session name
// or (if it has one) its peer name, compared with NormalizeName so casing
// and hyphen-vs-space don't cause a false miss (session "Jim" vs peer
// "jim"). What's left is a genuine live task summary.
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

// parseSessions parses the output of:
//
//	tmux list-sessions -F '#{session_name}|#{session_created}|#{session_activity}|#{session_attached}|#{session_windows}'
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
		// last_attached is EMPTY for a session that has never been attached
		// (`tmux new -d`), so it is parsed leniently and left as the zero
		// time. Treating it like the other fields would drop those sessions
		// from the list entirely -- silently, since the loop just continues.
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

// parsePanes parses the output of `tmux list-panes -a -F` in paneFormat
// (session_name|pane_current_path|pane_current_command|window_active|
// pane_active|window_bell_flag|pane_id|pane_title). pane_title is split
// with SplitN so a title containing "|" doesn't get mistaken for extra
// fields. It returns, per session name: the cwd (active window's pane
// path, falling back to the first pane seen), the distinct non-shell
// commands running across all its panes in first-seen order, whether any
// window has its bell flag set, the pane id to capture-pane for a session
// running an agent, and that same agent pane's raw title -- in both cases
// the truly focused pane (window_active && pane_active) if one is running
// the agent, else the first agent pane seen.
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
// tmux's session_created resets on every server restart, so age is usually small —
// hours/days matter more than a whole-days count that reads "0" all day.
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

// Age humanizes how long ago a session was created ("5h", "2d", "3w").
func Age(created, now time.Time) string { return humanizeDuration(now.Sub(created)) }

// LastActive humanizes the time since last activity ("now", "8s", "3m", "2h", "5d").
func LastActive(activity, now time.Time) string { return humanizeDuration(now.Sub(activity)) }

// AbbreviatePath renders path fish-style: $HOME collapses to "~", and every
// path component is shortened to its first rune except the last, e.g.
// "/home/willy/projects/work/willys-tmux" -> "~/p/w/willys-tmux".
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
