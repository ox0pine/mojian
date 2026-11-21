# Persistence, settings and layout

## Responsibilities

What survives a launch: the preferences and everything else in `settings.json`, the per-project
`layouts/<fnv64a>.json`, the workspace cache that keeps opened projects loaded, the debounced writes
that produce both files, and the environment overrides that move them.

## Source of truth

- [`internal/prefs/prefs.go`](../../internal/prefs/prefs.go) — `Prefs`, `Default`, `Sanitize`, the
  one preference schema, its defaults and range validation.
- [`internal/app/settings_file.go`](../../internal/app/settings_file.go) — `settingsFile`,
  `loadSettings`, `saveSettingsSoon`, `saveSettings`, and the preference getters.
- [`internal/app/layout.go`](../../internal/app/layout.go) — `layoutFile`, `layoutTab`,
  `layoutNode`, `layoutPane`, `layoutTerminal`, `layoutOf`, `snapshotLayout`, `restoreLayoutStatus`,
  `restoreColumns`, `saveLayout`.
- [`internal/app/app.go`](../../internal/app/app.go) — `dataDir`, `settingsPath`,
  `projectLayoutPath`, `migrateLayout`, `changed`, `scheduleLayoutSave`, `persistLayout`.
- [`internal/app/workspace.go`](../../internal/app/workspace.go) — `workspace`,
  `stashActiveWorkspace`, `activateWorkspace`, `closeWorkspaces`.
- [`internal/fs/write.go`](../../internal/fs/write.go) — `WriteFileAtomic`, the only writer both
  files go through.

## Runtime path

1. The data directory comes from `mygo.App.Path(mygo.PathUserData)`; `settingsPath` is
   `settings.json` inside it and each project's layout is `layouts/<fnv64a(projectDir)>.json`.
2. `loadSettings` starts from `prefs.Default()` and unmarshals over it, so a file written by an
   older version keeps sane values for fields it did not know, then calls `Sanitize`. A build
   without remote support clears `server`, `remotePath`, `remoteProjects` and `hosts`, so a
   settings file copied from a full build cannot start a remote connection. A missing or
   unreadable file leaves the defaults in place.
- A preference change calls `saveSettingsSoon`, which debounces the write so dragging a slider
  writes once; the write goes through `fs.WriteFileAtomic`.
- Settings rendering and search share platform availability: Option as Alt exists only on Darwin,
  Shell command and its Advanced metadata are absent on Windows, and an unavailable row or section
  cannot be found by search. About's Install row always exists; `installUpdateAction` derives its
  explanation and disabled state from the updater, so update searches always land on a real row.
4. A layout change calls `changed`, which marks the layout dirty and arms the timer;
   `persistLayout` saves once 550 ms pass without another change. `saveLayout` snapshots the tabs
   through `snapshotLayout`, writes atomically and returns any write error. A failed write leaves
   the layout dirty; synchronous saves propagate the error so project switches and normal quits do
   not silently discard the change.
5. Switching projects calls `stashActiveWorkspace` (the active workspace is the cache entry itself,
   so nothing is copied) and `activateWorkspace` restores the next project's tabs, terminals,
   layout and Git page without reading it again. Activation also issues a fresh editor focus
   request, because the previous request was consumed while that project was last visible.

## Contracts and failure behavior

- The layout JSON field names are compatibility surface: `tabs`, `active`, `name`, `root`, `focus`,
  `zoom`, `vertical`, `share`, `a`, `b`, `pane`, `id`, `note`, `asset`, `preview`, `terminal`,
  `sid`, `host`, `server`. Legacy `columns`/`panes` are still read through `restoreColumns` so old
  layouts survive.
- A missing layout is not a failure: the window falls back to the first note. A valid layout whose
  nonempty tabs all lose their panes does the same. A platform without terminals drops every
  terminal pane; a build without remote support drops SSH and remote-server panes but keeps local
  terminals. Empty tabs, including their custom names, are restored rather than treated as missing
  content. When earlier tabs are dropped, the active index follows the originally active tab; if
  that tab is gone, restoration selects the nearest following tab, then the nearest preceding one.
- `saveSettings` writes empty remote fields when the build has no remote support, so the sanitized
  state is durable rather than reappearing on the next launch.
- A failed read is not an empty layout. An unreadable or malformed file leaves the current state
  alone: opening the project neither creates a fallback tab nor writes over the original bytes.
- `MOJIAN_VAULT` selects the project folder; `MOJIAN_DIR` moves the data, socket and log directory
  (see [session-and-remote.md](session-and-remote.md)).
- Both files are written with `fs.WriteFileAtomic`; a partial file is never left behind.
- Switching projects bumps `git.Page.Seq`/`DiffSeq` so a refresh from the project being stashed
  cannot land in the one being activated.
- A project's row in the project chip carries a trash button: `closeLocalProject` or
  `closeRemoteProject` removes the entry from the recent/remembered list and unloads its
  workspace, saving it first when it is the open project. Closing the open local project switches
  to the next existing recent project or the empty workspace; closing the open remote project goes
  back to the last local project.

## Verification

```sh
go test ./internal/app -run 'TestSettingsPageAndPersistence|TestLayoutRoundTrip|TestRestoreLayoutRemapsActiveAfterDroppedTab|TestSaveNowReturnsLayoutErrorAndStaysDirty|TestOpenProjectPreservesMalformedLayout|TestTerminalLayoutRoundTrip|TestProjectSwitch'
```
