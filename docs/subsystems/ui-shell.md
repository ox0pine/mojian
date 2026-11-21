# UI shell and frame conventions

## Responsibilities

The window: one `app` state struct, the immediate-mode `view` that redraws the whole element tree
each frame, tabs and the binary split tree of panes, pane overlays and zoom, the separate Settings
window, the error banner and command dispatch from menu, palette and keyboard.

## Source of truth

- [`main.go`](../../main.go) — the command line and the `app.Run` entry;
  [`internal/app/launch.go`](../../internal/app/launch.go) parses the path a launch names and hands
  a later launch's command line to the window that is already open.
- [`internal/app/app.go`](../../internal/app/app.go) — `app`, `Run`, `open`, `initialStore`,
  `later`, `runPending`, `changed`, `scheduleLayoutSave`, `persistLayout`, `fail`.
- [`internal/app/view.go`](../../internal/app/view.go) — `(*app).view`, the window root:
  title bar, workspace switch, pane overlays and the error banner.
- [`internal/app/widgets/`](../../internal/app/widgets) — shared list rows, `CommandDialog`,
  `SheetHeader`, `IconButton`, `SectionLabel`, `EmptyLine`, `EmptyState`, and the title-bar menu
  chrome (`MenuSurface`, `MenuHeader`, `MenuChoice`, `MenuAction`, `MenuNote`, `MenuList`),
  forwarded into `app` through [`internal/app/bridges.go`](../../internal/app/bridges.go).
- [`internal/app/preview/`](../../internal/app/preview) — Markdown image/HTML preview helpers and
  PDF page rendering.
- [`internal/app/window.go`](../../internal/app/window.go) — `(*app).invalidate`, window and tray
  lifecycle.
- [`internal/app/settings.go`](../../internal/app/settings.go) — settings persistence,
  preference getters, and `(*app).settingsWindowView`, the second root, which draws Settings
  through [`internal/app/settingsui`](../../internal/app/settingsui).
- [`internal/app/pane_tree.go`](../../internal/app/pane_tree.go) — `tab`, `pane`, the `node` split
  tree, directions, moves, draggable dividers and share math.
- [`internal/app/headless.go`](../../internal/app/headless.go) — the `Headless` view API that
  `cmd/harness` drives.
- [`internal/app/commands/`](../../internal/app/commands),
  [`internal/app/commands_host.go`](../../internal/app/commands_host.go) and
  [`internal/app/palette.go`](../../internal/app/palette.go) — static command metadata, accelerator
  formatting/parsing, the per-window run registry, and the command palette, quick open and browser
  shortcuts.
- [`internal/motion/`](../../internal/motion) — the spring transitions the view animates with.
- [`internal/detach/`](../../internal/detach) — `FromTerminal` leaves the terminal a launch came
  from: it re-runs the binary in its own session and appends the copy's output to `app.log`.

## Runtime path

1. `main` validates the command line with `app.ParseStartup` and, when standard input is a
   terminal, `detach.FromTerminal` re-runs the binary in its own session and exits the parent: the
   shell returns at once, and closing the terminal or pressing Ctrl+C does not end the window. A
   launch without a terminal (Finder, LaunchServices) stays in place, `mygo dev` keeps its child
   attached, and the detached copy appends stdout and stderr to `app.log` under `session.Dir()`.
   `app.Run` then takes the single-instance lock before anything else: a second `mojian path`
   finds the socket MyGo publishes for the app, sends its arguments and working directory, and
   exits 0. If that window is still opening, a pathless launch leaves the first launch's queued
   path untouched and only a newer path-bearing launch replaces it. Once open, the running window
   parses arguments against the launch's own directory, brings its window forward and opens the
   project. When leaving a remote project waits for an asynchronous save, the named file opens in a
   continuation only after the new project is active, never against the old remote store. If an
   older remote open is still busy, the newer launch cancels it rather than dropping the requested
   path, so a launch never opens a second app or silently loses its file.
2. MyGo delivers input, `Window.Update` or `Invalidate`, and calls `(*app).view` again. The view
   builds a new element tree every frame from the fields on `*app`; nothing is cached between
   frames.
3. An action mutates `app` directly. When the mutation would change a collection the current frame
   is still iterating (add or close a tab or pane), the action wraps it in `a.later(fn)`;
   `runPending` calls those closures once the frame is done.
