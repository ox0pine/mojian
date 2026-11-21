// Package remote provisions Mojian's session server on an SSH host and
// keeps a framed connection to it. The UI and the pool stay in
// internal/app.
package remote

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os/exec"
	"path"
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

// upload installs the server and viewer library as one content-addressed
// release. The current symlink is the only commit point, so a failed or
// concurrent upload can never expose half of a release.
func (i *Install) upload(ctx context.Context, host string) (bool, error) {
	bin, err := remotebin.Binary(i.arch)
	if err != nil {
		return false, err
	}
	lib, err := remotebin.Library(i.arch)
	if err != nil {
		return false, err
	}
	// One ssh call hashes both files; a missing legacy or current file
	// makes the command fail, which simply uploads a complete release.
	hashes := map[string]string{}
	if got, err := remoteHashes(ctx, host, i.binPath, i.libPath); err == nil {
		hashes = got
	}
	binHash := sha256.Sum256(bin)
	libHash := sha256.Sum256(lib)
	if strings.EqualFold(hashes[i.binPath], hex.EncodeToString(binHash[:])) &&
		strings.EqualFold(hashes[i.libPath], hex.EncodeToString(libHash[:])) {
		return false, nil
	}
	h := sha256.New()
	_, _ = h.Write(binHash[:])
	_, _ = h.Write(libHash[:])
	release := hex.EncodeToString(h.Sum(nil))
	if err := i.uploadRelease(ctx, host, release, bin, lib); err != nil {
		return false, fmt.Errorf("uploading the session server and libghostty-vt: %w", err)
	}
	return true, nil
}

// uploadRelease streams both files into a unique staging directory. A
// release directory and then the current symlink are each replaced with
// one rename, while the remote trap removes every uncommitted artifact.
func (i *Install) uploadRelease(ctx context.Context, host, release string, bin, lib []byte) error {
	ssh, err := sshPath()
	if err != nil {
		return err
	}
	base := shellQuote(i.dir)
	script := `set -eu
base=` + base + `
release=` + shellQuote(release) + `
releases="$base/releases"
mkdir -p "$releases"
stage=$(mktemp -d "$base/.tmp-upload.XXXXXX")
link=
created=
cleanup() {
	rm -rf -- "$stage"
	if [ -n "$link" ]; then rm -f -- "$link"; fi
	if [ -n "$created" ] && [ "$(readlink "$base/current" 2>/dev/null || true)" != "releases/$release" ]; then
		rm -rf -- "$created"
	fi
}
trap cleanup EXIT HUP INT TERM
tar -xf - -C "$stage"
chmod 755 "$stage/mojian-server"
chmod 644 "$stage/libghostty-vt.so"
exec 9>"$base/.provision.lock"
flock 9
final="$releases/$release"
if [ -d "$final" ]; then
	rm -rf -- "$stage"
else
	mv "$stage" "$final"
	created="$final"
fi
link="$base/.current.${stage##*/}"
ln -s "releases/$release" "$link"
mv -Tf "$link" "$base/current"
link=
created=
if ! test -S ` + shellQuote(i.sock) + `; then
	for old in "$releases"/*; do
		[ -d "$old" ] || continue
		[ "$old" = "$final" ] || rm -rf -- "$old" || true
	done
	rm -f -- "$base/mojian-server" "$base/libghostty-vt.so" || true
fi
`
	cmd := exec.CommandContext(ctx, ssh, append(sshCommon(), "--", host, script)...)
	platform.HideConsole(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	tw := tar.NewWriter(stdin)
	writeErr := writeReleaseFile(tw, "mojian-server", 0o755, bin)
	if writeErr == nil {
		writeErr = writeReleaseFile(tw, "libghostty-vt.so", 0o644, lib)
	}
	if closeErr := tw.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if closeErr := stdin.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if waitErr != nil {
		return fmt.Errorf("%v %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return writeErr
}

func writeReleaseFile(w *tar.Writer, name string, mode int64, data []byte) error {
	if err := w.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
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
		_ = cmd.Wait()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("ssh %s: %s: %w", host, msg, err)
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
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		return &Conn{Host: host, Path: i.binPath, ssh: cmd, sshDone: done, Client: r.client}, nil
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-time.After(30 * time.Second):
		return fail(errors.New("the SSH connection timed out"))
	}
}

// dial connects and asks for a restart only after a real protocol
// mismatch. In particular, a socket plus a network or handshake failure
// is not evidence that the running server is old.
func (i *Install) dial(ctx context.Context, host string) (*Conn, error) {
	rc, err := i.connect(ctx, host)
	if err == nil {
		return rc, nil
	}
	var protocolErr *session.ProtocolError
	if errors.As(err, &protocolErr) {
		return nil, fmt.Errorf("%s: %w", host, ErrNeedsRestart)
	}
	if i.socketExists(ctx, host) && i.legacyProtocol(ctx, host) {
		return nil, fmt.Errorf("%s: %w", host, ErrNeedsRestart)
	}
	return nil, err
}

// legacyProtocol explicitly probes the pre-v4 one-line JSON protocol. A
// valid legacy hello is the only non-ProtocolError that proves the server
// needs a restart; timeouts and arbitrary connection errors do not.
func (i *Install) legacyProtocol(ctx context.Context, host string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ssh, err := sshPath()
	if err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, ssh, append(sshCommon(), "--", host,
		"MOJIAN_DIR="+shellQuote(i.dir)+" MOJIAN_RUNTIME_DIR="+shellQuote(i.runtimeDir)+
			" exec "+shellQuote(i.binPath)+" stdio")...)
	platform.HideConsole(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return false
	}
	if _, err := io.WriteString(stdin, "{\"id\":1,\"op\":\"hello\"}\n"); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return false
	}
	type lineResult struct {
		line []byte
		err  error
	}
	answer := make(chan lineResult, 1)
	go func() {
		line, err := bufio.NewReaderSize(stdout, 64<<10).ReadSlice('\n')
		answer <- lineResult{line, err}
	}()
	select {
	case result := <-answer:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if result.err != nil {
			return false
		}
		var response struct {
			ID    int64  `json:"id"`
			Error string `json:"error"`
			Data  struct {
				Version int `json:"version"`
			} `json:"data"`
		}
		if json.Unmarshal(result.line, &response) != nil || response.ID != 1 || response.Error != "" {
			return false
		}
		return response.Data.Version > 0 && response.Data.Version < session.ProtocolVersion
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return false
	}
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
		"-T",
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("ssh %s: %s", host, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
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
		binPath:    dir + "/current/mojian-server",
		libPath:    dir + "/current/libghostty-vt.so",
		runtimeDir: runtimeDir,
		sock:       remoteSocketPath(runtimeDir, uid),
	}, nil
}

