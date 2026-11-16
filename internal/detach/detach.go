// Package detach leaves the terminal that launched Mojian: a terminal
// launch re-runs the binary in its own session, so the shell returns at
// once and the window outlives it.
package detach

import "os"

// detachedEnv marks the copy a terminal launch re-ran in its own session,
// so that copy does not try to detach again.
const detachedEnv = "MOJIAN_DETACHED"

// shouldDetach reports whether this launch should leave the terminal.
// Only a terminal launch detaches: launched from Finder or LaunchServices
// there is no terminal to leave, and the process must stay the app
// instance macOS started. mygo dev also stays attached because it watches
// the child's lifetime and console output.
func shouldDetach(stdinTTY, dev bool) bool {
	return stdinTTY && !dev && os.Getenv(detachedEnv) != "1"
}
