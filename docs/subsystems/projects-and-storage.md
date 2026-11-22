# Project store and filesystem backends

## Responsibilities

The open project: `Store`, `Note`, `Asset` and `TreeNode`; the `fs.Backend` implementations that
back them (`Direct`, `Memory`, `Server`); the difference between a local project opened through the
local session server and a remote project edited on its host; listing and `Refresh`, read-on-open,
the save path, path guards and the asset cache.

## Source of truth

- [`internal/project/`](../../internal/project) — `Store` (`New`, `Refresh`, `ByRelPath`, `Save`,
  `SaveAll`, `LoadNote`, `MaterializeAsset`, `BuildTree`, the rename/copy/move operations in
  `files.go`), `Note` with its shared `editor.Buffer`, `Asset`, `TreeNode`.
- [`internal/fs/fs.go`](../../internal/fs/fs.go) — the `Backend` interface, `Kind`, `cleanRel`,
  `joinRoot` and `relOfRoot`. Every path the store accepts goes through the guards.
- [`internal/fs/direct.go`](../../internal/fs/direct.go) — `Direct`, a plain folder on disk with an
  eager listing that reads text bodies; used by Windows local projects, tests and the browser build's neighbours.
- [`internal/fs/memory.go`](../../internal/fs/memory.go) — `Memory`, the in-memory editor of the
  browser build; every write returns `errReadOnly`.
- [`internal/fs/server.go`](../../internal/fs/server.go) — `Server`, a project reached through a
  `session.Client`, with the lazily cached asset files.
- [`internal/fs/write.go`](../../internal/fs/write.go) — `WriteFileAtomic`.
- [`internal/app/store.go`](../../internal/app/store.go) — `openStoreAt`, `openStore`,
  `openDefaultStore`, `defaultStoreDir`, `resolveProjectRoot`, `openServerStore`, `openLocalStore`,
  and local session server lifecycle (`startLocalServer`, `stopLocalServer`); `openLocalStore`
  opens a Windows project through `Direct` and macOS/Linux projects through the local session
  server.
- [`internal/app/project.go`](../../internal/app/project.go) and
  [`internal/app/pane_actions.go`](../../internal/app/pane_actions.go) — `openRel`, `openNote`.

## Runtime path

1. A tree row click reaches `(*app).openRel`, which asks `store.ByRelPath` for the note and calls
   `(*app).openNote`. The pane keeps a `*project.Note`; the pane renders `Note.Buffer()`, one
   `*editor.Buffer` shared by every pane showing that file.
2. `⌘S` (**Save All**) runs `(*app).saveNowAsync`. Local projects keep the synchronous
   `store.SaveAll` → `store.Save` path. For a remote project the UI thread serializes each dirty
   buffer into an immutable `SaveSnapshot`; a goroutine performs `ReadFile` and `WriteFile` through
   the context-aware session API, and the UI thread commits the result. Before writing, the store
   compares the current file's exact bytes with the snapshot's `Note.Saved` baseline. A mismatch
   returns `project.ErrFileChanged`; a read failure also aborts that note's save. Serialization
   uses `Buffer.WriteTo`, preserving LF or CRLF. A successful write advances `Saved`, but calls
   `buf.MarkSaved()` only when the current buffer still equals the snapshot; newer edits stay dirty
   and are written by another pass. A snapshot also carries the store's path revision. Rename,
   recreate and trash advance that revision before their filesystem operation; a stale snapshot is
   skipped rather than written to its old path, and any surviving dirty note is saved from a fresh
   snapshot at its current path. Every create, rename, copy, move and trash advances the revision.
   A per-store read/write lock covers the final revision check through the end of the write RPC. The
   slower baseline read happens before that lock, so a path change never waits behind a listing or
   read; it either waits for an actual write or makes it stale before it can touch an old path. A late
   write cannot overwrite a file recreated at that path. Saves are serialized per store, and
   switching projects, closing a host or closing the window waits for the queue or an explicit
   discard decision.
3. `Store.Refresh` reads one listing and reconciles it into notes, assets and the tree: notes keep
   their shared buffers; a direct listing reloads changed clean notes in place with `Buffer.Reset`,
   while dirty notes retain both their edits and saved baseline. Vanished clean files drop out;
   vanished dirty files stay available for recovery and are not silently recreated on save.
   Assets keep their cached copy. The read
   is split into `Store.List` (backend only, safe off the UI thread) and `Store.Apply` (mutates the
   store on the UI thread). The Explorer re-reads a server project in the background and shows a
   spinner while it runs; a window focus and opening the file sidebar reuse a listing younger than
   15 seconds, while the Refresh button forces one. A background listing also captures the path
   revision: if a rename, recreate or trash finishes first, the older listing is dropped instead of
   recreating ghost notes or closing editors for the new paths.

## Contracts and failure behavior