// remoteSocketPath mirrors session.SocketPathIn using Linux POSIX path rules,
// so Windows and macOS clients agree with a remote Linux server.
func remoteSocketPath(dir string, uid int) string {
	p := path.Join(dir, "sessions.sock")
	if len(p) < 100 {
		return p
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(p))
	name := fmt.Sprintf("mojian-%x.sock", h.Sum32())
	if uid >= 0 {
		name = fmt.Sprintf("mojian-%d-%x.sock", uid, h.Sum32())
	}
	return path.Join("/tmp", name)
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
	if err := info.waitStopped(ctx, host); err != nil {
		return err
	}
	return info.cleanupStopped(ctx, host)
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
	command := "MOJIAN_DIR=" + shellQuote(i.dir) +
		" MOJIAN_RUNTIME_DIR=" + shellQuote(i.runtimeDir) +
		" bin=" + shellQuote(i.binPath) +
		"; export MOJIAN_DIR MOJIAN_RUNTIME_DIR" +
		"; test -x \"$bin\" || bin=" + shellQuote(i.dir+"/mojian-server") +
		"; \"$bin\" -stop"
	_, err := SSHOutputContext(ctx, host, command)
	return err
}

// waitStopped waits for a remote server socket to disappear after its
// shutdown request, so the next process can take the lock. One ssh call
// polls on the host instead of one connection per check.
func (i *Install) waitStopped(ctx context.Context, host string) error {
	_, err := SSHOutputContext(ctx, host, "for i in $(seq 50); do test -S "+
		shellQuote(i.sock)+" || exit 0; sleep 0.1; done; "+
		"printf 'server socket still exists after stop\\n' >&2; exit 1")
	if err != nil {
		return fmt.Errorf("waiting for the server to stop: %w", err)
	}
	return nil
}

// cleanupStopped removes superseded releases and the pre-release layout
// only once no running server can still need its library.
func (i *Install) cleanupStopped(ctx context.Context, host string) error {
	script := `set -eu
base=` + shellQuote(i.dir) + `
exec 9>"$base/.provision.lock"
flock 9
current=$(readlink "$base/current" 2>/dev/null || true)
for old in "$base/releases"/*; do
	[ -d "$old" ] || continue
	[ "releases/${old##*/}" = "$current" ] || rm -rf -- "$old"
done
rm -f -- "$base/mojian-server" "$base/libghostty-vt.so"
`
	if _, err := SSHOutputContext(ctx, host, script); err != nil {
		return fmt.Errorf("cleaning up old server releases: %w", err)
	}
	return nil
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

// uploadRemote writes data to remotePath on host through SSH, atomically.
func uploadRemote(ctx context.Context, host, remotePath string, data []byte, mode string) error {
	ssh, err := sshPath()
	if err != nil {
		return err
	}
	script := `set -eu
path=` + shellQuote(remotePath) + `
mkdir -p ` + shellQuote(path.Dir(remotePath)) + `
tmp=$(mktemp "$path.tmp.XXXXXX")
trap 'rm -f -- "$tmp"' EXIT HUP INT TERM
cat >"$tmp"
chmod ` + mode + ` "$tmp"
mv -f -- "$tmp" "$path"
trap - EXIT HUP INT TERM
`
	cmd := exec.CommandContext(ctx, ssh, append(sshCommon(), "--", host, script)...)
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
		if rc.sshDone != nil {
			<-rc.sshDone
		}
	}
}

// Conn is a bridge to one remote session server: the stdio of an SSH
// command, and the framed control connection through it.
type Conn struct {
	Host string
	// Path is the server binary on the host.
	Path    string
	ssh     *exec.Cmd
	sshDone chan struct{}
	Client  *session.Client
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
