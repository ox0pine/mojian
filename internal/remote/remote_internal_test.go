//go:build darwin || linux

package remote

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mojian/internal/session"
)

const sshArgLog = `printf '%s\n' "$@" >> "$MOJIAN_TEST_SSH_ARGS"
for command do :; done
`

func installFakeSSH(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + sshArgLog + body
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOJIAN_TEST_SSH_ARGS", filepath.Join(dir, "args"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func installShellSSH(t *testing.T) {
	t.Helper()
	installFakeSSH(t, "exec /bin/sh -c \"$command\"\n")
}

func framedHello(version int) string {
	payload, _ := json.Marshal(session.Hello{Version: version, Role: "server"})
	var frame bytes.Buffer
	_ = binary.Write(&frame, binary.BigEndian, uint32(len(payload)))
	frame.WriteByte(1)
	_ = binary.Write(&frame, binary.BigEndian, uint32(0))
	frame.Write(payload)
	var out strings.Builder
	out.WriteString("printf '")
	for _, b := range frame.Bytes() {
		fmt.Fprintf(&out, "\\%03o", b)
	}
	out.WriteString("'\n")
	return out.String()
}

func TestUploadRemoteConcurrentWritersUseUniqueTemporaryFiles(t *testing.T) {
	installShellSSH(t)
	dir := t.TempDir()
	remotePath := filepath.Join(dir, "mojian-server")
	const writers = 8
	payloads := make([][]byte, writers)
	start := make(chan struct{})
	errs := make(chan error, writers)
	for j := range payloads {
		payloads[j] = bytes.Repeat([]byte{byte('A' + j)}, 2<<20)
		go func(data []byte) {
			<-start
			errs <- uploadRemote(context.Background(), "test-host", remotePath, data, "755")
		}(payloads[j])
	}
	close(start)
	for range payloads {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, want := range payloads {
		if bytes.Equal(got, want) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatal("the final upload contains mixed or truncated data")
	}
	leftovers, err := filepath.Glob(remotePath + ".tmp.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary uploads remain: %v", leftovers)
	}
}

func TestUploadRemoteRemovesTemporaryFileOnFailure(t *testing.T) {
	installShellSSH(t)
	remotePath := filepath.Join(t.TempDir(), "mojian-server")
	if err := uploadRemote(context.Background(), "test-host", remotePath, []byte("server"), "invalid"); err == nil {
		t.Fatal("upload succeeded with an invalid mode")
	}
	leftovers, err := filepath.Glob(remotePath + ".tmp.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("failed upload left temporary files: %v", leftovers)
	}
}

func TestSSHOutputContextSeparatesStdoutAndStderr(t *testing.T) {
	installShellSSH(t)
	out, err := SSHOutputContext(context.Background(), "test-host", "printf 'clean stdout'; printf 'noisy stderr' >&2")
	if err != nil {
		t.Fatal(err)
	}
	if out != "clean stdout" {
		t.Fatalf("stdout was polluted: %q", out)
	}
	_, err = SSHOutputContext(context.Background(), "test-host", "printf 'command output'; printf 'specific failure' >&2; exit 7")
	if err == nil || !strings.Contains(err.Error(), "specific failure") {
		t.Fatalf("SSH failure omitted stderr: %v", err)
	}
}

func TestSSHCommandsDisableTTYWithoutOverridingUserConfig(t *testing.T) {
	installShellSSH(t)
	if _, err := SSHOutputContext(context.Background(), "test-host", "true"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(os.Getenv("MOJIAN_TEST_SSH_ARGS"))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(args))
	if len(fields) == 0 || fields[0] != "-T" {
		t.Fatalf("ssh did not disable TTY allocation: %q", args)
	}
	for _, arg := range fields {
		if arg == "-F" || arg == "-tt" {
			t.Fatalf("ssh overrode the user's terminal or config behavior: %q", args)
		}
	}
}

func TestDialPreservesArbitraryConnectErrorWithSocket(t *testing.T) {
	installFakeSSH(t, `case "$command" in
*stdio*) printf 'connection refused by bridge' >&2; exit 1 ;;
"test -S "*) exit 0 ;;
esac
`)
	i := &Install{dir: "/tmp/mojian", binPath: "/tmp/mojian/current/mojian-server", runtimeDir: "/tmp/runtime", sock: "/tmp/runtime/sessions.sock"}
	_, err := i.dial(context.Background(), "test-host")
	if err == nil || !strings.Contains(err.Error(), "connection refused by bridge") {
		t.Fatalf("dial lost the connection error: %v", err)
	}
	if errors.Is(err, ErrNeedsRestart) {
		t.Fatalf("arbitrary connection failure became ErrNeedsRestart: %v", err)
	}
}

func TestDialClassifiesProtocolError(t *testing.T) {
	installFakeSSH(t, `case "$command" in
*stdio*)
	cat >/dev/null &
	`+framedHello(session.ProtocolVersion-1)+`
	wait
	;;
esac
`)
	i := &Install{dir: "/tmp/mojian", binPath: "/tmp/mojian/current/mojian-server", runtimeDir: "/tmp/runtime", sock: "/tmp/runtime/sessions.sock"}
	_, err := i.dial(context.Background(), "test-host")
	if !errors.Is(err, ErrNeedsRestart) {
		t.Fatalf("protocol mismatch returned %v", err)
	}
}

