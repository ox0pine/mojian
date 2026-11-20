# Background agent tasks

## Responsibilities

An agent task runs one [Agent Client Protocol](https://agentclientprotocol.com) prompt in the
project folder without opening a pane. There is no transcript: the user asks for the task in a
small sheet, leaves it running, and hears from the system notification when the turn ends, a tool
fails, or the agent asks for permission. The window is the ACP *client*; the agent is a child
process that lives only as long as its task.

## Source of truth

- [`internal/acp/`](../../internal/acp) — the protocol side. `Start` runs the command over stdio
  (default `omp acp`), sends `initialize` advertising no file-system, terminal or auth help,
  opens a session in the project directory, and reports typed events. `Prompt` blocks until the
  turn ends; `Cancel` ends it; `Close` kills the process. The agent's stderr keeps its last lines
  for an error message.
- [`internal/app/agent.go`](../../internal/app/agent.go) — `agentTask`, `openAgentTask`,
  `startAgentTask`, `agentTaskEvent`, `agentSheet`, `agentPermission`, `notifyAgentTask`,
  `finishAgentTask` and `stopAgentTask`.
- [`internal/app/app.go`](../../internal/app/app.go) — `agentTask`, `agentOpen`, `agentInput`.
- [`internal/app/view.go`](../../internal/app/view.go) — `agentSheet` joins the modal stack.
- [`internal/app/workspace.go`](../../internal/app/workspace.go) — `closeModals` also closes the
  sheet.
- [`internal/acp/acp.go`](../../internal/acp/acp.go) — `Agents`, the ordered list of known ACP
  commands (`omp acp`, `claude-code-acp`, `claude-code-acp-rs`, `codex-acp`, `gemini --experimental-acp`,
  `qwen --experimental-acp`, `goose acp`, `opencode acp`, `kimi --acp`, `amp acp`,
  `cursor-agent acp`), and `StartAny`, which gives each one on PATH a bounded handshake and runs
  the first that answers. Claude Code and Codex join through those adapters, not their own
  interactive commands.

## Runtime path

1. View › Run Agent Task… (`run-agent-task`) or the palette calls `openAgentTask`. A memory store
   or a remote project is refused: the agent runs on this machine, in `a.store.Dir`. While a task
   runs, the command reopens the sheet showing either the pending permission or the running task
   with a Stop button.
2. The sheet takes one line and Enter or Run calls `startAgentTask`. The sheet closes, the task is
   stored on `*app`, and `acp.StartAny` tries the known agents in order on a goroutine, so the task
   does not depend on one vendor's command.
3. Events reach the window through `(*app).uiPoster`: `Window.Update` in a real window, the
   `Headless` queue in the harness. The task is identified by pointer, so events of a cancelled
   task are dropped.
4. Agent text is kept for the completion notification, capped at 8 KB. `tool_call_update` with
   status `failed` notifies at once and leaves the task running.
5. `session/request_permission` stores the request on the task, opens the sheet and notifies. The
   sheet answers with an option ID; Reject or closing the window answers cancelled.
6. `session/prompt` returning (or failing) notifies `Agent finished` / `Agent error` and closes
   the session. The notification body is the answer's first 160 characters, or the prompt when the
   answer was empty; its subtitle names the agent, and the time the task took once it ended.
7. Closing the window, Stop or a new project stops the task and kills the agent.

## Contracts and failure behavior

- One task per window. Its process never outlives the window, unlike a terminal's session server.
- Notifications are the only result: never in the harness, never while quitting, no focus gate (a
  background task has no in-app output to watch) and no setting of its own. Titles: `Agent finished`,
  `Agent tool failed`, `Agent needs permission`, `Agent error`, `Agent stopped`, `Agent could not start`.
- Permissions block the agent until the sheet is answered; the notification still fires first, and
  the command reopens the sheet later.
- The task is not persisted: a launch starts with no agent. A missing `omp` or a protocol error is
  reported in the start notification, with the agent's last stderr line when it wrote one.
- There is no Agent page in Settings: the agent is discovered per task, and every discoverable
  agent gets the same treatment. No pane, no transcript and no Markdown rendering exist by design.

## Verification

```sh
go test ./internal/acp ./internal/app
go tool mygo vet .
go run ./cmd/harness   # the sheet, without a real notification
```
