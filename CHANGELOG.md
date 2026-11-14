# Changelog

## 0.4.2

- The title bar shows the checked-out Git branch right of the project. Its
  menu switches to another branch or creates one (`⌘⇧B`, or View → Switch
  Branch…), locally and on remote projects. A switch reloads the project
  from the branch — Explorer tree, assets and every note without unsaved
  edits — and is refused, with the reason in the banner, while a note is
  dirty. Folders outside a repository show no chip.

## 0.4.1

- Releases now publish RPM packages for Fedora and RHEL alongside the
  `.deb` packages. RPM file names follow the other artifacts, without the
  package release number: `mojian-<version>-x86_64.rpm` and
  `mojian-<version>-aarch64.rpm` install with `dnf install`.
- The app creates `~/Documents/Mojian` when it is missing, so a fresh
  Linux install opens its own project instead of reporting that the
  folder was not found.

## 0.4.0

- MyGo moves to v0.9.0. A focused element now scrolls into view (with
  `FocusOptions.PreventScroll` where a jump would be wrong), scroll bars
  follow VS Code's styling and each axis can hide its indicators, and the
  updater offers a release published while the settings window is open.
  The session protocol and the libghostty library are unchanged; the
  embedded Linux servers are rebuilt so remote hosts get the new build on
  their next setup.
