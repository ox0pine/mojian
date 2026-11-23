# Mojian · 墨间

> Words on paper. The tool steps back.

<p>
  <a href="https://awesomemygo.dev/apps/mojian"><img src="https://awesomemygo.dev/badge.svg" alt="Listed on Awesome MyGo" height="20"></a>
  <a href="https://mygo.run/zh/showcase#:~:text=Mojian"><img src="https://img.shields.io/badge/MyGo%20showcase-listed-D24A2B?style=flat&amp;labelColor=1F1B16" alt="Listed on the MyGo showcase" height="20"></a>
  <a href="https://github.com/ox0pine/mojian/releases"><img src="https://img.shields.io/github/v/release/ox0pine/mojian?style=flat&amp;labelColor=1F1B16&amp;color=D24A2B" alt="Latest release" height="20"></a>
  <a href="https://github.com/ox0pine/mojian/releases"><img src="https://img.shields.io/github/downloads/ox0pine/mojian/total?style=flat&amp;labelColor=1F1B16&amp;color=D24A2B" alt="Total downloads" height="20"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/ox0pine/mojian?style=flat&amp;labelColor=1F1B16&amp;color=1F1B16" alt="MIT license" height="20"></a>
</p>

Mojian is a native plain-text editor for ordinary folders. Edit files, preview images and PDFs, run persistent terminals, and inspect Git diffs in tiled workspaces. No database, account, webview, or cgo. Runs on macOS 13+, Linux, and Windows 10 1809+.

