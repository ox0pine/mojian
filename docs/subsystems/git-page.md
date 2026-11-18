# Git page

## Responsibilities

The working-tree page: reading status and diffs for the open project, parsing Git's output, the page
state and its view, and the two runners that reach Git (a local command or the project's session
server). A second mode lists the repository's commits and shows the patch of the one selected. It
also reads, switches and creates the project's branches and reloads open notes after a checkout.

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
- [`internal/gitenv/gitenv.go`](../../internal/gitenv/gitenv.go) — `Env`, the environment Git runs
  in.
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
   `branchMenuBody` draws it from the shared title-bar pieces in `internal/app/context_menu.go`:
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
   refreshes if open, and the branch is read again.
7. `stashActiveWorkspace` and `activateWorkspace` bump `branch.seq`, so a read from the project
   that was on screen cannot land in the next one; activation reads the branch again, since a
   terminal may have moved it while the project was away.
8. `Page.ShowHistory` swaps the left list for the repository's commits. `ReadHistory` runs in the
   background under `HistorySeq` and lands through `applyHistory`; the selected commit's patch is
   `ReadCommitDiff`, landing through `applyDiff` under the same `DiffSeq` as a working-tree diff
   and split per file by `SplitDiff`. `ShowChanges` switches back and refreshes the status. One
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
  Explorer's and a pane's.
- A change row takes its double click from the list's `Submitted`, never from `DoubleClicked` read
  inside the row builder: that call applies the pending pointer input mid build and swallows the
  row's own click, so a single click changed nothing. `Clicked` inside the builder has the same
  effect.
- `Change.Path` stays relative to the repository root so Git commands keep working; `OldPath` keeps
  a rename's source so Git can pair it; `Display` is relative to the project for people.
- A project nested inside a repository reports changes with `Display` relative to the project and
  skips entries outside it.
- `gitenv.Env` removes the variables that redirect Git at another repository, so a hook or a parent
  process cannot change what the page reads.
- Failures render inside the page (`Page.Err`, `Page.DiffErr`) instead of the app's error banner; a
  stale result is dropped, never shown over a newer one.
- The diff pane is MyGo's `editor.DiffView` over one `editor.DiffState` per loaded diff. The states
  are built on the frame that first draws them (`buildDiffViews`, `newDiffView`), where the window's
  theme and font are known, and kept for that diff's generation (`diffGen`/`viewsGen`); a new diff
  clears them and starts at the top. `Host.EditorTheme` is the editor theme with the pane's card as
  its background, because the viewer mixes change tints against the background it is given.
- `toFilePatch` turns a parsed `FileDiff` into the viewer's `editor.FilePatch`: hunk headers keep
  their counts and section text, a blank context line regains its space prefix, and a `\ No newline`
  line stays as it is. Every other line is cut at `diffLineRunes` (2000) with `…` after its prefix,
  because a binary file Git reads as text arrives as one line of megabytes and the viewer would
  shape it for minutes. Cutting changes no hunk's line counts, so numbering stays right.
- The working tree's file scrolls inside the viewer (`FitContent` false, no outer `ui.Scroll`). A
  commit's patch keeps the outer scroll and `p.DiffScroll`, one `FitContent` viewer per file under
  its heading; the 2500-line budget cuts whole hunks (`cutFileDiff`) and still ends with the
  "… diff truncated" note. The viewer brings selection, copy, find, go-to-line and its own
  scrollbar, and draws its gutter, so the page no longer draws rows, numbers or per-line colors.
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
  `diff --git` line because it has no `+++` header.
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
