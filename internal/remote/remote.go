// Package remote provisions Mojian's session server on an SSH host and
// keeps a framed connection to it. The UI and the pool stay in
// internal/app.
package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mojian/internal/platform"
	"mojian/internal/remotebin"
	"mojian/internal/session"
)

// ErrNeedsRestart says the host runs a server from another build, which
// cannot speak this protocol. The user stops it in Settings › Hosts,
// because stopping it also ends its terminals and agents.
var ErrNeedsRestart = errors.New("the server on the host is an older build; stop it in Settings › Hosts, then open the project again")

// Install is where a host keeps the server this build needs. It is
// probed over SSH before the binaries are compared and uploaded.
type Install struct {
	home       string
	arch       string
	dir        string
	binPath    string
	libPath    string
	runtimeDir string
	sock       string
}

// upload puts the server and the viewer library in place, skipping a file
// whose hash matches. It reports whether it uploaded anything.
func (i *Install) upload(ctx context.Context, host string) (bool, error) {
	bin, err := remotebin.Binary(i.arch)
	if err != nil {
		return false, err
	}
	lib, err := remotebin.Library(i.arch)
	if err != nil {
		return false, err
	}
	files := []struct {
		path, mode, label string
		data              []byte
	}{
		{i.binPath, "755", "the session server", bin},
		{i.libPath, "644", "libghostty-vt", lib},
	}
	// One ssh call hashes both files; a missing file makes the command
	// fail, which simply uploads both.
	hashes := map[string]string{}
	if got, err := remoteHashes(ctx, host, i.binPath, i.libPath); err == nil {
		hashes = got
	}
	uploaded := false
	for _, f := range files {
		want := sha256.Sum256(f.data)
		if strings.EqualFold(hashes[f.path], hex.EncodeToString(want[:])) {
			continue
		}
		if err := uploadRemote(ctx, host, f.path, f.data, f.mode); err != nil {
			return uploaded, fmt.Errorf("uploading %s: %w", f.label, err)
		}
		uploaded = true
	}
	return uploaded, nil
}

// connect runs the server's stdio bridge over SSH and hands its pipes to
// a framed client. The bridge starts a detached server when none runs, so
// the connection survives the SSH session and several devices can talk to
// the same one.
func (i *Install) connect(ctx context.Context, host string) (*Conn, error) {
	ssh, err := sshPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(ssh, append(sshCommon(), "--", host,
		"MOJIAN_DIR="+shellQuote(i.dir)+" MOJIAN_RUNTIME_DIR="+shellQuote(i.runtimeDir)+
			" exec "+shellQuote(i.binPath)+" stdio")...)
	platform.HideConsole(cmd)
	var stderr syncBuffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	fail := func(err error) (*Conn, error) {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("ssh %s: %s", host, msg)
		}
		return nil, err
	}
	type result struct {
		client *session.Client
		err    error
	}
	done := make(chan result, 1)
	go func() {
		c, err := session.NewClient(pipe{r: stdout, w: stdin})
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return fail(r.err)
		}
		if err := ctx.Err(); err != nil {
			_ = r.client.Close()
			return fail(err)
		}
		return &Conn{Host: host, Path: i.binPath, ssh: cmd, Client: r.client}, nil
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-time.After(30 * time.Second):
		return fail(errors.New("the SSH connection timed out"))
	}
}

// dial connects, turning a failure on a host that still has a socket into
// ErrNeedsRestart: an older server there can never answer this protocol.
func (i *Install) dial(ctx context.Context, host string) (*Conn, error) {
	rc, err := i.connect(ctx, host)
	if err == nil {
		return rc, nil
	}
	if i.socketExists(ctx, host) {
		return nil, fmt.Errorf("%s: %w", host, ErrNeedsRestart)
	}
	return nil, err
}

// Setup is what provisioning a host found: whether this run uploaded the
// binaries and where the server lives.
type Setup struct {
	Uploaded bool
	Path     string
	Protocol int
	// Home is the host's home directory, where its folder picker starts.
	Home string
	// Conn is the live connection provisioning ended with. The caller owns
	// it and must Close it if it is not used.
	Conn *Conn
}

// sshCommon is the non-interactive SSH base. It deliberately leaves out
// ControlMaster: a stale master socket makes every later ssh wait on a
// process that is gone, and the commands here are few enough that a fresh
// connection is the reliable choice.
func sshCommon() []string {
	return []string{
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=20",
		"-o", "ServerAliveInterval=15",
	}
}