func TestDialConfirmsLegacyProtocolBeforeRestart(t *testing.T) {
	dir := installFakeSSH(t, `case "$command" in
*stdio*)
	n=0
	if [ -f "$MOJIAN_TEST_LEGACY_COUNT" ]; then n=$(cat "$MOJIAN_TEST_LEGACY_COUNT"); fi
	n=$((n+1))
	printf '%s' "$n" > "$MOJIAN_TEST_LEGACY_COUNT"
	if [ "$n" -eq 1 ]; then exit 1; fi
	printf '%s\n' '{"id":1,"data":{"version":3}}'
	;;
"test -S "*) exit 0 ;;
esac
`)
	t.Setenv("MOJIAN_TEST_LEGACY_COUNT", filepath.Join(dir, "legacy-count"))
	i := &Install{dir: "/tmp/mojian", binPath: "/tmp/mojian/current/mojian-server", runtimeDir: "/tmp/runtime", sock: "/tmp/runtime/sessions.sock"}
	_, err := i.dial(context.Background(), "test-host")
	if !errors.Is(err, ErrNeedsRestart) {
		t.Fatalf("confirmed legacy server returned %v", err)
	}
}

func TestStopExportsRuntimeEnvironment(t *testing.T) {
	installShellSSH(t)
	dir := t.TempDir()
	binDir := filepath.Join(dir, "current")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "environment")
	t.Setenv("MOJIAN_TEST_STOP_ENV", marker)
	server := "#!/bin/sh\nprintf '%s\\n%s\\n' \"$MOJIAN_DIR\" \"$MOJIAN_RUNTIME_DIR\" > \"$MOJIAN_TEST_STOP_ENV\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "mojian-server"), []byte(server), 0o755); err != nil {
		t.Fatal(err)
	}
	i := &Install{dir: dir, binPath: filepath.Join(binDir, "mojian-server"), runtimeDir: "/tmp/runtime"}
	if err := i.stop(context.Background(), "test-host"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if want := dir + "\n/tmp/runtime\n"; string(got) != want {
		t.Fatalf("stop environment = %q, want %q", got, want)
	}
}

func TestStopServerReturnsPollingFailure(t *testing.T) {
	installFakeSSH(t, `case "$command" in
*mkdir*) printf '/home/test\nx86_64\n1000\n/tmp/runtime\n' ;;
*-stop*) exit 0 ;;
*"seq 50"*) printf 'socket polling failed' >&2; exit 1 ;;
*) exit 0 ;;
esac
`)
	err := StopServerContext(context.Background(), "test-host")
	if err == nil || !strings.Contains(err.Error(), "socket polling failed") {
		t.Fatalf("stop ignored its polling failure: %v", err)
	}
}

func TestConnectReapsSSHAfterNaturalExit(t *testing.T) {
	installFakeSSH(t, `case "$command" in
*stdio*) dd bs=1 count=1 >/dev/null 2>&1; `+framedHello(session.ProtocolVersion)+` ;;
esac
`)
	i := &Install{dir: "/tmp/mojian", binPath: "/tmp/mojian/current/mojian-server", runtimeDir: "/tmp/runtime"}
	rc, err := i.connect(context.Background(), "test-host")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-rc.sshDone:
	case <-time.After(3 * time.Second):
		t.Fatal("the exited ssh process was not reaped")
	}
	Close(rc)
}

func TestCloseWaitsForSSHReaper(t *testing.T) {
	installFakeSSH(t, `case "$command" in
*stdio*) `+framedHello(session.ProtocolVersion)+`exec sleep 30 ;;
esac
`)
	i := &Install{dir: "/tmp/mojian", binPath: "/tmp/mojian/current/mojian-server", runtimeDir: "/tmp/runtime"}
	rc, err := i.connect(context.Background(), "test-host")
	if err != nil {
		t.Fatal(err)
	}
	Close(rc)
	select {
	case <-rc.sshDone:
	default:
		t.Fatal("Close returned before ssh was reaped")
	}
}

func TestUploadReleaseKeepsOldReleaseWithoutSocket(t *testing.T) {
	dir := installFakeSSH(t, "exec /bin/sh -c \"$command\"\n")
	// The release script targets Linux. These small shims let the same
	// behavior run on macOS, which has neither flock nor mv -T.
	if err := os.WriteFile(filepath.Join(dir, "flock"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mv := `#!/bin/sh
if [ "$1" = "-Tf" ]; then
	shift
	rm -f "$2"
	exec /bin/mv -f "$1" "$2"
fi
exec /bin/mv "$@"
`
	if err := os.WriteFile(filepath.Join(dir, "mv"), []byte(mv), 0o755); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	old := filepath.Join(base, "releases", "old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(old, "mojian-server")
	if err := os.WriteFile(marker, []byte("old server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/old", filepath.Join(base, "current")); err != nil {
		t.Fatal(err)
	}
	i := &Install{dir: base, sock: filepath.Join(base, "runtime", "sessions.sock")}
	if err := i.uploadRelease(context.Background(), "test-host", "new", []byte("new server"), []byte("new library")); err != nil {
		t.Fatal(err)
	}
	current, err := os.Readlink(filepath.Join(base, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if current != "releases/new" {
		t.Fatalf("current points to %q", current)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("old release was removed while its bridge could still start: %v", err)
	}
	if string(data) != "old server" {
		t.Fatalf("old release contents = %q", data)
	}
}
