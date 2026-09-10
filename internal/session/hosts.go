package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// DiscoverHosts returns the connectable host aliases in ~/.ssh/config, one
// per machine, in the order written.
//
// Parsing is kevinburke/ssh_config's job -- the same parser wishlist uses.
// Importing wishlist itself for it costs 16MB and 135 dependencies, because
// its Endpoint type lives beside an SSH server; this is 3MB and one.
//
// Patterns (`Host *`) configure other hosts and are dropped. The result is
// the set worth offering, never the set to poll: a real config holds typos
// and machines that are off most of the day.
func DiscoverHosts() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return nil // no ssh config is a machine with no hosts, not an error
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

			// Several aliases often resolve to one endpoint; polling each
			// would list the same sessions under each name. The first alias
			// written wins, which beats guessing from the names.
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

// endpointOf resolves an alias to user@hostname:port with `ssh -G`, which
// evaluates the config -- Match blocks and Include too -- without touching
// the network. Returns "" when ssh cannot resolve it; the caller keeps such a
// host, since failing to dedupe beats losing a machine.
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
