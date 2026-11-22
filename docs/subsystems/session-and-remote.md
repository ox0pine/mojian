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
  `ConnectAt`, `Ready`, `Client`, `Stream`, `StartLSP`, `Watch`, `Stop`, and the file operations
  (`ListTree`, `Search`, `ReadFile`, `WriteFile`, `Mkdir`, `Rename`, `Remove`, `Copy`, `Exec`,
  `ExecStdin`, `Stat`).
- [`internal/session/lsp_unix.go`](../../internal/session/lsp_unix.go) and
  [`internal/session/watch.go`](../../internal/session/watch.go) — multiplexed streams on
  `mojian-server` for running remote LSP servers over stdio and pushing remote project file-change
  signals (`"changed\n"`) from a bounded directory stat fingerprint.
- [`internal/app/remote_reconnect.go`](../../internal/app/remote_reconnect.go) — automatic
  exponential-backoff SSH reconnect watching `<-rc.Client.Closed()` to restore loaded remote
  workspaces and detached terminals after network drops or wake-from-sleep.
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
- [`internal/remote/remotehost/`](../../internal/remote/remotehost) — `Service`: the host list,
  `~/.ssh/config` import, remembered remote projects and the pool of `*remote.Conn` keyed by host.
- [`internal/app/remote_project.go`](../../internal/app/remote_project.go) — opening, reconnecting
  and the loading task state.
- [`internal/app/remote_pool.go`](../../internal/app/remote_pool.go) — the app's view of the pool,
  host service and `internal/remote`.
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
2. A connection is framed and multiplexed (`ProtocolVersion` 6): a 9-byte header of payload
   length, type and stream, then the payload. One connection carries every control request and
   every terminal stream, so the transport only has to be a byte stream. Control frames are
   considered before every queued stream frame, so continuous output cannot starve a response. Each
   stream reserves one queue slot for its ordered close marker, which never waits behind a full
   control queue. Small responses use one frame; responses over 64 MiB use `frameResponsePart`
   chunks carrying their request id, offset
   and total size, with a 256 MiB reassembly limit. File responses preserve `os.ErrNotExist` in a
   response code, so a remote rename can distinguish a missing destination from a permission or
   transport failure. A slow terminal stream is closed at both ends
   without closing the control connection. A terminal pane asks `Client.Stream` for a stream id
   on the connection it already has, and the server answers on that stream before replaying the
   session's screen. Quitting the app closes the connection, not the PTY: reopening reattaches to
   the same session by id, and each layout terminal records `Server` (`local` or an SSH host) so
   it reattaches to the server that owns it.
3. A remote host is provisioned once: `remote.Provision` uploads the binary and viewer library
   from `internal/remotebin` as one content-hashed release under `~/.local/share/mojian/releases`,
   then atomically switches the `current` symlink. Concurrent devices cannot publish a partial
   binary/library pair; old releases stay while a server may still use them and are removed only
   by the cleanup after a confirmed Stop Server. Uploading never treats an absent socket as proof
   that an old release is unused: another device's bridge may still be between resolving `current`
   and starting its server. Provisioning runs `ssh -T <host> -- current/mojian-server stdio`. The bridge
   dials the server socket, starting a detached `mojian-server` when none runs, and copies stdio
   to it; the client speaks v6 through that pipe. `remote.Dial` does the same, `Provision` returns
   the live `Conn` it ended with, and the app keeps it in the pool. Nothing is forwarded and no
   local socket is created, which is why Windows needs only `ssh.exe`.
4. The remote server stays alive while any of its sessions exists, so a dropped SSH connection does
   not end its shells. It never stops on its own: Settings › Hosts offers **Start Server** and
   **Stop Server** for each configured host, and a stop asks before ending its terminals.
   `mojian-server -stop` ends it explicitly.

## Contracts and failure behavior

- A server of another build answers with a different version; the client returns
  `session.ProtocolError{Server, Client}` and the app does not attach. A local v4/v5 server is stopped
  with its framed shutdown, a pre-v4 server with its one-line JSON shutdown, and either is replaced
  only after its socket disappears. A remote one cannot
  be stopped safely behind the user's back (its terminals and agents would end), so `Dial` reports
  `remote.ErrNeedsRestart` and the host's row asks for Stop Server instead. This classification
  requires either a framed protocol error or a confirmed pre-v4 JSON hello; a timeout, overload or
  SSH failure with a surviving socket keeps its original error. Stop Server waits for the socket
  to disappear and reports polling or SSH failures instead of claiming success.
