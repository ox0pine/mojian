# Repository Guidelines

Mojian is a quiet text editor for plain files: Go and MyGo's native UI, no webview, no cgo, no
JavaScript build. One binary draws its own window; a tab is a workspace, a pane edits a file,
previews an image or PDF, or runs a terminal, and a project is just a folder. Version, bundle id and
the macOS minimum live in `mygo.json`; nothing here repeats them.

## Where to read first

`docs/subsystems/` holds the contracts. Change one of these areas, read its page first: the page is
the authority, and it changes in the same commit as the behavior.

| Changing | Read |
|---|---|
| window, tabs, panes, frame conventions | [ui-shell.md](docs/subsystems/ui-shell.md) |
| project store, `fs.Backend`, save path | [projects-and-storage.md](docs/subsystems/projects-and-storage.md) |
| terminals, session server, SSH hosts | [session-and-remote.md](docs/subsystems/session-and-remote.md) |
| the Git page | [git-page.md](docs/subsystems/git-page.md) |
| settings, layouts, workspace cache | [persistence-and-layout.md](docs/subsystems/persistence-and-layout.md) |
| colors and themes | [theme.md](docs/subsystems/theme.md) |

Naming, vocabulary, bridges, errors and formatting: [docs/conventions.md](docs/conventions.md).
Tests: [docs/testing.md](docs/testing.md). Driving the UI headlessly: [docs/harness.md](docs/harness.md).

## Where things live

| Path | Purpose |
|---|---|
| `main.go` | Entry point: `-server` runs the session server; otherwise it reads the optional folder or file argument, detaches from an interactive terminal, and calls `app.Run`. A launch while Mojian is already running hands its command line to that window and exits instead of opening a second app. |
| `cmd/mojian-server/` | The per-user server uploaded to SSH hosts. Never installed as a service. |
| `cmd/harness/` | Dev-only MCP server that renders the real views headlessly over disposable projects. |
| `docs/subsystems/` | Contract pages for the UI shell, storage, session/remote, Git, persistence and themes. |
| `internal/app/` | The window in one package: `app.go` state and `Run`, `view.go` root, `window.go` lifecycle, `model.go` split tree, `layout.go`, `store.go` openers, `menu.go`, `context_menu.go` (the title-bar menus' shared pieces), `style.go` (the one list row, section label and empty states), `sheet.go` (the search sheets' shared header), `palette.go`, `pane.go` card and drag/drop, `pane_actions.go`, `splitter.go`, `term.go`, `remote_project.go` and `remote_picker.go`, `settings_file.go`, `settings_host.go` and `settings_hosts.go`, `headless.go`, `git_bridge.go` and `branch.go`. `widgets_bridge.go` and `aliases.go` are the names this package gives lower ones. Its UI layers are subpackages: `explorer/`, `settingsui/`, `widgets/`, `commands/`. |
| `internal/project/` | `Store`, `Note`, `Asset` and `TreeNode`: listing, save, rename/copy/move, asset cache, editor buffers. |
| `internal/fs/` | Project storage: `Backend`, `Direct`, `Memory`, `Server`, path guards, atomic writes and cached-file names. |
| `internal/session/` | PTY server and client, wire protocol, remote file ops, spawn/lock, platform stubs. |
| `internal/git/` | Working-tree diff page and branches: status/diff reading, parsing, branch read and switch, view. |
| `internal/gitenv/` | The environment Git commands run in, without redirect variables. |
| `internal/theme/` | The ten color themes and their derivation, `Colors`, `Radius`, background painting, editor and terminal themes, font registration. |
| `internal/motion/` | `ui.ElementTransition` values (`Page`, `Tab`, `Tree`, `Row`, `Pane`, `Colors`), springs for interruptible changes. |
| `internal/icons/` | Lucide action icons and Material Icon Theme file icons, cached from embedded SVG. |
| `internal/assets/` | `embed.FS` of icons, brand marks and JetBrains Mono. |
| `internal/remotebin/` | Embedded Linux `mojian-server` binaries and libghostty `.so` files (generated). |
| `internal/remote/`, `internal/remotehost/` | SSH provisioning, binary upload and forwarding; the host list, remembered remote projects and the connection pool. |
| `internal/prefs/` | The one preference schema, defaults and range validation. |
| `internal/settings/` | Pure Settings page table: categories, rows, kinds, search words. |
| `internal/shellcmd/` | The `mojian` alias Settings installs in zsh or bash, in one marked block. |
| `internal/update/` | Release checks, signed install and the GitHub release source. |
| `internal/platform/` | OS key spelling, file-manager labels and terminal support. |
| `internal/filekind/` | Shared text/asset/skip-dir classification for the app and the session server. |
| `internal/welcome/` | Welcome and sample note text, in the platform's key spelling. |
| `internal/testutil/` | Fixtures shared by tests: `WriteVault`, `InitRepo`, `Main`. |
| `resources/` | `icon.png`, the app icon embedded at build. |