4. `a.changed()` marks the layout dirty, records `lastChange` and arms a 700 ms `time.AfterFunc`.
   `persistLayout` writes through `saveLayout` only after 550 ms without another change; file
   contents are written only by an explicit save.
5. `a.invalidate()` requests a redraw of both windows after a state change made outside the view.
6. `a.fail(err)` stores the message in `a.err`, which the root view renders as the error banner.

## Contracts and failure behavior

- Mutate `app` only on the UI thread.
- Views start no goroutines and keep no elements or parts; store `ui.Handle`, app data and widget
  state on `app` instead.
- Stateful widgets need stable identity: `c.Key(key)` before construction, `.Bind(&handle)` /
  `.FocusBind(&field, value)` for persistent fields, `handle.Focused(c)` for committed focus.
- Slow work never runs in a view. Background results land through `Window.Update` and are dropped
  when a generation or sequence counter moved on — `git.Page.Seq`/`DiffSeq` and `assetView.pdfGen`
  are the examples. Remote saves and new-terminal SSH/`Create` calls also run off the UI thread.
  A normal window close or app quit prevents the close until its remote save finishes or the user
  explicitly discards the edits; a synchronous local save defers the quit retry on MyGo's
  application-level main queue until after `OnQuitCanceled`, so the cancellation cannot clear the
  completed save's `quitReady`, and the retry still runs after every window has closed. OS session
  ending keeps MyGo's synchronous best-effort save constraint.
  `windowSeq`, `hostSetupSeq` and `remoteTask.seq` reject results from a closed window, replaced
  operation or cancelled host, and production callbacks never mutate state directly on a worker
  after every window has closed.
- Bound values update after construction: `OnClick`/`OnShortcut` run after construction,
  `OnChange`/`OnSubmit` observe the rebuilt view.
- Explorer reveal requests leave search mode before selecting and opening the target path. Both
  file and folder rows handle `TreeReveal` with `ScrollIntoView`, so tab switches and tree mutations
  cannot leave the selected row filtered out or outside the viewport.
- The harness replaces native behavior through `a.harness`; any action that needs a real window or
  the session server must be guarded by it, or the headless worker blocks. Both `Headless.WindowView`
  and `Headless.SettingsView` drain the same pending callback queue before building a frame, so a
  Settings-only fixture displays background update results instead of leaving the action in progress.
- Show Tab 1–9 keeps its fixed keys when a custom command requests the same accelerator. Browser and
  harness views register the tab actions before configurable commands, so the key switches tabs and
  does not run the conflicting command. While Settings records a shortcut, every menu accelerator,
  including Show Tab, is temporarily empty so macOS delivers ⌘1–⌘9 to the recording control; ending
  recording restores the menu keys. Closing Settings stops recording before dropping its window
  handle, so the Settings-only lifetime restores them too.
- Motion uses `internal/motion`: critically damped springs for pages, tabs, the tree and panes, a
  short color curve for hover; MyGo drops movement when the system asks for less motion.
- Settings › Advanced › Shell command writes the `mojian` alias through `internal/shellcmd` into
  the startup file of the chosen shell, in one marked block. Both existing and dangling startup
  symlinks stay links: Install and Remove write the referenced target, creating it when necessary.
  The row's Install, Update or Remove button reads `shellcmd.Inspect`, and the headless harness has
  no executable to install.
- Update checks and release-asset downloads use MyGo's shared `netproxy.Transport()`, so proxy
  settings held only by the OS apply to the custom GitHub release source as well as MyGo's updater.