// sshPath finds the OpenSSH client, which Windows ships as an optional
// feature; there is no built-in fallback.
func sshPath() (string, error) {
	p, err := exec.LookPath("ssh")
	if err != nil {
		return "", errors.New("ssh was not found; install the OpenSSH client")
	}
	return p, nil
}

// SSHOutput runs a command over SSH with the user's config and agent.
func SSHOutput(host, command string) (string, error) {
	return SSHOutputContext(context.Background(), host, command)
}

// SSHOutputContext is SSHOutput that stops the command when ctx is done.
func SSHOutputContext(ctx context.Context, host, command string) (string, error) {
	ssh, err := sshPath()
	if err != nil {
		return "", err
	}
	args := append(append(sshCommon(), "--"), host, command)
	cmd := exec.CommandContext(ctx, ssh, args...)
	platform.HideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("ssh %s: %s", host, msg)
	}
	return strings.TrimSpace(string(out)), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// probe reads the host's home, architecture, user id and runtime
// directory, and makes sure the install and socket folders exist.
func probe(ctx context.Context, host string) (*Install, error) {
	// One ssh call: a socket under the home directory is not always
	// reachable through sshd, and SELinux labels it data_home_t on
	// Fedora, so the server's socket goes in the user's runtime
	// directory, passed to it as MOJIAN_RUNTIME_DIR.
	script := `d="$HOME/.local/share/mojian"; r="${XDG_RUNTIME_DIR:-/tmp}/mojian-$(id -u)";` +
		` mkdir -p "$d" "$r" && chmod 700 "$d" "$r" &&` +
		` printf '%s\n%s\n%s\n%s\n' "$HOME" "$(uname -m)" "$(id -u)" "$r"`
	out, err := SSHOutputContext(ctx, host, script)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 4 {
		return nil, fmt.Errorf("ssh %s: unexpected probe output %q", host, out)
	}
	home, arch, uidText, runtimeDir := lines[0], lines[1], lines[2], lines[3]
	uid, err := strconv.Atoi(strings.TrimSpace(uidText))
	if err != nil {
		uid = -1
	}
	dir := home + "/.local/share/mojian"
	return &Install{
		home:       home,
		arch:       arch,
		dir:        dir,
		binPath:    dir + "/mojian-server",
		libPath:    dir + "/libghostty-vt.so",
		runtimeDir: runtimeDir,
		sock:       session.SocketPathIn(runtimeDir, uid),
	}, nil
}

// Dial prepares a connection without touching the app's active
// project, so a switch can run it away from the UI thread.
func Dial(host string) (*Conn, error) {
	return DialContext(context.Background(), host)
}