## Development Commands

```sh
go tool mygo dev          # "Mojian Dev", live reload; state in .mygo/dev
go tool mygo dev -web     # native UI in a browser (wasm); files live in memory
go test ./...             # whole suite, no window needed
go tool mygo vet .        # go vet + MyGo UI lifetime checker
go tool mygo build        # build/<os>-<arch>/Mojian.app and .dmg
go run ./cmd/harness      # long-running MCP server; real views, no window
CGO_ENABLED=0 go test ./...
go test ./internal/app -run '^TestGitPage$' -v
go test -race ./internal/session ./internal/app
go generate ./internal/remotebin  # rebuild embedded Linux servers (needed after server/protocol changes)
```

The Go version, the MyGo pin and `mygo` itself all live in `go.mod`; there is no global install, no
Makefile, no justfile, no Bun/Node. `go tool mygo migrate-ui .` previews UI API migrations for
MyGo version bumps (`-write` applies).

## Constraints

- Edits stay in memory until an explicit `⌘S`: nothing writes a buffer on a timer.
- Anything user-visible on disk goes through `fs.WriteFileAtomic`, never in place.
- No cgo anywhere, no linter or formatter config (`gofmt` and `go vet` are the standard), and no new
  test framework.
- Do not commit `/build/`, `/.mygo/`, the root `mojian` binary or `internal/remotebin` artifacts;
  `go generate ./internal/remotebin` produces those, so never hand-edit them, and after a MyGo
  upgrade copy the Linux `libghostty-vt-*.so` files with their SHA-256 checked against the pinned
  MyGo's `plugins/terminal/mygo-plugin.json`.
- MyGo is `v0.9.0`; check the pinned source at `$(go list -m -f '{{.Dir}}' github.com/egoist/mygo)`
  before using an API, especially `ui/doc.go` and the widget's implementation comments.
- There is no pull-request CI: `.github/workflows/release.yml` runs `go test ./...` on tag pushes, so
  verification is local. Run before yielding: `go test ./...` and `go tool mygo vet .`; for a visible
  change also `go tool mygo dev` with a screenshot or the running app, not just a green test.
- Windows is a local-only client: no SSH hosts, no remote projects and no Windows server binary.
- A release is a tag: the steps, artifacts and update-key handling are in [RELEASING.md](RELEASING.md).

## Decisions and traps

- No reducers, observables or state stores: every piece of app state is a plain field on `*app`.
- No testify, gomock, golden files or coverage thresholds; stdlib `testing` only.
- A test that sets env or binds a socket never calls `t.Parallel()`.
- Code that queues work with `a.later` needs an extra `tt.Frame()`, often two.
- `go tool mygo vet .` rejects stored UI build values and goroutine captures, so the UI lifetime
  checker is the reason a view cannot keep elements or start goroutines.