- One socket and one server per data directory: a client dials the socket first and spawns a server
  only when nothing answers, and `server.lock` (an exclusive `flock` or Windows `LockFileEx`) makes
  a server that loses the race exit instead of binding a second socket.
- Shutdown rejects new Create requests. If shutdown begins while a PTY is being created, that
  session either joins the final kill snapshot or is killed before Create returns an error; it can
  never register after the snapshot and outlive the stopped server. Sessions already detached by a
  Kill request stay in the same termination wait group. Final kills start in parallel, share the
  one-second interrupt grace period, and wait for actual process exit before `Serve` returns.
- A remote project root may be a symlink to a directory; tree listings resolve that root and return
  the target's entries. Copying rejects a source/destination overlap before creating anything,
  including a directory destination below the source through a symlinked parent. File, directory
  and broken symlinks are recreated with their original link text instead of being followed. Stat
  reports a broken symlink as an existing non-directory entry, so a rename cannot overwrite it as
  though its name were free.
- Windows local projects keep files and Git on `fs.Direct`; its local session server owns
  terminals only, so the local `exec` op is unavailable. Remote hosts work on Windows the same way
  as elsewhere, because the transport is `ssh.exe` over pipes: remote projects use the remote
  server for files, Git and terminals, and the host UI is shown. A missing `ssh` surfaces as
  "ssh was not found; install the OpenSSH client".
- Every device that reaches a host gets its own connection to the same detached server, so several
  apps can use one host at once. View › Attach Terminal Session… (`attach-terminal`,
  `internal/app/term_sheets.go`) lists live sessions on the active server (with title, working
  directory `SessionInfo.Dir` and age), attaches a chosen session into a pane (or focuses it if
  already open), and can kill orphan sessions.
- Terminal panes show the running program's live OSC title (`Term.Title()`) in the pane header when
  present, falling back to `Spec.title()`. When a non-focused terminal rings its bell, exits, or
  emits a desktop notification via OSC 9, OSC 99 (Kitty), or OSC 777 (`internal/app/term_osc.go`),
  it sets a quiet attention dot on both the pane header and its tab; View › Jump to Attention (`⌘J`
  / `Ctrl+J`) cycles unread attention terminals and clears the marker upon focus.
- Terminal × Editor synergy (`internal/app/term_link.go`): Command+Click (Ctrl+Click elsewhere) on
  printed file paths (`file.go:42:10`, `App.tsx(12,4)`, `README.md#L8`, `file://...`) resolves local
  and remote project paths and opens/splits the file beside the terminal at the exact line;
  `Send Reference to Terminal` (`⌥⌘L` / `Ctrl+Alt+L`) pastes `@rel/path:line[-end]` into the active
  tab's terminal (opening one if needed); and `⌘F` (macOS) / `Ctrl+Shift+F` (Linux/Windows) inside a
  terminal opens a floating scrollback find bar backed by MyGo's `Terminal.Search`.
- Remote connections are pooled by host and kept while a loaded project uses them, and after
  `Set Up Server` until the last project on that host is removed (`closeRemoteProject`); `CloseAll`
  ends them at quit. Choosing a server that is not the one on screen resumes its most recently
  opened project when that workspace is still cached, so switching to Local and back keeps the tabs,
  terminals and layout; only when nothing on the host is loaded does it open the folder picker, at
  once and without the window-wide loading card, using the home cached in `remoteHomes`. Choosing
  the server already on screen opens the picker, so another folder on it can be opened. A dead
  control channel is dropped by `remoteForHost` and dialed again on the next use. Replacing a pooled
  connection closes the previous owner, including when Set Up and a project open finish out of order.
- Settings › Hosts is the explicit host source; the app does not automatically scan `~/.ssh/config`,
  but Settings › Hosts offers an explicit **Import from ~/.ssh/config** button
  (`remotehost.ReadSSHConfig`) that lists concrete non-wildcard `Host` aliases not yet in the list
  for one-click import. At launch, and
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
  attached; a session already created for that stale result is killed. Every Create and stale Kill
  holds an independent counted connection lease until cleanup finishes, so closing the last project
  cannot close the client first. A duplicate connection made while another one entered the pool is
  closed instead of overwriting it. Closing panes finish their overlays and Kill requests before
  their clients close, and a stopped host leaves its panes stopped until an explicit Start Server.
- Host setup, stop and probe completions use the application main queue even after the last window
  closes. Closing the only Settings window invalidates those operations and leaves them retryable;
  a stale successful setup still closes its connection instead of leaking it. Editing or removing a
  host closes its old directory picker and invalidates queued directory results.
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