// DialContext is Dial that stops at the next step when ctx is done.
func DialContext(ctx context.Context, host string) (*Conn, error) {
	if host == "" {
		return nil, fmt.Errorf("no server selected")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := probe(ctx, host)
	if err != nil {
		return nil, err
	}
	if _, err := info.upload(ctx, host); err != nil {
		return nil, err
	}
	return info.dial(ctx, host)
}

// Provision installs the server and checks that it answers. The returned
// connection is live and belongs to the caller; the server keeps running
// for project and terminal use.
func Provision(host string, existing *Conn) (*Setup, error) {
	return ProvisionContext(context.Background(), host, existing)
}

// ProvisionContext is Provision that stops at the next step when ctx is
// done.
func ProvisionContext(ctx context.Context, host string, existing *Conn) (*Setup, error) {
	if host == "" {
		return nil, fmt.Errorf("no server selected")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if existing != nil && existing.Healthy() {
		if hello, err := existing.Client.Hello(); err == nil && hello.Version == session.ProtocolVersion {
			return &Setup{Path: existing.Path, Protocol: session.ProtocolVersion, Conn: existing}, nil
		}
	}
	info, err := probe(ctx, host)
	if err != nil {
		return nil, err
	}
	uploaded, err := info.upload(ctx, host)
	if err != nil {
		return nil, err
	}
	rc, err := info.dial(ctx, host)
	if err != nil {
		return nil, err
	}
	return &Setup{Uploaded: uploaded, Path: info.binPath, Protocol: session.ProtocolVersion, Home: info.home, Conn: rc}, nil
}

// StopServer asks the server on host to exit. Its terminals and the
// processes inside them end; the next Dial or Provision starts a fresh
// server.
func StopServer(host string) error {
	return StopServerContext(context.Background(), host)
}

// StopServerContext is StopServer that stops waiting when ctx is done.
func StopServerContext(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("no server selected")
	}
	info, err := probe(ctx, host)
	if err != nil {
		return err
	}
	if err := info.stop(ctx, host); err != nil {
		return err
	}
	info.waitStopped(ctx, host)
	return nil
}

// ServerRunning reports whether the server on host still has its socket.
// A host that cannot be reached is reported as not running: the next
// Start Server or Dial surfaces the SSH error.
func ServerRunning(ctx context.Context, host string) (bool, error) {
	if host == "" {
		return false, fmt.Errorf("no server selected")
	}
	info, err := probe(ctx, host)
	if err != nil {
		return false, err
	}
	if _, err := SSHOutputContext(ctx, host, "test -S "+shellQuote(info.sock)); err != nil {
		return false, nil
	}
	return true, nil
}

// socketExists reports whether the server socket is already on the host,
// so dial can tell an old server from an absent one.
func (i *Install) socketExists(ctx context.Context, host string) bool {
	_, err := SSHOutputContext(ctx, host, "test -S "+shellQuote(i.sock))
	return err == nil
}

// stop asks the running server to exit, through its own -stop command.
func (i *Install) stop(ctx context.Context, host string) error {
	_, err := SSHOutputContext(ctx, host, "MOJIAN_DIR="+shellQuote(i.dir)+
		" MOJIAN_RUNTIME_DIR="+shellQuote(i.runtimeDir)+
		" "+shellQuote(i.binPath)+" -stop")
	return err
}

// waitStopped waits for a remote server socket to disappear after its
// shutdown request, so the next process can take the lock. One ssh call
// polls on the host instead of one connection per check.
func (i *Install) waitStopped(ctx context.Context, host string) {
	_, _ = SSHOutputContext(ctx, host, "for i in $(seq 50); do test -S "+
		shellQuote(i.sock)+" || exit 0; sleep 0.1; done")
}

// remoteHashes reads the hashes of the files on host that exist, in one
// ssh call.
func remoteHashes(ctx context.Context, host string, paths ...string) (map[string]string, error) {
	args := make([]string, len(paths))
	for j, p := range paths {
		args[j] = shellQuote(p)
	}
	list := strings.Join(args, " ")
	script := "if command -v sha256sum >/dev/null 2>&1; then sha256sum -- " + list +
		"; else shasum -a 256 -- " + list + "; fi"
	out, err := SSHOutputContext(ctx, host, script)
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string, len(paths))
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hashes[fields[len(fields)-1]] = strings.ToLower(fields[0])
	}
	return hashes, nil
}

// uploadRemote writes data to path on host through SSH, atomically.
func uploadRemote(ctx context.Context, host, path string, data []byte, mode string) error {
	ssh, err := sshPath()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ssh, append(sshCommon(), "--", host,
		"mkdir -p "+shellQuote(filepath.Dir(path))+" && cat > "+shellQuote(path+".tmp")+
			" && chmod "+mode+" "+shellQuote(path+".tmp")+" && mv "+shellQuote(path+".tmp")+" "+shellQuote(path))...)
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	platform.HideConsole(cmd)
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%v %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Close drops the connection and the SSH process without stopping the
// server or its terminals on the host.
func Close(rc *Conn) {
	if rc == nil {
		return
	}
	if rc.Client != nil {
		// Detach only: the server and its terminals keep running for the
		// next connection.
		_ = rc.Client.Close()
	}
	if rc.ssh != nil && rc.ssh.Process != nil {
		_ = rc.ssh.Process.Kill()
		_, _ = rc.ssh.Process.Wait()
	}
}

// Conn is a bridge to one remote session server: the stdio of an SSH
// command, and the framed control connection through it.
type Conn struct {
	Host string
	// Path is the server binary on the host.
	Path   string
	ssh    *exec.Cmd
	Client *session.Client
}

// Healthy reports whether the connection still has a live control client.
func (c *Conn) Healthy() bool {
	if c == nil || c.Client == nil {
		return false
	}
	select {
	case <-c.Client.Closed():
		return false
	default:
		return true
	}
}

// pipe is one SSH child's stdio, so a framed client can own both ends as
// one stream.
type pipe struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p pipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipe) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipe) Close() error {
	_ = p.w.Close()
	return p.r.Close()
}

// syncBuffer collects the child's stderr while another goroutine may
// still be filling it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
