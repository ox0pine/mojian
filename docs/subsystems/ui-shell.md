# UI shell and frame conventions

## Responsibilities

The window: one `app` state struct, the immediate-mode `view` that redraws the whole element tree
each frame, tabs and the binary split tree of panes, pane overlays and zoom, the separate Settings
window, the error banner and command dispatch from menu, palette and keyboard.

## Source of truth

- [`main.go`](../../main.go) — the command line and the `app.Run` entry;
  [`internal/app/startup.go`](../../internal/app/startup.go) parses the path a launch names, and
  [`internal/app/second_instance.go`](../../internal/app/second_instance.go) hands a later
  launch's command line to the window that is already open.
- [`internal/app/app.go`](../../internal/app/app.go) — `app`, `Run`, `open`, `initialStore`,
  `later`, `runPending`, `changed`, `scheduleLayoutSave`, `persistLayout`, `fail`.
- [`internal/app/view.go`](../../internal/app/view.go) — `(*app).view`, the window root:
  title bar, workspace switch, pane overlays and the error banner.
- [`internal/app/context_menu.go`](../../internal/app/context_menu.go) — `menuSurface`,
  `menuHeader`, `menuChoice`, `menuAction`, `menuNote`, `menuList`: the one look the three
  title-bar menus share.
- [`internal/app/window.go`](../../internal/app/window.go) — `(*app).invalidate`, window and tray
  lifecycle.
- [`internal/app/settings_host.go`](../../internal/app/settings_host.go) —
  `(*app).settingsWindowView`, the second root, which draws Settings through
  [`internal/app/settingsui`](../../internal/app/settingsui).
- [`internal/app/model.go`](../../internal/app/model.go) — `tab`, `pane`, the `node` split tree,
  directions, moves and split shares.
- [`internal/app/splitter.go`](../../internal/app/splitter.go) — draggable dividers and share math.
- [`internal/app/headless.go`](../../internal/app/headless.go) — the `Headless` view API that
  `cmd/harness` drives.
- [`internal/app/commands/`](../../internal/app/commands) and
  [`internal/app/palette.go`](../../internal/app/palette.go) — static command metadata, the
  per-window run registry and the command table used by menu, palette and browser shortcuts.
- [`internal/motion/`](../../internal/motion) — the spring transitions the view animates with.

## Runtime path

1. `main` validates the command line with `app.ParseStartup` and, when standard input is a
   terminal, `detachFromTerminal` re-runs the binary in its own session and exits the parent: the
   shell returns at once, and closing the terminal or pressing Ctrl+C does not end the window. A
   launch without a terminal (Finder, LaunchServices) stays in place, `mygo dev` keeps its child
   attached, and the detached copy appends stdout and stderr to `app.log` under `session.Dir()`.
   `app.Run` then takes the single-instance lock before anything else: a second `mojian path`
   finds the socket MyGo publishes for the app, sends its arguments and working directory, and
   exits 0. The running window parses them against the launch's own directory, brings its window
   forward, opens the project, and shows the named file, so a launch never opens a second app.
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
  are the examples.
- Bound values update after construction: `OnClick`/`OnShortcut` run after construction,
  `OnChange`/`OnSubmit` observe the rebuilt view.
- The harness replaces native behavior through `a.harness`; any action that needs a real window or
  the session server must be guarded by it, or the headless worker blocks.
- Motion uses `internal/motion`: critically damped springs for pages, tabs, the tree and panes, a
  short color curve for hover; MyGo drops movement when the system asks for less motion.
- Settings › Advanced › Shell command writes the `mojian` alias through `internal/shellcmd` into
  the startup file of the chosen shell, in one marked block. The row's Install, Update or Remove
  button reads `shellcmd.Inspect`, and the headless harness has no executable to install.
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
  reads as the raised tab above its popover.
- The three title-bar popovers share `internal/app/context_menu.go`: `menuSurface` (one width,
  inset and shadow for all of them), `menuHeader`, `menuChoice` (36 tall, with a leading check on
  the current entry), `menuAction`, `menuNote` and `menuList` (scrolls after nine rows). A menu
  adds its own rows only; a new menu takes the same pieces rather than a fourth set of metrics.
- Every list row in a window is one row: `internal/app/style.go` holds `rowH` (34), `rowPadX`
  (10), `rowGap` (10), `rowTitle` (13), `rowDetail` (12), aliased from `internal/app/widgets`,
  which draws the one file row the pickers use. Quick open, a blank pane's picker, the Explorer's
  search results, the Welcome page's recents, the folder pickers and the Git change list take
  those numbers; `sectionLabel`, `emptyLine` and `emptyState` draw their captions, empty lines and
  empty pages. Settings keeps its own denser scale inside its own window.
- The palette, quick open and the SSH picker are one sheet: `CommandDialog` (560 wide, pinned 78
  below the title bar, `k.ShadowDialog`) with `sheetHeader` (48-tall search row, magnifier, field,
  close button); their lists scroll after `sheetListH` (384) and say "no matches" with
  `emptyLine`. `theme.Colors.ShadowDialog` is the dialog shadow; `k.ShadowFocused` stays for
  popovers and floating overlays. Icon buttons come in four sizes, `iconBtnXs` (18) through
  `iconBtnLg` (30), rather than a pair per call site.
- `internal/app/theme_bridge.go` is the only file in `internal/app` that names `internal/theme`
  (besides `settings_host.go`); `archtest` enforces it.

## Verification

```sh
go test ./internal/app
go test ./internal/app -run '^TestGitPage$' -v
go run ./cmd/harness   # then drive the fixture over MCP; see AGENTS.md
```
