package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpage "mojian/internal/git"
	"mojian/internal/testutil"
)

type gitWorkspaceActionRunner struct {
	gitpage.Runner
	afterDiscard func()
}

func (r *gitWorkspaceActionRunner) Run(dir string, args ...string) (string, error) {
	out, err := r.Runner.Run(dir, args...)
	if err == nil && r.afterDiscard != nil {
		for _, arg := range args {
			if arg != "checkout" {
				continue
			}
			fn := r.afterDiscard
			r.afterDiscard = nil
			fn()
			break
		}
	}
	return out, err
}

type gitWorkspaceActionHost struct {
	*app
	runner           gitpage.Runner
	reloadedProjects []string
	mutatedProjects  []string
}

func (h *gitWorkspaceActionHost) GitRunner() gitpage.Runner { return h.runner }

func (h *gitWorkspaceActionHost) ReloadFiles() {
	if h.store != nil {
		h.reloadedProjects = append(h.reloadedProjects, h.store.Dir)
	}
}

func (h *gitWorkspaceActionHost) AfterGitMutation() {
	if h.store != nil {
		h.mutatedProjects = append(h.mutatedProjects, h.store.Dir)
	}
}

func TestGitActionCompletionStaysWithCachedWorkspace(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	testutil.WriteVault(t, dirA, map[string]string{
		"a.md":    "A old\n",
		"keep.go": "package keep\n\nvar Value = \"old\"\n",
	})
	testutil.WriteVault(t, dirB, map[string]string{"b.md": "B old\n"})
	testutil.InitRepo(t, dirA)
	testutil.InitRepo(t, dirB)
	testutil.WriteVault(t, dirA, map[string]string{
		"a.md":    "A new\n",
		"keep.go": "package keep\n\nvar Value = \"new\"\n",
	})
	testutil.WriteVault(t, dirB, map[string]string{"b.md": "B new\n"})

	storeA, err := openStoreAt(dirA)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := openStoreAt(dirB)
	if err != nil {
		t.Fatal(err)
	}
	wsA := &workspace{store: storeA}
	wsB := &workspace{store: storeB}
	a := &app{workspace: wsA}
	runner := &gitWorkspaceActionRunner{Runner: gitpage.LocalRunner{}}
	host := &gitWorkspaceActionHost{app: a, runner: runner}
	pageA := &a.git
	pageA.Toggle(host)
	rootA := pageA.Root
	rootB := gitpage.ReadStatus(gitpage.LocalRunner{}, dirB).Root
	if rootA == "" || rootA == rootB || len(pageA.Changes) != 2 {
		t.Fatalf("initial A page: root=%q B root=%q changes=%+v", rootA, rootB, pageA.Changes)
	}
	if !strings.Contains(pageA.Diff, "+A new") {
		t.Fatalf("initial A diff: %q", pageA.Diff)
	}

	// The real discard finishes in A, then B becomes active before the
	// action's completion callback, deterministically reproducing a slow action.
	runner.afterDiscard = func() {
		a.stashActiveWorkspace()
		a.activateWorkspace(wsB)
	}
	actionSeq := pageA.ActionSeq + 1
	pageA.DiscardSelected()

	if a.workspace != wsB {
		t.Fatal("the action did not finish after activating B")
	}
	if pageA.ActionBusy || pageA.ActionErr != "" || pageA.ActionSeq != actionSeq {
		t.Fatalf("A action state: busy=%v err=%q seq=%d, want %d", pageA.ActionBusy, pageA.ActionErr, pageA.ActionSeq, actionSeq)
	}
	if len(host.reloadedProjects) != 0 || len(host.mutatedProjects) != 0 {
		t.Fatalf("A completion ran against active B: reloads=%v mutations=%v", host.reloadedProjects, host.mutatedProjects)
	}
	if !wsA.gitMutationPending || !wsA.gitReloadPending || wsB.gitMutationPending || wsB.gitReloadPending {
		t.Fatalf("deferred mutation state: A=(%v, %v) B=(%v, %v)", wsA.gitMutationPending, wsA.gitReloadPending, wsB.gitMutationPending, wsB.gitReloadPending)
	}
	if pageA.Root != rootA {
		t.Fatalf("B root landed in retained A page: got %q, want %q (B=%q)", pageA.Root, rootA, rootB)
	}
	for _, change := range pageA.Changes {
		if change.Display == "b.md" {
			t.Fatalf("B changes landed in retained A page: %+v", pageA.Changes)
		}
	}
	if strings.Contains(pageA.Diff, "B new") || strings.Contains(pageA.Diff, "b.md") {
		t.Fatalf("B diff landed in retained A page: %q", pageA.Diff)
	}
	body, err := os.ReadFile(filepath.Join(dirA, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "A old\n" {
		t.Fatalf("A discard was not preserved: %q", body)
	}

	a.stashActiveWorkspace()
	a.activateWorkspace(wsA)
	if pageA.Root != rootA {
		t.Fatalf("reactivated A root: got %q, want %q", pageA.Root, rootA)
	}
	if len(pageA.Changes) != 1 || pageA.Changes[0].Display != "keep.go" {
		t.Fatalf("reactivated A changes: %+v", pageA.Changes)
	}
	if !strings.Contains(pageA.Diff, `+var Value = "new"`) || strings.Contains(pageA.Diff, "B new") {
		t.Fatalf("reactivated A diff: %q", pageA.Diff)
	}
	if pageA.ActionBusy || pageA.ActionErr != "" || pageA.ActionSeq != actionSeq {
		t.Fatalf("reactivated A action state: busy=%v err=%q seq=%d, want %d", pageA.ActionBusy, pageA.ActionErr, pageA.ActionSeq, actionSeq)
	}
	if wsA.gitMutationPending || wsA.gitReloadPending {
		t.Fatal("reactivating A left its mutation follow-up pending")
	}
	noteA := storeA.ByRelPath("a.md")
	if noteA == nil || noteA.Text() != "A old\n" {
		t.Fatalf("reactivated A did not reload the discarded note: %+v", noteA)
	}
}
