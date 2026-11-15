package app

import (
	"strings"
	"testing"

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
