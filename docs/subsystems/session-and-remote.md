# Session server and remote hosts

## Responsibilities

The process that owns terminals and project files: the Unix-socket or Windows named-pipe PTY
server, its framed protocol, spawn/reattach/detach, the SSH provisioning of a remote server, the
stdio bridge that carries a remote connection, and the host list and connection pool the app
keeps. Windows runs both parts: its local server owns terminals, and it reaches SSH hosts through
its bundled `ssh.exe`.

## Source of truth

- [`internal/session/proto.go`](../../internal/session/proto.go) — the frames, the wire types,
  `ProtocolVersion`, `ProtocolError`, and the directory helpers `Dir`, `RuntimeDir`, `SocketPathIn`.
- [`internal/session/mux.go`](../../internal/session/mux.go) — `mux` and `muxStream`: one reader and
  one writer goroutine per connection, a control queue and one queue per terminal stream.
- [`internal/session/stdio_unix.go`](../../internal/session/stdio_unix.go) and
  [`internal/session/stdio_windows.go`](../../internal/session/stdio_windows.go) — `Bridge`, which
  turns stdio into the server's socket for an SSH client.
- [`internal/session/socket_unix.go`](../../internal/session/socket_unix.go) and
  [`internal/session/socket_windows.go`](../../internal/session/socket_windows.go) — `SocketPath`,
  listen/dial and socket cleanup for a Unix socket or a per-user named pipe.
- [`internal/session/server.go`](../../internal/session/server.go) — `Serve`, `serveConn` and
  `dispatch`, `session` and `attached` streams, `listTree`.
- [`internal/session/client.go`](../../internal/session/client.go) — `NewClient`, `Connect`,
  `ConnectAt`, `Ready`, `Client`, `Stream`, `Stop`, and the file operations (`ListTree`,
  `ReadFile`, `WriteFile`, `Mkdir`, `Rename`, `Remove`, `Copy`, `Exec`, `Stat`).
- [`internal/session/pty.go`](../../internal/session/pty.go), `pty_unix.go` and `pty_windows.go` —
  the process seam: `creack/pty` on macOS/Linux, a ConPTY implementation with a job object on
  Windows.
- [`internal/session/spawn.go`](../../internal/session/spawn.go),
  [`internal/session/spawn_unix.go`](../../internal/session/spawn_unix.go) and
  [`internal/session/spawn_windows.go`](../../internal/session/spawn_windows.go) — `Spawn`, which
  starts the server as a detached process of this binary, with a Unix `flock` or a Windows
  `LockFileEx` lock.
- [`internal/remote/remote.go`](../../internal/remote/remote.go) — `Dial`, `Provision`, `Install`
  (probe, upload, stdio connect, stop), `Conn`, `Close`, `ErrNeedsRestart`.
- [`internal/remotehost/`](../../internal/remotehost) — `Service`: the host list, remembered remote
  projects and the pool of `*remote.Conn` keyed by host.
- [`internal/app/remote_project.go`](../../internal/app/remote_project.go) — opening, reconnecting
  and the loading task state.
- [`internal/app/remote_pool.go`](../../internal/app/remote_pool.go) and
  [`internal/app/remote_bridge.go`](../../internal/app/remote_bridge.go) — the app's view of the
  pool and of `internal/remote`.
- [`cmd/mojian-server/main.go`](../../cmd/mojian-server/main.go) — the uploaded binary; `-stop`
  ends a remote server.
- [`internal/remotebin/`](../../internal/remotebin) — the embedded Linux server binaries and
  `libghostty` shared objects that provisioning uploads.

## Runtime path

1. The app re-executes itself with `-server` (`session.Spawn`) so the server is a child that
   detaches from the window. It keeps its socket, lock and log under `session.Dir()`: `MOJIAN_DIR`
   when set, otherwise `Mojian` inside `os.UserConfigDir()`. macOS and Linux use a Unix socket; a
   long path's socket moves to a hashed name through `SocketPathIn`. Windows uses a per-user named
   pipe (`\\.\pipe\mojian-<hash>`) protected by an owner-only SDDL. A remote server sets
   `MOJIAN_RUNTIME_DIR` to its own runtime directory, which the stdio bridge reads back.
2. A connection is framed and multiplexed (`ProtocolVersion` 5): a 9-byte header of payload
   length, type and stream, then the payload. One connection carries every control request and
   every terminal stream, so the transport only has to be a byte stream. Small responses use one
   frame; responses over 64 MiB use `frameResponsePart` chunks carrying their request id, offset
   and total size, with a 256 MiB reassembly limit. A slow terminal stream is closed at both ends
   without closing the control connection. A terminal pane asks `Client.Stream` for a stream id
   on the connection it already has, and the server answers on that stream before replaying the
   session's screen. Quitting the app closes the connection, not the PTY: reopening reattaches to
   the same session by id, and each layout terminal records `Server` (`local` or an SSH host) so
   it reattaches to the server that owns it.
3. A remote host is provisioned once: `remote.Provision` uploads the binary and viewer library
   from `internal/remotebin` as one content-hashed release under `~/.local/share/mojian/releases`,
   then atomically switches the `current` symlink. Concurrent devices cannot publish a partial
   binary/library pair; old releases stay while a server may still use them and are removed after
   a confirmed stop. Provisioning runs `ssh -T <host> -- current/mojian-server stdio`. The bridge
   dials the server socket, starting a detached `mojian-server` when none runs, and copies stdio
   to it; the client speaks v5 through that pipe. `remote.Dial` does the same, `Provision` returns
   the live `Conn` it ended with, and the app keeps it in the pool. Nothing is forwarded and no
   local socket is created, which is why Windows needs only `ssh.exe`.
