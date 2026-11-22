# Git page

## Responsibilities

The working-tree page: reading status and diffs for the open project, parsing Git's output, the page
state and its view, working-tree staging/unstaging/discarding/committing, and the two runners that
reach Git (a local command or the project's session server). A second mode lists the repository's
commits and shows the patch of the one selected. It also reads, switches and creates the project's
branches and reloads open notes after a checkout.

## Source of truth

- [`internal/git/git.go`](../../internal/git/git.go) — `Runner`, `LocalRunner`, `Host`, `PageMode`,
  `Page` (`Seq`, `DiffSeq`, `HistorySeq`, `Loading`, `DiffLoad`, `HistoryLoad`, `Mode`, `Changes`,
  `Sel`, `Commits`, `CommitSel`, `Diff`), `StatusData`, `Toggle`, `Refresh`, `ShowHistory`,
  `ShowChanges`, `applyStatus`, `Select`, `loadDiff`, `applyDiff`, `loadHistory`, `applyHistory`,
  `selectCommit`.
- [`internal/git/parse.go`](../../internal/git/parse.go) — `repoRoot`, `ReadStatus`, `parseStatus`,
  `statusLetter`, `ReadDiff`, `untrackedDiff`, `runGit`, `ParseDiff`, `DiffStat`, `FileDiff`,
  `SplitDiff`, `toFilePatch` (a `FileDiff` as the viewer's `editor.FilePatch`), `gitHunkCounts`.
- [`internal/git/history.go`](../../internal/git/history.go) — `Commit`, `HistoryData`,
  `ReadHistory`, `parseHistory`, `ReadCommitDiff`.
- [`internal/git/branch_menu.go`](../../internal/git/branch_menu.go) — `ToggleBranchMenu`,
  `openBranchMenu`, `CloseBranchMenu`, `loadBranches`, `applyBranches`, `chooseBranch`,
  `makeBranch`, `branchMenuBody`, `branchMenuRow`, `newBranchBody`.
- [`internal/git/view.go`](../../internal/git/view.go) — `(*Page).View`: header, file list, diff
  pane and its empty/loading/error states.
- [`internal/git/gitenv/gitenv.go`](../../internal/git/gitenv/gitenv.go) — `Env`, the environment
  Git runs in.
- [`internal/git/branch.go`](../../internal/git/branch.go) — `Branch`, `BranchData`,
  `ReadBranches`, `SwitchBranch`, `CreateBranch`, `branchName`, `retryCheckout`.
- [`internal/app/git_bridge.go`](../../internal/app/git_bridge.go) — `*app`'s implementation of
  `git.Host`.
- [`internal/app/branch.go`](../../internal/app/branch.go) — `branchState`, `refreshBranch`,
  `openBranchPicker`, `branchChip`, `branchMenuBody`, `branchMenuRow`, `checkoutBranch`,
  `createBranchAt`, `branchRoot`, `runBranchChange`, `dirtyNotes`.
- [`internal/project/store.go`](../../internal/project/store.go) — `(*Store).Reload`, which replaces
  the text of every clean note with what the checkout left on disk.

## Runtime path

1. `Page.Toggle` opens the page and starts a refresh. `Refresh` calls `ReadStatus` through the
   `Runner` from `Host.GitRunner()` — `LocalRunner` for a direct project, the session server's
   `Exec` for a server-backed one — and stores the result with the sequence number it started at.
2. `applyStatus` drops a result whose sequence is stale, then parses the NUL-separated
   `git status --porcelain` output into `Change` values. `Select` starts `loadDiff`, which calls
   `ReadDiff` in the background and lands through `applyDiff`, guarded by `DiffSeq`.
3. The view reads `Page` and draws; it never starts work. `Host.Invalidate` asks the window for the
   next frame.
4. `branchChip` draws the branch right of the project chip. Its first frame queues
   `refreshBranch` through `a.later`, so the first paint never waits on Git; `ReadBranches` runs in
   the background through the same `Runner` and lands under `branch.seq`. The chip is absent while
   that read is pending, and for `BranchData.Root == ""`.
5. The menu is a `ui.MenuBase` menu anchored below that chip, not a centered dialog: the chip is
   the menu button, so Up/Down/Home/End, type-ahead, Escape/Tab and focus return come from MyGo.
   `branchMenuBody` draws it from the shared title-bar pieces in `internal/app/widgets/menu.go`:
   a `BRANCHES` header with the shortcut as a plain child, one 34-pixel menu item per branch —
   leading check and `PanelSel` fill on the current one — a `New Branch…` item under a divider,
   and `branchNote` for the reading, empty and failure lines. `New Branch…` keeps the menu open
   and swaps the list for a name field whose Enter runs `createBranchAt` with
   `BranchData.Current` named as the source. A click on a branch runs `checkoutBranch`. The same
   menu opens from the `switch-branch` command, whose banner reports the reason when there is no
   chip to anchor to.
6. `branchRoot` refuses the change while any note is dirty, since checkout would overwrite the file
   those edits belong to: the reason lands in the banner and in the menu's footer, and the menu
   stays open. Otherwise `runBranchChange` runs `git switch` (or `git checkout` for Git older than
   2.23) off the UI thread, then `(*Store).Reload` replaces the text of clean notes, `git.Page`
   refreshes if open, and the branch is read again. Branch reads and changes capture their starting
   workspace: their sequence checks and state writes never follow `a.workspace` to another project,
   and only a still-current workspace receives banners or follow-up refreshes.
7. `stashActiveWorkspace` and `activateWorkspace` bump `branch.seq`, so a read from the project
   that was on screen cannot land in the next one; activation reads the branch again, since a
   terminal may have moved it while the project was away. `branch.changing`/`changeSeq` keep a
   checkout exclusive across those activations even though read loading is reset; its completion
   clears only the originating workspace's operation.
8. `Page.ShowHistory` swaps the left list for the repository's commits. `ReadHistory` runs in the
   background under `HistorySeq` and lands through `applyHistory`; the selected commit's patch is
   `ReadCommitDiff`, landing through `applyDiff` under the same `DiffSeq` as a working-tree diff
   and split per file by `SplitDiff`. `ShowChanges` increments `HistorySeq`, clears history loading
   and refreshes the status, and `applyHistory` also requires History mode, so a late commit list
   cannot clear or replace a working-tree diff. One
   track holds `Changes`, `History` and the branch chip, as the title bar's track holds its own
   chips; the branch chip opens the page's menu under itself (`ToggleBranchMenu`), which
   lists the branches, checks out the one chosen through `Host.SwitchBranch`, and creates a typed
   one through `Host.CreateBranch`. The left header names the list it holds (`WORKING TREE` or
   `RECENT COMMITS`).

## Contracts and failure behavior

- `Change.Status` is one letter: `M` modified, `A` added, `D` deleted, `R` renamed, `U` untracked,
  `C` conflict. `statusLetter` maps `??` to `U` and any code containing `U`, plus `AA`/`DD`, to `C`.
- A change row is the window's list row: 34 tall, a 15-pixel file icon, 13-pixel name, 12-pixel
  directory and the status badge at 10. The panel's two headers are `headerH` tall, like the
  Explorer's and a pane's. The file list is 248 wide, narrowing to 190 below a 900-wide window to
  leave the diff useful room.
- Closing Changes or History through the shortcut, the page's close button or the active tab goes
  through `Host.CloseGit`; the app restores the active pane's editor or terminal focus before the
  workspace is rebuilt.
- A change row takes its double click from the list's `Submitted`, never from `DoubleClicked` read
  inside the row builder: that call applies the pending pointer input mid build and swallows the
  row's own click, so a single click changed nothing. `Clicked` inside the builder has the same
  effect.
- `Change.Path` stays relative to the repository root so Git commands keep working; `OldPath` keeps
  a rename's source so Git can pair it; `Display` is relative to the project for people.
- A project nested inside a repository reports changes with `Display` relative to the project and
  skips entries outside it. Every filesystem-derived pathspec passed to status, diff, log or show
  uses Git's `--literal-pathspecs`, so `[`, `*`, `?` and leading `:` remain filename characters.
- `repoRoot` removes only rev-parse's CRLF record terminator, not whitespace in the directory name;
  a repository root ending in a space therefore remains usable by status and branch reads.
- On an unborn HEAD, `ReadDiff` compares the working tree with Git's empty tree (hashed with
  `hash-object -t tree /dev/null`), not just the index. An `AM` file thus includes both its staged
  addition and subsequent unstaged lines.
- Before reading an untracked file, `untrackedDiff` asks Git for a raw no-index diff. Mode `120000`
  builds a symlink patch whose added line is the exact target text from `Runner.Readlink` — local
  `os.Readlink` or the host's `readlink` — so Windows' Git/Go path normalization cannot make it read
  the target file. Regular files keep the existing size and binary limits.
- `gitenv.Env` removes the variables that redirect Git at another repository, including
  `GIT_CONFIG_COUNT` and its `GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` pairs, so a hook or parent
  process cannot change what the page reads.
- Local Git output is bounded, but an overflow is an explicit error rather than a silently truncated
  status or patch. Remote `pwd -P` removes only its record newline, preserving a project root's
  trailing spaces and reporting a failed `pwd` instead of treating its output as a path.
- Failures render inside the page (`Page.Err`, `Page.DiffErr`) instead of the app's error banner; a
  stale result is dropped, never shown over a newer one.
- The diff pane is MyGo's `editor.DiffView` over one `editor.DiffState` per loaded diff, with
  word-level inline highlights (`DiffInlineWordAlt`) and a Unified/Split layout toggle (`Page.SplitDiff`). The states
  are built on the frame that first draws them (`buildDiffViews`, `newDiffView`), where the window's
  theme and font are known, and kept for that diff's generation (`diffGen`/`viewsGen`); a new diff
  clears them and starts at the top. `Host.EditorTheme` is the editor theme with the pane's card as
  its background, because the viewer mixes change tints against the background it is given.
- Working-tree mutations (`Stage`, `Unstage`, `Discard`, `CreateCommit`, `AmendCommit`, `Fetch`,
  `Pull`, `Push`, `Stash`, `StashPop` in `internal/git/actions.go`) and hunk-level patch operations
  (`StageHunk`, `UnstageHunk`, `DiscardHunk` in `internal/git/hunk.go`, using `StdinRunner` over
  `git apply` on both local and remote `session.Client.ExecStdin`) run through the active `Runner`
  off the UI thread, guarded by `ActionBusy` and `ActionSeq`. `Discard` requires a second
  confirmation click (`Confirm Discard`) and calls `Host.ReloadFiles` so open clean notes reflect
  the restored file; every completed mutation calls `Host.AfterGitMutation` and refreshes the page.
- `toFilePatch` turns a parsed `FileDiff` into the viewer's `editor.FilePatch`: hunk headers keep
  their counts and section text, a blank context line regains its space prefix, and a `\ No newline`
  line stays as it is. Every other line is cut at `diffLineRunes` (2000) with `…` after its prefix,
  because a binary file Git reads as text arrives as one line of megabytes and the viewer would
  shape it for minutes. Cutting changes no hunk's line counts, so numbering stays right. A file
  with no parsed hunks gets no `editor.DiffState`: Changes shows Git's original binary or mode-only
  section in the non-text state, and History renders that section under the file heading instead
  of an empty viewer.
- The working tree's file scrolls inside the viewer (`FitContent` false, no outer `ui.Scroll`). A
  commit's patch keeps the outer scroll and `p.DiffScroll`, one `FitContent` viewer per file under
  its heading; the 2500-line budget cuts whole hunks (`cutFileDiff`) and still ends with the
  "… diff truncated" note. The viewer brings selection, copy, find, go-to-line and its own
  scrollbar, and draws its gutter, so the page no longer draws rows, numbers or per-line colors.
- Editor panes in a Git repository asynchronously read each open file's `HEAD` text through
  `git.ReadHeadFile` (`internal/git/base.go`, cached per workspace in `internal/app/git_bridge.go`)
  and pass it to `editor.State.DiffBase` for gutter change markers and hunk navigation (`NextHunk`,
  `PreviousHunk`, `RevertHunk` commands in the palette and menu). When `prefs.EditorGitBlame` is
  enabled, moving the cursor asynchronously queries `git.ReadLineBlame` and renders a trailing
  inlay (`editor.State.SetInlays`) for the current line. Switching branches or mutating Git from
  the page invalidates the cache.
- `BranchData.Current` is the checked-out branch, or the short hash of a detached HEAD with
  `Detached` set; no branch is marked `Current` in that case. `Root` is empty outside a repository,
  which hides the chip; the command reports "This folder is not a Git repository." in the banner.
- `ReadBranches` lists `refs/heads` ordered by latest commit (`for-each-ref
  --sort=-committerdate`); the menu scrolls that list, it does not filter or paginate it.
- `(*Store).Reload` is not `Refresh`: a note with unsaved edits keeps its text, a clean one gets the
  branch's text through `editor.Buffer.Reset`, which keeps the buffer the open panes hold so they
  redraw without being rebuilt. A body the listing left out is read by its own `Read`. It ends in
  `Apply` with the fresh listing, so the entries, the assets and so the Explorer tree are the
  branch's: a file only the branch has appears, one it lacks leaves (a dirty note stays). It runs on
  the UI thread, as `Apply` does. A tab open on a file the branch does not have keeps its old text;
  saving it writes the file into the new branch, as with any file deleted outside the app.
- A status refresh that lands while the page shows history updates `Changes` but never replaces the
  commit's patch: the selected commit is what the pane shows until the list is read again or another
  commit is picked.
- The page's branch menu reads the branches again when it opens, so a branch added in a terminal
  appears without a refresh. It never opens the title bar's menu: its popover anchors to the page's
  chip and its state is the page's. A checkout is refused while a note holds unsaved edits, and the
  menu keeps the reason in its footer.
- A commit row keeps the file row's frame — 34 tall, 15-pixel icon, 13-pixel subject, 12-pixel
  second line — with `git-commit` in the icon column and the short hash and relative date under the
  subject; the author stays in the row's tooltip.
- Both lists space their rows 6 pixels apart (`List(...).Gap(6)`), so a chosen row is its own pill
  and a hovered row's fill never meets the accent of the chosen one.
- History lists at most `historyLimit` (100) commits that touched the project, newest first, from
  `git log -z` with `%h`, `%an`, `%cr` and `%s`. A repository without commits is an empty list, not
  a failure.
- `ReadCommitDiff` runs `git show --format= --patch <hash>` scoped to the project's prefix, with
  `-c core.quotePath=false` so a non-ASCII path is the path itself instead of C-quoted. A merge
  commit leaves no single patch; the page says so rather than showing an empty diff.
- `SplitDiff` gives each file of a patch its own heading. `new file mode`, `deleted file mode` and
  `rename from` set `A`, `D` and `R`; a binary or mode-only section names its file from the
  `diff --git` line because it has no `+++` header. For an unquoted line, the separator is accepted
  only when the old and new paths match, so a path containing ` b/` is not truncated. The complete
  no-hunk section is retained in `FileDiff.Note` for the non-text view.
- Branch names typed into the menu pass `git check-ref-format --branch` before `git switch -c`;
  empty names and names starting with `-` are refused before Git sees them.
- The switch is refused while any note is dirty and it writes no note itself: a refusal leaves HEAD
  and the working tree where they were. The command is absent for a memory-backed project (the
  browser build) and for a folder outside a repository.

## Verification

```sh
go test ./internal/git
go test ./internal/git -run 'History|SplitDiff' -v
go test ./internal/git -run 'Branch' -v
go test ./internal/app -run '^TestGitPage$' -v
go test ./internal/app -run '^TestGitHistoryPage$' -v
go test ./internal/app -run 'Branch' -v
```
