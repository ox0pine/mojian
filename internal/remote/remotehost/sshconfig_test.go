package remotehost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSSHConfigExtractsConcreteHosts(t *testing.T) {
	cfg := `
# Global defaults
Host *
    ServerAliveInterval 60

Host devbox staging
    User alice
    HostName 10.0.0.1

Host *.prod !bastion-deny prod-web
    User deploy

Match host foo
    Include ignored_in_match

Host=gpu-1
    Port 2222
`
	hosts := ParseSSHConfig(strings.NewReader(cfg))
	want := []string{"devbox", "staging", "prod-web", "gpu-1"}
	if len(hosts) != len(want) {
		t.Fatalf("got %+v, want %v", hosts, want)
	}
	for i, w := range want {
		if hosts[i].Address != w || hosts[i].Name != w {
			t.Fatalf("hosts[%d] = %+v, want %q", i, hosts[i], w)
		}
	}
}

func TestReadSSHConfigFollowsInclude(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "config.d")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "extra.conf"), []byte("Host extra-box\n  HostName 1.2.3.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainCfg := filepath.Join(dir, "config")
	if err := os.WriteFile(mainCfg, []byte("Include config.d/*.conf\nHost main-box\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hosts, err := ReadSSHConfig(mainCfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].Address != "main-box" || hosts[1].Address != "extra-box" {
		t.Fatalf("got %+v", hosts)
	}
}
