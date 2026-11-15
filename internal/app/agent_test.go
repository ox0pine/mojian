package app

import (
	"testing"

	"mojian/internal/acp"
)

func TestAgentPaneMergesToolUpdates(t *testing.T) {
	ag := &agentPane{tools: map[string]*agentItem{}}
	ag.applyTool(&acp.Tool{ID: "t1", Title: "Run the tests", Kind: acp.ToolExecute, Status: acp.StatusInProgress})
	ag.applyTool(&acp.Tool{ID: "t1", Status: acp.StatusFailed})
	if len(ag.items) != 1 {
		t.Fatalf("%d rows, want one", len(ag.items))
	}
	item := ag.items[0]
	if item.kind != itemTool || item.tool == nil {
		t.Fatalf("row = %+v, want a tool call", item)
	}
	if item.tool.Title != "Run the tests" || item.tool.Kind != acp.ToolExecute {
		t.Errorf("update dropped the earlier fields: %+v", item.tool)
	}
	if item.tool.Status != acp.StatusFailed {
		t.Errorf("status = %q, want failed", item.tool.Status)
	}
}

func TestAgentPaneAppendsStreamedText(t *testing.T) {
	ag := &agentPane{tools: map[string]*agentItem{}}
	ag.appendText(itemAgent, "hello ")
	ag.appendText(itemAgent, "world")
	ag.appendText(itemThought, "thinking")
	ag.appendText(itemAgent, "again")
	if len(ag.items) != 3 {
		t.Fatalf("%d rows, want three", len(ag.items))
	}
	if ag.items[0].text != "hello world" {
		t.Errorf("first message = %q, want %q", ag.items[0].text, "hello world")
	}
	if ag.items[1].kind != itemThought || ag.items[2].text != "again" {
		t.Errorf("rows = %+v, want thought then a new message", ag.items)
	}
}

func TestNewAgentNeedsAProjectOnDisk(t *testing.T) {
	a := &app{workspace: &workspace{}}
	a.newAgent()
	if a.err == "" {
		t.Fatal("a memory or missing project should be refused with a message")
	}
}
