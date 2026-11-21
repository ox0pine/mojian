package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mojian/internal/acp"
	"mojian/internal/fs"
	"mojian/internal/project"
)

func TestAgentNotificationBodyCondensesTheAnswer(t *testing.T) {
	long := strings.Repeat("word ", 100)
	body := agentNotificationBody(long, "do it")
	if r := []rune(body); len(r) != 160 || r[159] != '…' {
		t.Fatalf("body length = %d, want 160 ending in an ellipsis", len(r))
	}
	if body != strings.Join(strings.Fields(body), " ") {
		t.Errorf("body kept line breaks: %q", body)
	}
	if body := agentNotificationBody("  ", "do the thing"); body != "Finished: do the thing" {
		t.Errorf("empty answer body = %q", body)
	}
}

func TestOpenAgentTaskNeedsAProjectOnDisk(t *testing.T) {
	a := &app{workspace: &workspace{}}
	a.openAgentTask()
	if a.err == "" {
		t.Fatal("a missing project should be refused with a message")
	}
	if a.agentOpen {
		t.Error("the sheet opened without a project")
	}
}

func TestOpenAgentTaskShowsTheRunningTask(t *testing.T) {
	store := project.New(fs.NewDirect(t.TempDir()))
	a := &app{workspace: &workspace{store: store}, agentTask: &agentTask{prompt: "sort the notes"}}
	a.openAgentTask()
	if !a.agentOpen {
		t.Fatal("the sheet did not open for the running task")
	}
	if a.err != "" {
		t.Errorf("opening the running task reported %q", a.err)
	}
}

func TestStopAgentTaskClearsIt(t *testing.T) {
	task := &agentTask{prompt: "sort the notes"}
	a := &app{workspace: &workspace{}, agentTask: task, agentOpen: true}
	a.stopAgentTask()
	if a.agentTask != nil || a.agentOpen {
		t.Fatalf("task = %+v, open = %v after stop", a.agentTask, a.agentOpen)
	}
}

func TestAgentReplyCapsAt8KBWithoutResetting(t *testing.T) {
	task := &agentTask{prompt: "summarize"}
	a := &app{workspace: &workspace{}, agentTask: task, harness: true}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindAgentText, Text: "START " + strings.Repeat("x", 7994)})
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindAgentText, Text: "TAIL"})
	got := task.reply.String()
	if !strings.HasPrefix(got, "START ") {
		t.Fatalf("reply lost its beginning after reaching 8 KB: %q", got[:min(len(got), 20)])
	}
	if len(got) != 8000 {
		t.Fatalf("reply length = %d, want 8000", len(got))
	}
}

type testAgentSession struct {
	cancels int
	closes  int
}

func (s *testAgentSession) Cancel() { s.cancels++ }
func (s *testAgentSession) Close()  { s.closes++ }

type testAgentNotice struct {
	title string
	body  string
}

func TestAgentPermissionsAreAnsweredInRequestOrder(t *testing.T) {
	first := &acp.Permission{Title: "first"}
	second := &acp.Permission{Title: "second"}
	task := &agentTask{prompt: "edit"}
	a := &app{workspace: &workspace{}, agentTask: task, harness: true}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermission, Permission: first})
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermission, Permission: second})
	if task.perm != first || len(task.perms) != 1 || task.perms[0] != second {
		t.Fatalf("the second request replaced the first: active=%p queued=%+v", task.perm, task.perms)
	}
	a.answerAgentTask("allow-first")
	if task.perm != second || !a.agentOpen {
		t.Fatalf("answering the first request did not reveal the second: active=%p open=%v", task.perm, a.agentOpen)
	}
	// The first handler's resolution can arrive after the sheet advanced.
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermissionResolved, Permission: first})
	if task.perm != second {
		t.Fatal("a stale resolution cleared the next permission request")
	}
	a.answerAgentTask("allow-second")
	if task.perm != nil || len(task.perms) != 0 || a.agentOpen {
		t.Fatalf("permission state remained after both answers: active=%p queued=%d open=%v", task.perm, len(task.perms), a.agentOpen)
	}
}

func TestAgentPermissionCancellationClearsOnlyThatRequest(t *testing.T) {
	first := &acp.Permission{Title: "first"}
	second := &acp.Permission{Title: "second"}
	task := &agentTask{prompt: "edit"}
	a := &app{workspace: &workspace{}, agentTask: task, harness: true, agentOpen: true}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermission, Permission: first})
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermissionResolved, Permission: first})
	if task.perm != nil || a.agentOpen {
		t.Fatalf("cancelled request stayed visible: active=%p open=%v", task.perm, a.agentOpen)
	}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermission, Permission: second})
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermissionResolved, Permission: first})
	if task.perm != second || !a.agentOpen {
		t.Fatal("a stale cancelled request affected the next live request")
	}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermissionResolved, Permission: second})
	if task.perm != nil || a.agentOpen {
		t.Fatal("the last cancelled request did not close the permission sheet")
	}
}

func TestStopAgentTaskReleasesEveryPermission(t *testing.T) {
	session := &testAgentSession{}
	task := &agentTask{prompt: "edit", sess: session}
	a := &app{workspace: &workspace{}, agentTask: task, harness: true}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermission, Permission: &acp.Permission{Title: "first"}})
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindPermission, Permission: &acp.Permission{Title: "second"}})
	a.stopAgentTask()
	if a.agentTask != nil || task.perm != nil || task.perms != nil || a.agentOpen {
		t.Fatal("stop retained permission or task state")
	}
	if session.cancels != 1 || session.closes != 1 {
		t.Fatalf("session cleanup = %d cancels, %d closes; want one each", session.cancels, session.closes)
	}
}

