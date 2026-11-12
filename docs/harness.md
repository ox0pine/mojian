# Headless UI harness

`cmd/harness` serves the real `(*app).view` and `settingsWindowView` to an MCP client through [mygo-mcp](https://github.com/auron-labs/mygo-mcp), so an agent can inspect, click, type into and screenshot the interface without a desktop window. `cmd/harness/fixtures.go` builds each fixture over a disposable project (welcome note, prose and code files) and `internal/app/headless.go` exposes the real views; the harness is never imported by the app.

Start it in its own long-lived terminal (`rex`), never with `&` or `nohup`:

```sh
go run ./cmd/harness                 # http://127.0.0.1:8080/mcp
go run ./cmd/harness -addr 127.0.0.1:8090
```

The endpoint speaks MCP Streamable HTTP, has no authentication, and must stay on loopback. magpie already registers it for `omp` as `mygo-mcp http://127.0.0.1:8080/mcp` with `--local`; a new agent session is needed before its tools appear, named `mygo-mcp__mygo_*` because the router shares one server. There is no live reload: stop with Ctrl+C and start again after code changes, or the MCP client still drives the old build.

Fixtures are `Mojian` (1120×720) and `Mojian Settings` (900×640). The workflow:

1. `mygo_sessions` gives each fixture's `session_id`.
2. `mygo_screenshot` or `mygo_inspect` gives an observation and its targets.
3. Act with `mygo_click`, `mygo_click_label`, `mygo_type`, `mygo_key` or `mygo_scroll`.
4. Verify with a new screenshot or `mygo_wait_text`.

Every action invalidates the previous observation: pass the new `observation_id`, or request `inspect: true`/`screenshot: true` with the action. `mygo_click_label` accepts exact text or explicit MyGo labels and fails with `ambiguous` when two elements share the label; those rows need screenshot coordinates instead. Key modifiers are `shift`, `control`, `alt`, `meta` and `primary` (Cmd on macOS); printable text goes through `mygo_type`, not `mygo_key`.

A fixture deliberately stops at the view. Native windows, the session server, terminals and project switching are replaced or guarded by `a.harness`: the Settings gear shows a banner pointing at the `Mojian Settings` fixture, terminals fail with a message, and the disposable project stays put. Because there is no native menu, the harness also registers the command accelerators in the view, so `mygo_key` with `primary` drives Save (`S`), the palette (`Shift+P`), tabs and splits as a user would. Fixtures edit and save only their own temp project, which cleanup removes. Any new action that needs a real window or the session server must be guarded by `a.harness` or it can block the headless worker forever.

To add a fixture, register it in `cmd/harness/main.go` and add its factory in `cmd/harness/fixtures.go`.
