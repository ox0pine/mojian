package remotehost

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultSSHConfigPath returns ~/.ssh/config for the current user.
func DefaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// ReadSSHConfig parses concrete Host aliases from path (and any top-level
// Include directives inside ~/.ssh), skipping wildcards and negations.
func ReadSSHConfig(path string) ([]Host, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	seen := map[string]bool{}
	var out []Host
	if err := readSSHConfigDepth(path, filepath.Dir(path), 0, seen, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func readSSHConfigDepth(path, sshDir string, depth int, seen map[string]bool, out *[]Host) error {
	if depth > 3 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hosts, includes := parseSSHConfigWithIncludes(f)
	for _, h := range hosts {
		if seen[h.Address] {
			continue
		}
		seen[h.Address] = true
		*out = append(*out, h)
	}
	for _, inc := range includes {
		pattern := inc
		if strings.HasPrefix(pattern, "~/") {
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				pattern = filepath.Join(home, pattern[2:])
			}
		} else if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(sshDir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			_ = readSSHConfigDepth(m, sshDir, depth+1, seen, out)
		}
	}
	return nil
}

// ParseSSHConfig reads concrete Host entries from an OpenSSH config stream.
func ParseSSHConfig(r io.Reader) []Host {
	hosts, _ := parseSSHConfigWithIncludes(r)
	return hosts
}

func parseSSHConfigWithIncludes(r io.Reader) (hosts []Host, includes []string) {
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	inMatch := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip inline comments that are not inside quotes.
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		key, val, ok := splitSSHDirective(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "match":
			inMatch = true
		case "include":
			if !inMatch {
				includes = append(includes, strings.Fields(val)...)
			}
		case "host":
			inMatch = false
			for _, pat := range strings.Fields(val) {
				pat = strings.Trim(pat, `"'`)
				if pat == "" || strings.HasPrefix(pat, "!") || strings.ContainsAny(pat, "*?") {
					continue
				}
				if seen[pat] {
					continue
				}
				seen[pat] = true
				hosts = append(hosts, Host{Name: pat, Address: pat})
			}
		}
	}
	return hosts, includes
}

func splitSSHDirective(line string) (key, val string, ok bool) {
	i := strings.IndexAny(line, " \t=")
	if i <= 0 {
		return "", "", false
	}
	key = line[:i]
	val = strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
	if val == "" {
		return "", "", false
	}
	return key, val, true
}