func TestAgentTurnDoneCannotBeLostToClosed(t *testing.T) {
	for _, closedFirst := range []bool{true, false} {
		session := &testAgentSession{}
		var notices []testAgentNotice
		task := &agentTask{prompt: "edit", sess: session, notify: func(title, _, body string) {
			notices = append(notices, testAgentNotice{title: title, body: body})
		}}
		task.reply.WriteString("answer")
		a := &app{workspace: &workspace{}, agentTask: task}
		if closedFirst {
			a.agentTaskEvent(task, acp.Event{Kind: acp.KindClosed, Err: errors.New("process exited")})
			if a.agentTask != task || len(notices) != 0 {
				t.Fatal("Closed erased the pending prompt result")
			}
			a.agentTaskEvent(task, acp.Event{Kind: acp.KindTurnDone, Stop: "end_turn"})
		} else {
			a.agentTaskEvent(task, acp.Event{Kind: acp.KindTurnDone, Stop: "end_turn"})
			a.agentTaskEvent(task, acp.Event{Kind: acp.KindClosed, Err: errors.New("process exited")})
		}
		if len(notices) != 1 || notices[0].title != "Agent finished" || notices[0].body != "answer" {
			t.Fatalf("closedFirst=%v produced notices %+v", closedFirst, notices)
		}
		if a.agentTask != nil || session.closes != 1 {
			t.Fatalf("closedFirst=%v left task=%p or closes=%d", closedFirst, a.agentTask, session.closes)
		}
	}
}

func TestAgentClosedThenPromptFailureReportsError(t *testing.T) {
	session := &testAgentSession{}
	var notices []testAgentNotice
	task := &agentTask{prompt: "edit", sess: session, notify: func(title, _, body string) {
		notices = append(notices, testAgentNotice{title: title, body: body})
	}}
	a := &app{workspace: &workspace{}, agentTask: task}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindClosed})
	a.failAgentTask(task, errors.New("connection lost"))
	if len(notices) != 1 || notices[0].title != "Agent error" || notices[0].body != "connection lost" {
		t.Fatalf("crash notification = %+v", notices)
	}
	if a.agentTask != nil || session.closes != 1 {
		t.Fatal("the failed task was not cleaned up")
	}
}

func TestAgentTurnNotificationsRespectStopReason(t *testing.T) {
	cases := []struct {
		stop  string
		title string
		body  string
	}{
		{stop: "end_turn", title: "Agent finished", body: "Finished: do it"},
		{stop: "refusal", title: "Agent refused", body: "The agent refused the request."},
		{stop: "max_tokens", title: "Agent incomplete", body: "The agent reached its token limit."},
		{stop: "max_turn_requests", title: "Agent incomplete", body: "The agent reached its turn-request limit."},
		{stop: "cancelled", title: "Agent stopped", body: "Stopped: do it"},
	}
	for _, tc := range cases {
		var notices []testAgentNotice
		task := &agentTask{prompt: "do it", notify: func(title, _, body string) {
			notices = append(notices, testAgentNotice{title: title, body: body})
		}}
		a := &app{workspace: &workspace{}, agentTask: task}
		a.agentTaskEvent(task, acp.Event{Kind: acp.KindTurnDone, Stop: tc.stop})
		if len(notices) != 1 || notices[0].title != tc.title || notices[0].body != tc.body {
			t.Errorf("stop %q produced %+v, want title %q body %q", tc.stop, notices, tc.title, tc.body)
		}
	}
}

func TestAgentCompletionRefreshesCleanNotesAndKeepsDirtyEdits(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"clean.md": "old clean\n", "dirty.md": "saved dirty\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := project.New(fs.NewDirect(dir))
	if err := store.Refresh(); err != nil {
		t.Fatal(err)
	}
	clean := store.ByRelPath("clean.md")
	dirty := store.ByRelPath("dirty.md")
	dirty.Buffer().Set("local edit\n")
	if err := os.WriteFile(filepath.Join(dir, "clean.md"), []byte("agent edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dirty.md"), []byte("external edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := &agentTask{prompt: "edit"}
	a := &app{workspace: &workspace{store: store}, agentTask: task, harness: true}
	a.agentTaskEvent(task, acp.Event{Kind: acp.KindTurnDone, Stop: "end_turn"})
	if clean.Text() != "agent edit\n" {
		t.Errorf("clean note text = %q, want agent edit", clean.Text())
	}
	if dirty.Text() != "local edit\n" || !dirty.Dirty() {
		t.Errorf("dirty note was overwritten: text=%q dirty=%v", dirty.Text(), dirty.Dirty())
	}
	if store.ByRelPath("new.md") == nil {
		t.Error("the agent's new file did not enter the refreshed project tree")
	}
}

func TestAgentEventsRemainIsolatedToTheirTask(t *testing.T) {
	oldTask := &agentTask{prompt: "old"}
	newTask := &agentTask{prompt: "new"}
	a := &app{workspace: &workspace{}, agentTask: newTask, harness: true}
	a.agentTaskEvent(oldTask, acp.Event{Kind: acp.KindTurnDone, Stop: "end_turn"})
	if a.agentTask != newTask {
		t.Fatal("an old task's terminal event cleared the replacement task")
	}
}