- Edits stay in memory until an explicit save. Nothing in the app writes a buffer on a timer.
- A save conflict leaves both disk and the dirty buffer intact and identifies the file in the
  existing error banner. Users must preserve their local edits separately before reloading.
  Content comparison detects changes even if size and modification time are unchanged. It is a
  pre-write check, not an inter-process lock: another writer can still race between the read and
  atomic replacement. Server-backed projects use the same check through the existing read/write
  protocol; their metadata-only listings do not automatically reload already loaded clean notes.
- `Note.Saved` holds exact on-disk bytes, including line endings. `Note.Text` remains normalized
  once an editor buffer exists; it must not be used to serialize a file for saving.
- A server project has no local mirror. `Server.List` names files with their size, time, kind and an
  asset's content hash; a note's text is read when it opens. `Direct` keeps the eager listing,
  `Server` the lazy one. Listing classifies by extension first: data and model files (`.mat`, `.pt`,
  `.npy`, `.h5`, …) are listed as binary without opening them, and an unknown extension over 4 MB is
  also binary rather than being read. Every regular file therefore appears in the Explorer and Quick
  Open; only text bodies and picture/PDF previews read file contents. The session server keeps each project's listing for five
  seconds; `WriteFile`, `Mkdir`, `Rename`, `Remove`, `Copy` and `Exec` clear it, and `ListFresh`
  bypasses it for the app's explicit Refresh. A repeated listing then costs a round trip instead of
  walking hundreds of thousands of files again.
- Classification stats the target before extension shortcuts or opening it. Only regular files can list as notes or
  assets; directories, FIFOs, sockets and other special files are omitted without being opened. A symlink follows its
  target, so a link to a regular file still classifies and opens normally while a link to a directory is omitted.
- Everything user-visible on disk that is not a project file (settings, layouts, cached assets) goes
  through `fs.WriteFileAtomic`; never write in place.
- Direct `os.WriteFile` outside these paths is a guard failure: `internal/archtest` allows it only in
  `internal/fs/server.go` (asset cache), `internal/welcome/welcome.go` (starter files) and
  `internal/testutil/testutil.go` (fixtures).
- A path that escapes the project root is rejected by `cleanRel`/`joinRoot` rather than clamped; the
  file operation surfaces the error to `a.fail`. Remote paste applies the same self/descendant guard
  as local copies before issuing `Copy`, so copying a folder into itself fails without creating a
  recursive destination tree.
- Server-backed case-only renames move the source to an unused temporary name before taking the new
  spelling. If a distinct destination remains on a case-sensitive host, the source is restored and
  the rename fails; on a case-insensitive host the destination alias disappears with the source and
  the new spelling replaces it, matching `Direct`. Both backends choose an unused temporary name,
  and a broken symlink still counts as an existing destination rather than free space.
- A listing that cannot read the project root (for example a denied macOS folder permission) returns
  that error instead of an empty listing, so the store and Explorer keep their previous tree. An
  unreadable folder below a readable root is skipped so the rest of the project stays usable.
- A listing hides the folders the project's `.gitignore` files exclude, read from the root down as
  the walk reaches them, so a generated tree the project ignores stays out of the Explorer while a
  tracked folder stays visible whatever it is named. Only folders are decided: a file the project
  ignores still lists and opens, so an ignored `.env` or log is not lost. `.git`, `.svn`, `.hg` and
  `.mojian-trash` are hidden by name. Patterns follow Git: `#` comments, `!` negation, a trailing
  `/` for folders, a `/` inside anchoring the pattern to the `.gitignore`'s folder, `**` crossing
  folders only in Git's special positions, and a deeper `.gitignore` overriding its parents. Escaped
  trailing spaces, POSIX classes and escaped `]` follow Git as well. Directory matching normalizes path separators and redundant
  `/` characters only; leading or trailing spaces in a real directory name are preserved, so only the exact Git match
  is skipped.
- The Explorer's search filters the same notes and assets that Quick Open lists: an image, PDF or
  binary match stays visible and opens through `openRel`, and Enter opens the first note or asset
  match. A binary pane shows its name, size and a not-editable-as-text explanation; a local file can
  open in its default app, while a remote binary is never downloaded just to show that metadata.
- Local desktop projects run a coarse recursive `mygo.WatchFiles` watcher (`HintsOnly`,
  debounced, skipping `.git`, `.gitignore`-excluded directories and `.mojian-*` atomic temp files
  in `internal/app/watch.go`). Remote SSH projects open a `session.Client.Watch` stream on
  `mojian-server` (`internal/session/watch.go`), which periodically computes a bounded stat
  fingerprint and emits `"changed\n"` notifications. When external tools or agents modify files on
  disk, the watcher reloads clean open notes, updates the Explorer tree and refreshes an open Git
  page while leaving dirty notes intact. The watcher stops on project switch, remote switch, or
  window close.
- `Memory` lists nothing and refuses file operations with `errReadOnly` (its `Write` is a no-op); the browser build keeps its editor in memory.

## Verification

```sh
go test ./internal/project

go test ./internal/app -run 'TestProjectSwitch|TestLocalProjectStoreUsesSessionServer|TestRemoteProjectReadsAndSavesOnTheServer'
go test ./internal/archtest -run TestDirectWritesStayInTheAllowlist
```
