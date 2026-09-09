package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// DiscoverHosts returns the connectable host aliases in ~/.ssh/config.
//
// Parsing is kevinburke/ssh_config's job, not ours -- it is the same library
// charmbracelet/wishlist uses for this, one layer down. Importing wishlist
// itself for the parser costs 16MB and 135 dependencies (measured) because its
// Endpoint type lives beside an SSH server; this is 3MB and one.
//
// Patterns are dropped, not resolved: `Host *` and friends configure other
// hosts, they are not machines you can reach. What is left is the set worth
// OFFERING the user -- never the set to start polling. A real config
// (2026-09-08, 18 literal hosts) contained `insipron` sitting next to
// `inspiron`, plus several devices that are powered off most of the day. Poll
// that list automatically and sessui spends every cycle retrying a typo.
func DiscoverHosts() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return nil // no ssh config is not an error, it is a machine with no hosts
	}
	defer f.Close()

	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil
	}

	var hosts []string
	seenAlias := map[string]bool{}
	seenMachine := map[string]bool{}
	for _, h := range cfg.Hosts {
		for _, p := range h.Patterns {
			alias := p.String()
			if strings.ContainsAny(alias, "*?!") || seenAlias[alias] {
				continue
			}
			seenAlias[alias] = true

			// Aliases are not machines. A real config had `inspiron`,
			// `inspiron-omarchy` and the typo `insipron` all resolving to
			// willy@100.96.252.51:22 -- polling each would list the same
			// sessions three times, under three names. Ask ssh which are the
			// same endpoint rather than guessing from the names.
			id := endpointOf(alias)
			if id != "" {
				if seenMachine[id] {
					continue
				}
				seenMachine[id] = true
			}
			hosts = append(hosts, alias)
		}
	}
	return hosts
}

// endpointOf resolves an alias to user@hostname:port using ssh's own config
// evaluation -- `ssh -G` reads the config and prints the effective settings
// without touching the network (measured: 7ms). Anything ssh understands is
// therefore understood here too, including Match blocks and Include.
//
// Returns "" when ssh cannot resolve it; the caller keeps such a host rather
// than dropping it, since failing to dedupe is better than losing a machine.
func endpointOf(alias string) string {
	out, err := exec.Command("ssh", "-G", alias).Output()
	if err != nil {
		return ""
	}
	var user, host, port string
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch k {
		case "user":
			user = v
		case "hostname":
			host = v
		case "port":
			port = v
		}
	}
	if host == "" {
		return ""
	}
	return user + "@" + host + ":" + port
}

// The first alias in the config wins for a machine reachable under several --
// it is the order the user wrote them in, which beats sorting by length and
// picking a typo.