4. The remote server stays alive while any of its sessions exists, so a dropped SSH connection does
   not end its shells. It never stops on its own: Settings › Hosts offers **Start Server** and
   **Stop Server** for each configured host, and a stop asks before ending its terminals.
   `mojian-server -stop` ends it explicitly.

## Contracts and failure behavior

- A server of another build answers with a different version; the client returns
  `session.ProtocolError{Server, Client}` and the app does not attach. A local server from an
  older build is stopped through the pre-v4 one-line shutdown and replaced. A remote one cannot
  be stopped safely behind the user's back (its terminals and agents would end), so `Dial` reports
  `remote.ErrNeedsRestart` and the host's row asks for Stop Server instead. This classification
  requires either a framed protocol error or a confirmed pre-v4 JSON hello; a timeout, overload or
  SSH failure with a surviving socket keeps its original error. Stop Server waits for the socket
  to disappear and reports polling or SSH failures instead of claiming success.
- One socket and one server per data directory: a client dials the socket first and spawns a server
  only when nothing answers, and `server.lock` (an exclusive `flock` or Windows `LockFileEx`) makes
  a server that loses the race exit instead of binding a second socket.
- Windows local projects keep files and Git on `fs.Direct`; its local session server owns
  terminals only, so the local `exec` op is unavailable. Remote hosts work on Windows the same way
  as elsewhere, because the transport is `ssh.exe` over pipes: remote projects use the remote
  server for files, Git and terminals, and the host UI is shown. A missing `ssh` surfaces as
  "ssh was not found; install the OpenSSH client".
- Every device that reaches a host gets its own connection to the same detached server, so several
  apps can use one host at once. Opening another device's terminal would need a session picker,
  which the app does not have; the protocol itself opens any session by id.
- Remote connections are pooled by host and kept while a loaded project uses them, and after
  `Set Up Server` until the last project on that host is removed (`closeRemoteProject`); `CloseAll`
  ends them at quit. Choosing a server that is not the one on screen resumes its most recently
  opened project when that workspace is still cached, so switching to Local and back keeps the tabs,
  terminals and layout; only when nothing on the host is loaded does it open the folder picker, at
  once and without the window-wide loading card, using the home cached in `remoteHomes`. Choosing
  the server already on screen opens the picker, so another folder on it can be opened. A dead
  control channel is dropped by `remoteForHost` and dialed again on the next use. Replacing a pooled
  connection closes the previous owner, including when Set Up and a project open finish out of order.
- Settings › Hosts is the only host source; the app does not scan `~/.ssh/config`. At launch, and
  when the page opens, the app asks each host once (`remote.ServerRunning`) whether its server
  socket still exists, so a server left running by the previous launch shows Stop Server rather
  than Set Up Server. Each host's row sets up, starts and stops that host's server; stopping
  removes the pooled connection and the terminal views it owned, and starting again reconnects the
  loaded projects on that host. While any setup or stop is active, every host's setup/start/stop
  action and final stop confirmation is disabled instead of silently rejecting the click. Status
  and error text wraps, truncated addresses keep full tooltips, and address-qualified accessible
  labels distinguish hosts that share a display name. Setup, stop and status checks capture
  `hostSetupSeq` and `windowSeq`; stale results neither clear a newer operation nor detach a new
  window's connection. Editing or removing a host cancels only that host's open task and migrates
  any surviving terminal to the edited address.
- Opening a remote project runs off the UI thread behind the loading card, so an unreachable host
  cannot block the window. The card's Cancel button cancels both the SSH connection attempt and the
  subsequent directory or project listing through the context-aware RPC. A result the cancelled
  task still produces is dropped, and a connection made only for that attempt is released; a shared
  pooled connection stays alive. The ssh child is deliberately not tied to that context once a
  connection is handed to the pool. Closing the window cancels a task that is still running, since
  its result would update a window that no longer exists; the next window starts fresh.
- New and split remote terminals also dial and create their sessions off the UI thread. Their pane
  enters the layout in a connecting state, and a result whose pane or window disappeared is not
  attached; a session already created for that stale result is killed. A duplicate connection made
  while another one entered the pool is closed instead of overwriting it.
- A restored terminal on the project on screen waits for the project's connection instead of
  showing Try Again; `installRemoteConn` clears the waiting state and the pane attaches. A failed
  reconnect puts the connection error behind Try Again.
- A terminal that rings its bell (`NotifyBell`) or ends (`NotifyExit`) notifies only while the
  window is not focused. The subtitle is the program's own title (OSC 0/2) or the pane's name, and
  the body is `exit N` for an ended shell plus the last line with text on the screen, so a shell or
  an agent that finished behind another tab says what happened.
- Missing or unreachable binaries surface as an error from `Provision`; the app reports it and keeps
  the project closed rather than editing a stale listing.

## Verification

```sh
go test ./internal/session
go test -race ./internal/session
go test ./internal/session -run TestWindowsNamedPipe -v   # Windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...     # cross-check
MOJIAN_REMOTE_TEST_HOST=user@host go test ./internal/app -run TestRemoteServerSetupFedora -v -timeout 10m
```