- The palette and the appearance are one decision made per frame: `(*app).dark` reads
  `prefs.Appearance` (System follows the frame's theme), `(*app).colorsOf` is
  `theme.Window(a.Theme, dark)`, and `(*app).applyTheme` hands the frame `theme.UI` of that
  palette. `(*app).editorTheme` is cached per theme and appearance pair and re-read by every pane
  each frame, so a theme switch re-colors notes that are already open.
- A forced appearance must reach the native side, which frame colors cannot do: `themeSource`
  maps the preference to `mygo.ThemeSource` and `applyAppearanceSource` calls
  `mygo.Theme.SetSource`, both at window creation (before `mygo.NewWindow`) and on every change.
- A window's own `BackgroundColor` comes from `(*app).windowBackground`, a `light-dark()` pair of
  the open theme, so the strip behind the traffic lights matches without a flash.
- A hidden title bar still has a caption band: both windows pass `TitleBarHeight: titleH`, the
  same 44 the views draw, so on Windows the minimize, maximize and close buttons fill that band
  and their glyphs center on the same line as the title bar's own buttons. With MyGo's 32
  default they sat half a row higher. The page keeps clear of them with `c.TitleBar().Right`.
- The title bar's left end is one track, as a tab strip: `serverChip`, `projectChip` and
  `branchChip` sit in a `k.Track` frame at `radius`, and `titleChip` gives each a `tabH`-tall face
  that turns `k.TabActive` with the active tab's shadows while its menu is open, so the open chip
  reads as the raised tab above its popover. The track and chips shrink before the active tab
  disappears: a chip keeps a 56-pixel minimum, the tab track keeps at least `tabMinW`, and an
  oversized active tab keeps its close action revealed. Below 900 pixels the branch becomes a
  56-pixel icon-only chip and the view toolbar keeps its first action plus overflow in 62 pixels,
  before the server and project lose their names. While the Git page is open its own branch chip is
  the only one; the title bar does not repeat it. A tab name of only whitespace resets to the
  focused pane's automatic name.
- Below 360 pixels a pane keeps its ellipsized file title; its `ui.Toolbar` is capped at 44 pixels
  and moves actions that no longer fit into the built-in overflow menu instead of discarding the
  pane's identity. Below 160 pixels the type icon yields too, the toolbar keeps only overflow in
  22 pixels, and the drag handle remains available.
- The three title-bar menus are `ui.MenuBase` menus, so the chip is the menu button and
  Up/Down/Home/End, type-ahead, Escape/Tab and focus return come from MyGo. Their shared look
  lives in `internal/app/widgets/menu.go` (forwarded by `internal/app/bridges.go`): `menuSurface`
  (one width, inset and shadow for all of them), `menuHeader`, `menuChoice` (34 tall, with a
  leading check on the current entry), `menuAction`, `menuNote` and `menuList` (scrolls after nine
  rows). Headers and notes are plain children between the items, which is why the menus keep their
  exact look; a menu adds its own rows only, and a new menu takes the same pieces rather than a
  fourth set of metrics.
- A pane's body is one of three kinds, chosen by `paneCardMode`: a note, an asset, or a terminal.
  Background agent tasks have no pane; their sheet is part of the window's modal stack.
- Every list row in a window is one row: `internal/app/widgets` holds `RowH` (34), `RowPadX`
  (10), `RowGap` (10), `RowTitle` (13), `RowDetail` (12), aliased into `app` in
  `internal/app/bridges.go`, and draws the one file row the pickers use. Quick open, a blank pane's
  picker, the Explorer's search results, the Welcome page's recents, the folder pickers and the Git
  change list take those numbers; `SectionLabel`, `EmptyLine` and `EmptyState` draw their captions,
  empty lines and empty pages. Settings keeps its own denser scale inside its own window.
- The palette, quick open, project search and the SSH picker are one sheet: `CommandDialog` (560 wide, pinned 78
  below the title bar, `k.ShadowDialog`) with `sheetHeader` (48-tall search row, magnifier, field,
  close button). Project search (`find-in-files`, `⇧⌘F` / `Ctrl+Shift+F`, `internal/app/search.go`)
  adds Match Case (`Aa`) and Regular Expression (`.*`) toggles, runs `project.Search` off the UI
  thread with dirty-buffer overrides, and jumps the focused pane directly to the matched line and
  span. The panel is at most 440 tall and leaves 24 below it; on shorter windows it uses
  the remaining height instead. `CommandListHeight` subtracts the 56-pixel sheet chrome, up to the
  original 384 list maximum, so the last row stays inside the window. Empty results use `emptyLine`.
  `theme.Colors.ShadowDialog` is the dialog shadow; `k.ShadowFocused` stays for popovers and floating
  overlays. Icon buttons come in four sizes, `iconBtnXs` (18) through `iconBtnLg` (30), rather than
  a pair per call site.
- `internal/app/bridges.go` is the only file in `internal/app` that names `internal/theme`
  (besides `settings.go`) and `internal/platform`; `archtest` enforces it.

## Verification

```sh
go test ./internal/app
go test ./internal/app -run '^TestGitPage$' -v
go run ./cmd/harness   # then drive the fixture over MCP; see AGENTS.md
```
