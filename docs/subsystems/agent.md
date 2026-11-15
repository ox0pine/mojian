# Agent panes

## Responsibilities

One agent pane talks to one [Agent Client Protocol](https://agentclientprotocol.com) agent over
stdio: it starts the agent in the project folder, opens a session, streams the turn's messages and
tool calls into the pane, answers the agent's permission questions, and tells the user when a turn
ends, a tool fails or the agent needs an answer. The window is the ACP *client*; the agent keeps
its own tools, files and model access.

## Source of truth

- [`internal/acp/`](../../internal/acp) — the protocol side: `Start` runs the agent, negotiates
  `initialize`, opens a session and returns a `Session`; `Prompt`, `Cancel` and `Close` are the
  only calls the window makes. Events cross the package boundary as `acp.Event`.
- [`internal/app/agent.go`](../../internal/app/agent.go) — `agentPane`, `newAgent`, `startAgent`,
  `agentEvent`, the transcript, the composer and the permission card.
- [`internal/app/model.go`](../../internal/app/model.go) — `pane.Agent`.
- [`internal/app/pane.go`](../../internal/app/pane.go) — the card's body switch, header icon and
  `paneName`.
- [`internal/prefs/prefs.go`](../../internal/prefs/prefs.go) — `AgentCommand` and `NotifyAgent`.

## Runtime path

1. View › New Agent Pane (`new-agent`) adds a pane at the right edge of the active tab, like a
   terminal, and calls `startAgent`. A memory store or a remote project is refused with a message:
   the agent runs on this machine, in `a.store.Dir`.
2. `startAgent` runs `acp.Start` on a goroutine with the command in `AgentCommand` (empty means
   `omp acp`). Each event goes through the poster captured on the UI thread:
   `Window.Update` in the window, and the `Headless` queue in the harness.
3. `Initialize` advertises no file-system, terminal or auth help, so the agent never asks the
   window to read, write, spawn or authenticate. The session's `cwd` is the project folder.
4. A prompt is one `session/prompt` turn. `Prompt` blocks on a goroutine; chunks arrive through
   `session/update`, become transcript rows (or update a tool row by `toolCallId`), and the turn's
   response emits `KindTurnDone` with the stop reason.
5. A permission request blocks the agent until one of its options is answered, the turn is
   cancelled, or the session closes; a cancelled answer is `cancelled`.
6. Closing the pane, its tab, the window or the app closes the session and kills the process.
   Agent panes are not written to the layout: a session belongs to a process, so a restored tab
   keeps its files and drops its agent panes.

## Contracts and failure behavior

- The agent is a child process of the window; it never outlives the pane the way a terminal owned
  by the session server does. `closeWorkspaces` ends the agents of every cached workspace.
- `notifyAgent` mirrors the terminal's notifications: only when the window is not focused, only
  when `NotifyAgent` is on, never in the harness, and never while quitting. Titles are
  `Agent finished`, `Agent tool failed` and `Agent needs permission`; the body names the project,
  the tool or the question.
- Agent text is plain text in the transcript; the pane does not render Markdown. A tool row keeps
  the first title and kind it saw and changes only on later updates.
- A refused start (no `omp`, no ACP answer) shows the command, the error and the agent's last
  stderr line, with Start Again. A turn error is a red row and the pane stays usable.
- The composer sends on Enter and turns into Stop while a turn runs; Stop answers a pending
  permission with `cancelled` and cancels the prompt.
- Notifications are not OS banners in the harness; the headless fixture renders the same pane
  state and can run a real agent through its queue.

## Verification

```sh
go test ./internal/acp ./internal/app
go tool mygo vet .
go run ./cmd/harness   # palette → New Agent Pane → type a prompt
```