- Terminal panes take the upstream fixes: Control+I, Control+M and
  Control+[ send Tab, Enter and Esc, a URL stops at the first non-ASCII
  character, pasting prefers UTF-8 text, and a temporary read error no
  longer ends the program.
- Settings › About can notify you about a new release. Mojian checks
  GitHub once at launch, shows a system notification, and opens About
  when the notification is clicked; the same version is announced once,
  and the switch turns the launch check off. The page also links to
  `github.com/ox0pine/mojian`.

- The Explorer tree, the title-bar actions, and the Explorer and pane headers
  now use MyGo's native tree and toolbar widgets. The tree is one Tab stop
  with arrow keys and type-ahead; each toolbar moves with Left/Right and
  folds items that do not fit into an overflow menu. The Explorer, SSH
  picker and empty-pane filters are native search fields, so Escape clears
  them.

## 0.3.4

- On Windows the minimize, maximize and close buttons now line up with the
  icons at the right of the title bar: the caption band is as tall as the
  title bar Mojian draws, where the system's 32 point default left the
  buttons half a row higher. The Settings window gets the same band.

## 0.3.3

- `mojian path` while Mojian is running now opens in the window that is
  already open instead of starting a second app. The launch hands its
  command line and working directory to the running instance, which
  switches to the project, shows the named file, and comes to the front;
  a launch that names nothing, such as the Dock icon, only brings the
  window forward.

## 0.3.2

- The Git page no longer stalls when Git prints a file as text that is
  really binary, such as an archive. Git sent it as a single line of
  megabytes and shaping that line took seconds to minutes; a diff line is
  now cut at 2,000 characters, with `…` marking the cut.
- A diff line wider than the pane now wraps instead of being clipped at
  the edge, so long lines and unbroken tokens read in full without
  scrolling sideways. The line numbers stay on the first line and the
  added or removed tint covers every wrapped line.

## 0.3.1

- Switching focus between panes with Option+Command and the arrows no
  longer makes the workspace blink. The focus command ended a zoom on
  every press, and that path replayed every pane's entrance fade even
  when no pane was zoomed; closing a pane and adding a terminal took the
  same path. A tab without a zoom now keeps its cards as they are.

## 0.3.0

- Mojian now runs on Windows as a local-only client. Local projects use
  `fs.Direct`, Git uses the local Git installation, and terminals use a
  per-user named-pipe session server with ConPTY.
- Windows hides SSH, remote projects, and remote host controls. The
  Windows OpenSSH client may still be used manually inside a terminal,
  but Mojian does not manage remote projects there yet.
- Windows releases include amd64 and arm64 installers, update archives,
  manifests, and `install.ps1`.

## 0.2.21

- A project listing that cannot read its root now reports an error
  instead of looking like an empty project. Denying macOS access to a
  folder no longer wipes its Explorer tree while the files still exist;
  the error banner names the permission failure and the tree stays.

## 0.2.20

- The session server caches a project listing for five seconds. File
  writes, mkdir, rename, remove, copy and exec clear it, and the app's
  Refresh button asks for a fresh walk, so a repeated listing (opening the
  sidebar, regaining focus) costs a round trip instead of walking the
  whole project again.
- Opening the file sidebar reuses the listing from the last 15 seconds,
  so it no longer starts another remote listing each time; the Refresh
  button still forces one.

## 0.2.19

- Refreshing a server project no longer blocks the window: the listing is
  read in the background and applied when it arrives, with a spinner in
  the Explorer header. Gaining window focus reuses a listing younger than
  15 seconds; the Refresh button still forces one.
- Listing a project skips data and model files (`.mat`, `.pt`, `.npy`,
  `.h5`, `.safetensors`, …) without opening them, and treats an unknown
  extension over 4 MB as data instead of reading its head. A project with
  20,000 data files now lists in about 45 ms instead of reading every
  file; the embedded Linux servers are rebuilt for the change.

## 0.2.18

- A finished connection no longer loses its SSH forward when the loading
  card cancels its task context. The forward now outlives the task, so a
  project stays connected after opening instead of dropping immediately
  and making its terminal show Try Again or reconnect forever. Cancelling
  a dial still in progress kills its forward.

## 0.2.17

- Closing the window while a server connection task is still running now
  cancels it, so reopening the window no longer shows a Reconnecting or
  Opening card that never clears.

## 0.2.16

- Mojian checks every configured host's server in the background when it
  launches, before the saved project reconnects, so a server left running
  is known without opening Settings and the project connects against the
  real state. Dial also checks the socket first and reuses a running
  server instead of starting a second process; a stale socket falls back
  to starting it and forwarding again.

## 0.2.15

- Settings › Hosts asks each configured host once whether its server is
  still running. A server left alive by the previous launch now shows
  **Stop Server** instead of **Set Up Server**; the row reads "Checking
  server…" while the SSH probe runs.
- A restored terminal whose project is still reconnecting waits for that
  connection and attaches afterwards, instead of showing Try Again. If
  the reconnect fails, its Try Again shows the connection error.

## 0.2.14

- An empty project's Explorer prompt — the file-plus icon, "No files
  here" and the New File button — now sits in the middle of the sidebar
  under the search field instead of hugging its top edge.
- The window-wide connection card has a Cancel button on its right: it
  kills the SSH commands the attempt is waiting on, closes the card at
  once, and leaves the project that was on screen untouched. A result
  the cancelled attempt still produces is dropped.
- Settings › Hosts now controls each server: **Stop Server** (after a
  confirmation that its terminals end) and **Start Server** bring the
  remote process down and back, and starting uploads a missing binary.
  The server survives Mojian and SSH disconnects and never stops on its
  own. The **Local** row controls this machine's server the same way;
  while it is stopped, opening a local project asks for it to be started
  again instead of spawning one behind the user's back.

## 0.2.13

- Choosing a server in the server chip or Settings › Hosts now returns to
  the project still loaded on that host, tabs and terminals included,
  instead of showing the folder picker again after a switch to Local. The
  picker still opens when nothing on the host is loaded, or when the
  server is already on screen.

## 0.2.12

- The default Petal theme is rebuilt around one surface tint: porcelain
  with a breath of rose in the light, a deep neutral plum in the dark.
  The window no longer drifts from pink to yellow, or from plum to olive:
  its gradient, cards, text, accent and terminal colors now belong to one
  family, and a note's page is the card itself.
- A note's editor no longer paints the window's paper behind itself. Its
  background was left unset as transparent, which the editor resolves by
  filling it from the window theme, so the page showed the gradient's
  bottom stop instead of the pane card, and the default light window
  looked yellow. The editor is clear again and the card shows through.
- The command palette, quick open, the project, SSH and remote pickers,
  the Git page, the Settings rail and font list, and the window's icon
  and row buttons now use MyGo's list, sidebar, search-field and button
  bases: one Tab stop per list, consistent arrows and type-to-choose,
  hover and focus, and the roles screen readers expect.

## 0.2.11

- Settings › Advanced gains a Shell command row: choose zsh or bash and
  install the `mojian` alias in that shell's startup file, update it when
  the app has moved, or remove it. The alias lives in one marked block,
  so the rest of the file is left as it was; a symlinked startup file is
  written through to its target.

## 0.2.10

- `mojian .` from a terminal returns the prompt at once: the app re-runs
  itself in its own session after checking the command line, so closing
  the terminal or pressing Ctrl+C no longer ends the window or reaches
  its process group. A launch without a terminal, as from Finder or
  LaunchServices, stays in place, and `mygo dev` keeps its child attached
  because its live reload watches that child. The detached window has no
  terminal left for its errors, so its standard output and error go to
  `app.log` in the data directory.

## 0.2.9

- The Appearance page offers ten color themes: Petal, the look Mojian has
  always had, and Mist, Meadow, Dusk, Tide, Apricot, Aurora, Dune, Paper
  and Plain. Each colors the window, its cards, the editor's syntax and
  the terminals from one palette, the grid shows a thumbnail of every
  theme, and the shells and notes already open follow the choice without
  reopening. The three appearances are a compact control now, and a forced
  Light or Dark also switches the native window, so the traffic lights and
  the system panels follow the choice instead of staying on the Mac's.
- Each project in the project chip, local or remote, has a trash button on
  its right: it removes that project from the list and unloads it if it is
  open, saving first when it is the open project. A local close switches to
  the next recent project; a remote one returns to the last local project.
  The server's connection stays pooled after **Set Up Server** and while
  any project on it is loaded, so its folder picker opens without
  connecting again; the connection is released when the last project on
  that host is removed.
- Markdown previews draw the HTML blocks documents use for badges and
  linked pictures, which used to show as their source: links open in the
  browser, pictures come from beside the note or from the web, and a
  README's screenshot with a cache query (`?v=…`) loads again.
- PDF panes page through every page of a document instead of stopping
  after the first. A page is drawn when you turn to it, so a long
  document opens at once and holds one page image rather than a folder
  of them. macOS draws with PDFKit when Poppler is not installed, and a
  Homebrew `pdftoppm` beside the app is found when Mojian starts from
  Finder.

## 0.2.8

- The Open Folder picker lists folders reached through symbolic links, and
  its **Hidden folders** switch beside Cancel includes names that start
  with a dot. A project opened through a symlink is stored at its target,
  so its files are listed.
- The tab and pane defaults now follow Ghostty: `⌘D` and `⇧⌘D` split
  right and down, `⌥⌘` and the arrows move focus, `⇧⌘Enter` zooms a
  pane, `⌘W` closes a pane while `⌥⌘W` closes the tab, `⇧⌘[` and `⇧⌘]`
  switch tabs, and `⌘1` through `⌘9` go to a tab. Linux follows
  Ghostty's Control+Shift defaults for a new tab, splits and zoom, and
  keeps Control+Alt+W for closing a tab. Select Next and New Line Above
  move to `⌃⇧D` and `⌥⇧Enter` so those keys belong to the window. Left,
  up and Move Pane have no default keys; Settings lists them so they can
  be assigned. Open File moves to `⌘P`, the tree to `⌘B`, the terminal
  to Control+backtick, Git to `⌃⇧G` and SSH to `⇧⌘S`.
- Mojian accepts a path on the command line: `mojian .` opens the
  current folder, `mojian ~/git/abc` opens that project, and
  `mojian ~/git/abc/README.md` opens the file with its folder as the
  project. `mojian --help` prints the usage, and an unreadable path is
  reported on stderr instead of silently opening the last project.

## 0.2.7

- MyGo moves to v0.8.3. Markdown previews now read GitHub's alerts,
  mentions and issue references, an unfocused window fades its accent as
  native apps do, and a keyed list or settings row is built with its key
  already set, so its state stays with the row as the view rebuilds.
- Terminal panes take the upstream fixes: a program's synchronized output
  shows after a second even when it never ends it, CJK punctuation keeps
  its cells, Option-E sends Esc e, and a full-screen repaint costs less of
  the main thread.
- Remote hosts get the rebuilt server and the same terminal fixes on
  their next setup; the session protocol and the libghostty library are
  unchanged.

## 0.2.6

- Every tab keeps its close button visible, including the first and only
  tab. The unsaved dot joins it only while there are edits, and the tab
  grows by the dot's room then, so a clean short tab carries no blank
  space beside its title. Closing the only tab leaves the empty workspace
  as before.
- Split panes are resizable: drag the gap between two panes, or focus the
  divider and use the arrow keys; a double click splits evenly again.
  Each split keeps its share in `layout.json`, and a divider stops at a
  readable minimum as the window shrinks.
- Zooming a pane animates both ways: the panes the zoom hides fade over
  their old places and fade back in when the zoom ends, while the zoomed
  card, file or terminal, flies to and from the tab's box.
- Terminal panes no longer snap when the layout moves them: they follow
  the same matched geometry as files when a pane zooms, splits, moves or
  closes, settling their grid at each size on the way.

## 0.2.5

- The project model (`Store`, `Note`, `Asset` and the file tree) moved to
  `internal/project`, and the welcome/sample notes to `internal/welcome`,
  so `internal/app` is the window's composition root rather than the home
  of every domain package.
- The Explorer, Settings window, shared widgets, command registry and
  remote-host pool are separate layers: `internal/app/explorer`,
  `internal/app/settingsui`, `internal/app/widgets`,
  `internal/app/commands` and `internal/remotehost`.
- The headless harness owns its fixtures in `cmd/harness` and shares the
  welcome project files with the app tests; the extra test-only fixture
  shim is gone.
- No user-facing behavior changes; tests, vet and the web build pass.

## 0.2.4

- The Explorer slides in from the window's edge and fades back out when
  the file tree is hidden, and it crossfades when switching projects on
  the same spring that unfolds its folders.
- The README header carries an Awesome MyGo listing badge beside release,
  license and platform badges in the ink, paper and vermilion palette.
- A headless UI harness (`cmd/harness`) serves the real window and
  Settings views to an MCP client, so an agent can inspect, click, type
  into and screenshot the interface without a desktop window.
- The File menu no longer repeats Toggle Terminal, and View no longer
  repeats New SSH Terminal: each command keeps one menu entry.
- Settings › Advanced no longer repeats the settings file path in its
  About section.
- Under the hood, duplicated comments, dead fields and copied helpers
  are gone, and the direct and server file backends share their path
  handling.

## 0.2.3

- Settings › Updates reads the latest release manifest directly from the
  release download URL and retries GitHub's transient 5xx answers, so a
  rate-limited API or a brief Gateway Timeout no longer fails the check.
- The README opens with the real window and walks through the Explorer,
  Open File, panes, terminals, remote servers and Settings.

## 0.2.2

- Double-clicking a tab's name and typing a new one keeps the text on the
  original label's baseline instead of dropping a few pixels.
- Settings › Hosts no longer repeats the recent remote projects list; the
  project switcher still offers them.
- Every pane keeps its split, zoom and close buttons visible, focused or
  not. With more than one pane, their accessible labels name the pane.
- Switching between loaded projects no longer reopens or reconnects them:
  the picker keeps its connection while the chosen project opens, and a
  new connection updates every cached project on that host.
- The project and server menus drop in with a short spring, and their
  title-bar chips animate their hover face.

## 0.2.1

- Windows is no longer a supported target. The app builds and runs on
  macOS and Linux; the browser build keeps its in-memory editor.
- Settings › Hosts is the only host source. The SSH terminal picker lists
  the hosts configured there instead of scanning `~/.ssh/config`.
- Each host has a Set Up Server action: it uploads `mojian-server` and
  `libghostty-vt` to `~/.local/share/mojian` on the host, starts the
  server, checks its protocol, and reports the result in the host row.
  Tested over real SSH against Fedora 44.
- A cached remote project reconnects when its connection dropped instead
  of switching to a closed client. Terminals owned by that host are
  reattached when their pane is shown again.
- Clipboard cuts are one-shot: a cut moves files only while the system
  clipboard still holds the cut paths. Switching projects clears the cut;
  a remote project's clipboard is scoped to its host and folder.
- Slider settings are written once the drag settles instead of on every
  pointer frame.
- A failed update check no longer also says the app is up to date.
- The active project is stored as one workspace object instead of being
  copied field by field between the app and the cache.
- A project's files live behind one `fsBackend`: a direct folder, the
  session server, or the browser's memory. Listing, reads, writes, rename,
  copy, trash, assets and path guards no longer branch on `server` and
  `memory` at each call site.
- The remote connection pool owns every connection; cached workspaces
  keep only a host name. A dropped connection is re-established before a
  cached project switches, and its terminal panes reattach when shown.
- A restored terminal attaches off the window thread and shows a Try Again
  action when its server is unreachable.
- Command dialogs, filtered-list keys and file-picker rows share one
  implementation.
- Settings search builds its index once per shortcut change instead of on
  every frame; the found-page map only stores which pages matched.
- The Settings pages are data: `internal/settings` declares categories,
  rows, controls, ranges and search words, and the app renders the generic
  pages from that one table. Keyboard, Hosts, Advanced and Updates keep
  custom controls but their rows still come from the same table.
- Large files are split by domain: window/settings persistence/menu/panes,
  pane and welcome views, tree state and tree view, clipboard and tree
  drafts, remote connection/project/picker, Git state/parse/view, and
  note/store/file-creation pieces.
- The session protocol (version 3) carries a SHA-256 hash for every asset
  in a listing, and remote picture/PDF caches are keyed by that hash
  instead of size and timestamp. A file changed with the same size and
  mtime no longer serves a stale image.
- The remote connection pool releases a host when its last project,
  terminal or open picker goes, instead of holding every connection until
  the app quits.

## 0.2.0

- On macOS and Linux, local projects now open through the same session
  server as terminals: the Explorer, saves, file operations and Git all go
  through the server, while Finder reveal and the system Trash remain
  local OS integrations. Quitting Mojian detaches the terminals, so they
  keep running; reopening reattaches them. Windows keeps its direct-file
  editor.
- Remote servers are persistent. They no longer shut down when the app or
  SSH connection goes away, so remote terminals and files stay available
  for the next connection. Each terminal records the server that owns it,
  and `mojian-server -stop` is the explicit way to end a server.
- Use the reversed ink tile as the Finder and Dock app icon, so the paper
  `m` and vermilion cursor stay legible on light and dark Finder
  backgrounds. The paper tile remains the light brand icon.
- Add an icon-only Mojian mark to the macOS menu bar, like Docker's, with
  Show, Settings and Quit in its menu. Closing the last window leaves the
  menu bar item and the app running; the Dock icon is set from the same
  embedded artwork at launch.

## 0.1.6

- Removed the separate Fonts settings page; editor and terminal font
  families are still picked from their own pages, through the same
  searchable family list.
- Settings › Keyboard's shortcut filter now lines up with the shortcut
  rows and their key chips.
- The remote server's folder picker scrolls again, with room between
  hovered rows, and uses the same opaque panel, header and row spacing as
  the other pickers.
- Connecting to an SSH server runs away from the UI thread behind a
  loading card; reading a folder or opening its project no longer freezes
  the window.
- Opened projects stay loaded in a workspace cache: switching back
  restores their tabs, terminals, layout and Git page instead of reading
  the project again. SSH connections are shared by host and kept while
  any loaded project uses them.
- Settings › Hosts' add form now uses padded, visible address and name
  fields aligned with the section; the empty-server and About notes no
  longer touch the card edges.

## 0.1.5

- Settings is redesigned in the app's own visual language: a searchable
  rail finds every preference by its name, its words or its page, switches
  to that page, and highlights and scrolls to the match. The existing
  title bar, cards, rows, switches and type sizes are kept.
- Every preference is now editable and persisted. Line numbers, highlight
  active line, indent guides, auto-close brackets, scroll past the last
  line, cursor blink and the Git refresh switch used to edit a temporary,
  so their toggles did not stick; they now write the setting back.
- Keyboard shortcuts can be rebound in Settings › Keyboard: click a key,
  type the new one, reset it to the default, or unbind it with Backspace.
  A key taken from another command unbinds that command, the menus follow,
  and conflicts are reported. Overrides live in `settings.json`.
- Hosts can be edited and removed, not only added. Advanced reveals or
  opens `settings.json` and can reset preferences, keeping projects and
  servers.
- A picture, PDF or Markdown preview pane keeps its split, zoom and close
  buttons visible after another pane takes the focus; a preview has no
  editor focus to bring them back.
- Closing a pane or a terminal now mirrors opening: the pane leaves the
  layout at once so the survivor expands, while a copy of the card fades
  and slides back toward the edge it entered from over the old place.
  Focus moves to the next pane immediately, the closing copy takes no
  input, and the terminal's session ends when the animation rests.

## 0.1.4

- Remote servers keep their socket in `XDG_RUNTIME_DIR/mojian-<uid>` (or
  `/tmp/mojian-<uid>`), not under the home directory. Fedora's SELinux
  labels the latter `data_home_t`, and sshd refuses stream-local
  forwarding to it with `open failed`, so 0.1.3 could not reach a remote
  server on such hosts. Verified over real SSH against Fedora 44: server
  upload, tree listing, lazy reads, saves, file operations, remote Git, a
  terminal's process-group kill, and transient shutdown on disconnect.

## 0.1.3

- Remote projects now edit the host's files directly, like VS Code Remote:
  the Explorer lists names and kinds, a file is read when it opens, saves
  and file operations write straight back, and Git runs on the host. The
  local mirror, its manifest and the full re-upload on every file operation
  are gone; pictures and PDFs are fetched on demand.
- A remote terminal ends with its SSH connection: the remote server is
  transient and shuts down (taking its shells) shortly after the app
  disconnects. Closing a terminal now signals the whole foreground process
  group, so `npm run dev`, `ssh` and `vim` no longer survive the pane.
- The session protocol is version 2, checked with `hello` on connect; a
  stale local server is replaced, and a remote one is restarted with the
  current binary, compared by SHA-256 rather than file size. Session
  frames are bounded, remote writes keep file modes and fsync, and the
  remote archive op is gone.
- Git: rename diffs now pair the old and new paths instead of showing the
  file as new; an untracked file is sized before it is read; refreshing the
  page on focus no longer resets the diff scroll; `GIT_DIR` and friends are
  stripped from the environment.
- Motion runs on short, critically damped springs: page and tab switches
  and the tree reverse from where they are when the state changes again
  mid-flight, instead of restarting a curve. Tree rows glide when a folder
  opens or closes, and the folder's children fold instead of letting the
  rows below jump. Hover colors share one 120 ms curve.
- Opening a pane or a terminal fades and slides the new card in from the
  edge of its split, while the pane it splits glides to its new width; a
  pane the layout moves into another split, or back to full width when one
  closes, follows from where it showed. Terminals skip the size animation,
  so their grid is not reflowed at every width on the way.
- **Open File** now opens a file picker over the window instead of
  focusing the Explorer's search field. Every file of the project is
  listed, typing filters them, and Enter or a click opens one in the
  active tab. This fixes the welcome page's row, which did nothing visible
  when the Explorer was already open.

## 0.1.2

- Settings › Updates now has **Check for Updates**, **Install Update** and
  **Restart to Update**. The check reads the latest signed GitHub release;
  the app verifies the update before installing it, and the restart applies
  a staged or replaced version.
- Signed update archives and manifests are built and published with every
  release. The update signing public key is in `mygo.json`; the private key
  lives only in the repository's GitHub Actions secret.
- Windows releases now also include the NSIS installer beside the portable
  zip and the signed update archive.

## 0.1.1

- Fix **Open File** on the welcome page: it now focuses the Explorer's
  search field, including when the Explorer was already open, so typing
  filters files at once.
- Show shortcuts, reveal labels and the welcome note in the platform's
  own keys and words: `⌘`/`⌥` on macOS, `Ctrl`/`Alt` on Linux and Windows.
- Add a portable Windows 10/11 amd64 build with the editor, Explorer,
  panes, Git page and settings. Terminal, SSH and remote-project features
  are hidden because they need the Unix session server.
- Build and test macOS, Linux and Windows in the tag-driven release
  workflow. Linux now runs the test suite in CI too.
- Rewrite both READMEs with per-platform install steps, a support matrix
  and a cross-platform shortcut table.

## 0.1.0

First release.

- A quiet plain-text editor for macOS and Linux, drawn by MyGo's native UI:
  no webview, no cgo, one Go binary.
- Projects are ordinary folders. The in-window folder picker, recent
  projects and per-project layouts keep each workspace separate.
- Tabs hold a tile of panes. Splits open an empty pane with an inline file
  search; drag a pane onto another pane's edge to place it there, or move it
  with Option+Shift and the arrows.
- Syntax highlighting for the languages MyGo knows, line numbers for code,
  wrapped prose, and Markdown source or preview.
- Explorer with file operations, in-project search, and Material file icons.
- A real terminal pane backed by a persistent session server; quitting the
  app detaches from shells instead of ending them.
- Git working-tree page with status, per-file diff and change stats.
- An SSH project workflow that mirrors a folder from a host and runs its
  terminals and Git diff there.
- Images and PDFs in a contained viewer, Settings in their own window, light
  and dark palettes, and a command palette over every command.
- Linux builds as per-user tar.gz archives and Debian packages for amd64
  and arm64, beside the macOS disk images.