[Download the latest release](https://github.com/ox0pine/mojian/releases) · [简体中文](README.zh-CN.md) · [Build from source](#build-from-source)

![Mojian 0.7.0 showing Explorer Git status badges, Liquid Glass branch popover and title tracks, Go code with gutter diff markers, side-by-side Markdown live preview, and the interactive status bar](resources/screenshot.png?v=0.7.0)

## Features

- **Ordinary files and folders.** A project is a folder on disk. Nothing is imported, indexed, or synced, and edits stay in memory until you save—with a visual side-by-side diff resolver if a file changed externally before `⌘S`.
- **Tabs as workspaces.** Each tab contains a split tree of editors, side-by-side Markdown live previews, image and PDF viewers, and terminals, with drag-and-drop panes, tab reordering, and `⇧⌘T` Reopen Closed.
- **Persistent terminals × editor synergy.** Shells run in a detached session server and reattach when Mojian opens again. `⌘+Click` file paths (`file:line:col`) to split and jump beside the terminal, press `⌥⌘L` to send `@file:line-range` to a shell or CLI agent, search scrollback with `⌘F`, and jump to OSC 9/99/777 notifications with `⌘J`.
- **Zero-config lightweight LSP.** Auto-detects installed language servers (`gopls`, `tsserver`, `pyright`, `rust-analyzer`, `clangd`, `zls`, and more) locally and over SSH: completion, hover, go to definition, find references, document highlights, file and workspace symbols, in-memory rename, Quick Fix / Code Actions (`⌘.`), Format Document (`⌥⇧F`) & Format on Save, Problems panel, and navigation history.
- **Complete Git loop.** Unified and split diffs with word-level highlights, hunk-level Stage / Unstage / Discard, branch switcher (`⇧⌘B`), Commit / Stash / Sync, Explorer status badges (`M`/`A`/`U`/`D`) and dirty directory dots, editor gutter markers, and inline Git Blame.
- **Remote projects over SSH.** Files, real-time watching, LSP, Git, and persistent terminals run directly on the configured Linux host via a per-user `mojian-server` over standard `ssh`, with automatic reconnect.
- **Native UI with Liquid Glass in one Go binary.** [MyGo](https://mygo.egoist.dev) draws the interface with Metal on macOS, GTK 3 on Linux, and Direct3D 11 on Windows—with Liquid Glass floating surfaces and title-bar tracks, asymmetric spring motion, and ten hand-tuned light/dark themes. No Electron or JavaScript build.

## Projects and files

Press `Cmd+O` on macOS or `Ctrl+O` on Linux and Windows to open a folder. The project chip switches between recent projects; on macOS and Linux the server chip switches between Local and configured SSH hosts. Each project keeps its own tabs, panes, and recent files.

From a terminal, pass the project folder or a file:

```sh
mojian .                        # the current folder
mojian ~/git/abc                # a project folder
mojian ~/git/abc/README.md      # a file, with its folder as the project
```

The file opens in the focused pane of the project's saved layout. The app bundle's binary is `Mojian.app/Contents/MacOS/Mojian`: link it as `mojian` to use the name in these examples, and run `mojian --help` for the usage. On Windows, run the installed `Mojian.exe` directly, or add its folder to `PATH` to use `Mojian` in these examples.

A launch from a terminal detaches at once: the prompt returns, and closing the terminal or pressing Ctrl+C no longer ends the window. A launch without a terminal, such as from Finder, stays in place, and errors from a detached window go to `app.log` in Mojian's data folder.

To run `mojian` from zsh or bash, **Settings › Advanced › Shell command** installs an alias in `~/.zshrc` or the bash startup file; the same row updates or removes it.

The Open Folder picker lists folders reached through symbolic links, and its **Hidden folders** switch beside Cancel includes names that start with a dot.

On first launch, Mojian opens `~/Documents/Mojian` when no project is selected and leaves a welcome note if the folder is empty.

The Explorer shows the folder as it exists on disk:

- Toggle it with `Cmd+B` / `Ctrl+B`.
- See working-tree Git status badges (`M`, `A`, `U`, `D`) on files and dirty dots on parent folders.
- Filter the visible tree with its search field.
- Search every project file with **Open File**, `Cmd+P` / `Ctrl+P`, or search and replace across the project with `Shift+Cmd+F` / `Ctrl+Shift+F`.
- Create files and folders directly in the tree, or drag files from the OS onto the Explorer or any editor pane.
- Right-click to open, rename, duplicate, copy, cut, copy a path, reveal, or move an entry to trash.

Generated folders such as `.git`, `node_modules`, `dist`, and `build` stay hidden.

## Tabs, panes, and terminals

Press `Cmd+T` / `Ctrl+Shift+T` to open a tab, as in Ghostty, and `Shift+Cmd+T` / `Ctrl+Alt+Shift+T` to reopen a recently closed pane or tab. Split the focused pane right or down with `Cmd+D` / `Shift+Cmd+D` (`Ctrl+Shift+O` / `Ctrl+Shift+E` on Linux); left and up have no default keys, and can be assigned in **Settings › Keyboard**. A new pane opens with an inline file search, and panes with duplicate basenames automatically disambiguate their parent folder in the header.

Drag a pane by its header onto another pane's edge to move it, or drag tabs in the title bar to reorder them. Use `Option+Cmd` / `Ctrl+Alt` and the arrows to move focus, `Shift+Cmd+Enter` / `Ctrl+Shift+Enter` to zoom the focused pane, `Cmd+W` to close a pane, and `Option+Cmd+W` / `Ctrl+Alt+W` to close the tab.

Double-click a tab to rename it, double-click a pane header to zoom it, or right-click a tab or pane header for more actions.

Press `Ctrl` and the backtick key (`⌃` + backtick on macOS) to toggle a terminal at the project root. Terminals are panes, so they can be split, moved, zoomed, and closed like any other pane. On macOS, Linux and Windows, quitting Mojian leaves their shells alive and reopening attaches them again. `⌘+Click` (`Ctrl+Click`) any `path:line:col` in terminal output to jump directly to that line in an editor pane, press `⌥⌘L` (`Ctrl+Alt+L`) from an editor to send `@file:line-range` to the terminal, search terminal scrollback with `⌘F`, and press `⌘J` (`Ctrl+J`) to jump to panes signaling OSC 9/99/777 attention.

Press `Shift+Cmd+S` / `Ctrl+Shift+S` on macOS or Linux to open an SSH terminal to a host from **Settings › Hosts**.

## Writing and previewing

Code keeps line numbers and prose wraps. Syntax highlighting covers Python, JavaScript, TypeScript, Go, Rust, C and C++, Swift, Kotlin, Ruby, JSON, YAML, TOML, SQL, HTML, CSS, Markdown, and more. Mojian resolves hierarchical `.editorconfig` files automatically for indentation, line endings, trailing whitespace, and final newlines, and offers an optional quiet 24px interactive Status Bar (cursor position, indent override, line ending, language, LSP status) plus optional Vim modal editing (with automatic CJK IME suppression in Normal mode).

Zero-config LSP activates automatically when a language server is on `PATH` (locally or on a remote SSH host), providing completion, hover, inline type/parameter Inlay Hints (coexisting with inline Git Blame), Signature Help (`⇧⌘Space` or automatically inside call parentheses), go to definition, find references (`⇧F12`), same-symbol document highlights, file and workspace symbols (`⇧⌘O` / `⌥⌘O`) with live pane-header symbol breadcrumbs, in-memory rename (`⇧F2`), Quick Fix / Code Actions (`⌘.`), Format Document (`⌥⇧F`) and optional Format on Save, a Problems panel (`⇧⌘M`), code folding commands (`Fold` `⌥⌘[`, `Unfold` `⌥⌘]`, `Fold All`, `Unfold All`, `Fold Imports`), and back/forward navigation history. Right-clicking inside an editor opens a context menu with LSP, folding, and editing actions.

Optional Vim mode (in **Settings › Editor**) suppresses CJK IMEs in Normal/Visual modes and supports motions, text objects (`iw`/`i"`/`i(`/etc.), `.` repeat last edit, system clipboard registers (`"+y` / `"+p`), `*`/`#` current-word search, and Ex commands (`:w`, `:q`, `:wq`, `:sp`, `:vs`, `:e`, `:tabnew`, `:tabc`, `:<line>`).

Open a live side-by-side Markdown preview with `Shift+Cmd+V` / `Ctrl+Shift+V` (or the **Preview to the Side** button in the pane header), with synchronized scrolling and clickable GFM task checkboxes that update the source buffer in memory. Images, including SVG, open in a contained viewer. PDFs page through every page: drawn with Poppler's `pdftoppm` when it is installed, else with PDFKit on macOS.

Press `Cmd+S` / `Ctrl+S` to write every unsaved note atomically. If a file changed on disk while you were editing it, Mojian opens a visual side-by-side conflict diff resolver so you can keep yours, reload from disk, or review every hunk before overwriting. Unsaved work is also written when the window closes.

## Git, commands, and settings

### Git

Press `Ctrl+Shift+G` on both platforms to compare the working tree with `HEAD`. Switch between unified and split diffs with word-level highlights, Stage / Unstage / Discard individual hunks, switch or create branches (`⇧⌘B`), and Commit, Stash, or Sync without leaving the window. Editors also show live gutter diff markers and optional inline Git Blame.

### Command palette

Press `Shift+Cmd+P` / `Ctrl+Shift+P` to search commands, files, and tabs with fuzzy matching inside a translucent Liquid Glass overlay.

### Settings

Press `Cmd+,` / `Ctrl+,` to open the separate Settings window. Its pages are General, Appearance, Editor, Terminal, Keyboard, Hosts, Advanced, and About; shell, notification, Git and update options live inside those pages.

Search finds preferences by name or keyword, and `Cmd+F` focuses the search field. Mojian follows the system appearance by default; **View › Appearance** overrides it, and the Appearance page's theme grid picks one of ten color themes (Petal, Mist, Meadow, Dusk, Tide, Apricot, Aurora, Dune, Paper, Plain) for the window, Liquid Glass surfaces, the editor, and terminals.

## Remote projects

1. Add a host in **Settings › Hosts**.
2. Choose **Set Up Server** on its row. Mojian uploads `mojian-server` and `libghostty-vt` to `~/.local/share/mojian` and checks that the server answers.
3. Choose the host from the server chip and open a folder.

Setup requires no root access, listening port, or system service. Remote hosts must be Linux x86_64 or arm64 with GNU glibc; musl-based systems such as Alpine are not supported by the embedded server and terminal library.

The Explorer lists files on the host. Files are read when opened, and saves and file operations write straight back. Git also runs on the host. Once opened, a project stays loaded, so switching back restores its tabs and terminals without reading it again.

Each project in the project chip, local or remote, has a trash button on its right. It removes that project from the list and unloads it if it is open, saving first when it is the open project: a local close switches to the next recent project, and a remote one goes back to the last local project. The host's connection is kept after **Set Up Server** and while any project on it is loaded, so its folder picker opens without connecting again; it is released when the last project on that host is removed, and at quit.

The remote server and its terminals stay alive when Mojian or the SSH connection disconnects. Each terminal remembers the server that owns it. Run `mojian-server -stop` to end a server explicitly.

## Updates

Open **Settings › About › Software Update** to check the latest signed GitHub release:

- **Check for Updates** reports whether a newer version is available.
- **Install Update** downloads it and verifies its signature.
- **Restart to Update** applies a staged update and relaunches Mojian.

Self-updates require a writable installation, such as the macOS app bundle, the per-user Linux archive, or the Windows installer. A `.deb` or `.rpm` installed in `/opt` is updated by its package manager instead.

## Keyboard shortcuts

The labels in Mojian follow the platform. These are the default bindings; **Settings › Keyboard** can rebind, reset, or unbind them. The tab and pane defaults follow Ghostty: `⌘` for files, tabs and tools, `⌘⌥` for moving through panes, and `⌃` for the panels beside them.

| Action | macOS | Linux |
|---|---|---|
| Open project | `⌘O` | `Ctrl+O` |
| Open file | `⌘P` | `Ctrl+P` |
| New file | `⌘N` | `Ctrl+N` |
| New folder | `⇧⌘N` | `Ctrl+Shift+N` |
| Rename selected entry | `F2` | `F2` |
| Move selected entry to Trash | `⌘⌫` | `Ctrl+Backspace` |
| Save all | `⌘S` | `Ctrl+S` |
| New tab | `⌘T` | `Ctrl+Shift+T` |
| Reopen closed pane / tab | `⇧⌘T` | `Ctrl+Alt+Shift+T` |
| Close pane / tab | `⌘W` / `⌥⌘W` | `Ctrl+Shift+W` / `Ctrl+Alt+W` |
| Next / previous tab | `⇧⌘]` / `⇧⌘[` | `Ctrl+Tab` / `Ctrl+Shift+Tab` |
| Show tab 1-9 | `⌘1…⌘9` | `Alt+1…Alt+9` |
| Split right / down | `⌘D` / `⇧⌘D` | `Ctrl+Shift+O` / `Ctrl+Shift+E` |
| Split left / up | assign in Settings | assign in Settings |
| Focus neighboring pane | `⌥⌘` + arrows | `Ctrl+Alt` + arrows |
| Move focused pane | drag, or assign in Settings | drag, or assign in Settings |
| Zoom pane | `⇧⌘↩` | `Ctrl+Shift+Enter` |
| Markdown preview to the side | `⇧⌘V` | `Ctrl+Shift+V` |
| Search & replace in project | `⇧⌘F` | `Ctrl+Shift+F` |
| Go to symbol (file / workspace) | `⇧⌘O` / `⌥⌘O` | `Ctrl+Alt+O` / `Alt+Shift+O` |
| Find references / Rename symbol | `⇧F12` / `⇧F2` | `Shift+F12` / `Shift+F2` |
| Quick Fix / Code Actions | `⌘.` | `Ctrl+.` |
| Signature Help | `⇧⌘Space` | `Ctrl+Shift+Space` |
| Format document | `⌥⇧F` | `Alt+Shift+F` |
| Fold / Unfold | `⌥⌘[` / `⌥⌘]` | `Ctrl+Alt+[` / `Ctrl+Alt+]` |
| Show problems / Next problem | `⇧⌘M` / `F8` | `Ctrl+Shift+M` / `F8` |
| Go back / forward | `⌃-` / `⌃⇧-` | `Alt+Left` / `Alt+Right` |
| Toggle file tree | `⌘B` | `Ctrl+B` |
| Toggle terminal | `⌃` + backtick | `Ctrl` + backtick |
| Send reference to terminal | `⌥⌘L` | `Ctrl+Alt+L` |
| Jump to terminal attention | `⌘J` | `Ctrl+J` |
| SSH terminal | `⇧⌘S` | `Ctrl+Shift+S` |
| Git diff page / Switch branch | `⌃⇧G` / `⇧⌘B` | `Ctrl+Shift+G` / `Ctrl+Shift+B` |
| Command palette | `⇧⌘P` | `Ctrl+Shift+P` |
| Settings | `⌘,` | `Ctrl+,` |

Two Ghostty keys stay with the editor: `⌘[` / `⌘]` indent and outdent, and `⌘Enter` inserts a line below.

On Windows, `Cmd` bindings become `Ctrl`; **Settings › Keyboard** shows the active keys. Terminal shortcuts are available on macOS, Linux and Windows. SSH shortcuts are available on macOS and Linux only.

## Build from source

Mojian requires Go 1.27.3 or later. MyGo is pinned as a Go tool in `go.mod`, so the first `go tool mygo` command downloads the exact version.

```sh
git clone https://github.com/ox0pine/mojian.git
cd mojian

go tool mygo dev        # live-reloading development app
go test ./...           # unit and integration tests
go tool mygo vet .      # go vet plus MyGo's UI lifetime checks
```

Use `go tool mygo dev -web` to open the interface in a browser with files kept in memory.

### Headless UI harness

`cmd/harness` serves the real window and Settings views to an MCP client
through [mygo-mcp](https://github.com/auron-labs/mygo-mcp), so an agent can
inspect, click, type into and screenshot the interface without a desktop
window:

```sh
go run ./cmd/harness                 # http://127.0.0.1:8080/mcp
go run ./cmd/harness -addr 127.0.0.1:8090
```

Each run seeds a disposable project in the temp directory, and the fixtures
never open native windows, start terminals or touch your projects and
settings. The endpoint has no authentication, so keep it on loopback.

### Build an app bundle

```sh
go tool mygo build
```

It writes the app and its installers under `build/<os>-<arch>/`. After changing the session protocol or `cmd/mojian-server`, rebuild the embedded remote binaries:

```sh
go generate ./internal/remotebin
```

## Design

Mojian uses ink `#1F1B16`, paper `#FBF7EF`, and vermilion `#D24A2B`. Almost everything is ink on paper; only the cursor keeps the vermilion, the one voice the page raises.

## Credits

- Action icons: [Lucide](https://lucide.dev), ISC
- File icons: [Material Icon Theme](https://github.com/PKief/vscode-material-icon-theme), MIT
- Interface font: [JetBrains Mono](https://www.jetbrains.com/lp/mono/), OFL
- Wordmark: Literata, SIL OFL
- Terminals: libghostty-vt and [creack/pty](https://github.com/creack/pty), MIT
- Window and interaction references: [GoRex](https://github.com/egoist/gorex) and [Kero](https://kero.sh)
- Native UI: [MyGo](https://mygo.egoist.dev)

## License

MIT. See [`LICENSE`](LICENSE).
