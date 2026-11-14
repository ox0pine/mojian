# Git page

## Responsibilities

The working-tree page: reading status and diffs for the open project, parsing Git's output, the page
state and its view, and the two runners that reach Git (a local command or the project's session
server). It also reads, switches and creates the project's branches and reloads open notes after a
checkout.

## Source of truth

- [`internal/git/git.go`](../../internal/git/git.go) — `Runner`, `LocalRunner`, `Host`, `Page`
  (`Seq`, `DiffSeq`, `Loading`, `DiffLoad`, `Changes`, `Sel`, `Diff`), `StatusData`, `Toggle`,
  `Refresh`, `applyStatus`, `Select`, `loadDiff`, `applyDiff`.
- [`internal/git/parse.go`](../../internal/git/parse.go) — `ReadStatus`, `parseStatus`,
  `statusLetter`, `ReadDiff`, `untrackedDiff`, `runGit`, `ParseDiff`, `DiffStat`.
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
5. The menu is a popover anchored to that chip, not a centered dialog: `branchMenuBody` lists
   `BranchData.Branches` with the current one checked, and its `New Branch…` row swaps the list for
   a name field whose Enter runs `createBranchAt` with `BranchData.Current` named as the source. A
   click on a branch runs `checkoutBranch`. The same menu opens from the `switch-branch` command,
   whose banner reports the reason when there is no chip to anchor to.
6. `branchRoot` refuses the change while any note is dirty, since checkout would overwrite the file
   those edits belong to: the reason lands in the banner and in the menu's footer, and the menu
   stays open. Otherwise `runBranchChange` runs `git switch` (or `git checkout` for Git older than
   2.23) off the UI thread, then `(*Store).Reload` replaces the text of clean notes, `git.Page`
   refreshes if open, and the branch is read again.
7. `stashActiveWorkspace` and `activateWorkspace` bump `branch.seq`, so a read from the project
   that was on screen cannot land in the next one; activation reads the branch again, since a
   terminal may have moved it while the project was away.

## Contracts and failure behavior

- `Change.Status` is one letter: `M` modified, `A` added, `D` deleted, `R` renamed, `U` untracked,
  `C` conflict. `statusLetter` maps `??` to `U` and any code containing `U`, plus `AA`/`DD`, to `C`.
- `Change.Path` stays relative to the repository root so Git commands keep working; `OldPath` keeps
  a rename's source so Git can pair it; `Display` is relative to the project for people.
- A project nested inside a repository reports changes with `Display` relative to the project and
  skips entries outside it.
- `gitenv.Env` removes the variables that redirect Git at another repository, so a hook or a parent
  process cannot change what the page reads.
- Failures render inside the page (`Page.Err`, `Page.DiffErr`) instead of the app's error banner; a
  stale result is dropped, never shown over a newer one.
- `diffLineText` draws at most `diffLineRunes` characters of one line and marks the cut with `…`. A
  binary file Git reads as text is a single line of megabytes, and MyGo shapes a line in time
  quadratic to its length, so the whole line stalled the frame for minutes.
- A diff row wraps at the pane's width and has no horizontal scroll: `LineHeight(1.5)` with
  `AlignItems(ui.Start)` so the line numbers sit on the first visual line, and the row's background
  covers every wrapped line. `diffLineText` keeps ordinary spaces, since a no-break space would stop
  the line from wrapping at all.
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
- Branch names typed into the menu pass `git check-ref-format --branch` before `git switch -c`;
  empty names and names starting with `-` are refused before Git sees them.
- The switch is refused while any note is dirty and it writes no note itself: a refusal leaves HEAD
  and the working tree where they were. The command is absent for a memory-backed project (the
  browser build) and for a folder outside a repository.

## Verification

```sh
go test ./internal/git
go test ./internal/git -run 'Branch' -v
go test ./internal/app -run '^TestGitPage$' -v
go test ./internal/app -run 'Branch' -v
```
