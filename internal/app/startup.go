package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrHelp ends command-line parsing for -h and --help.
var ErrHelp = errors.New("help")

// Startup is what the command line asked the window to open: a project
// folder, and optionally an absolute file inside it to show.
type Startup struct {
	Dir  string
	File string
}

// ParseStartup reads the one path a launch may name. base is the working
// directory the launch ran in, which resolves a relative path: empty for
// this process's own, and the launch's own for one handed over by a later
// instance. A folder becomes the project; a file makes its folder the
// project and names the file to show.
func ParseStartup(args []string, base string) (Startup, error) {
	var paths []string
	for _, arg := range args {
		switch {
		case arg == "-h" || arg == "--help" || arg == "help":
			return Startup{}, ErrHelp
		case strings.HasPrefix(arg, "-psn_"):
			// macOS LaunchServices passes this when it opens the app.
		case strings.HasPrefix(arg, "-") && arg != "-":
			return Startup{}, fmt.Errorf("unknown option %q", arg)
		default:
			paths = append(paths, arg)
		}
	}
	if len(paths) > 1 {
		return Startup{}, fmt.Errorf("expected one path, got %d", len(paths))
	}
	if len(paths) == 0 {
		return Startup{}, nil
	}
	abs, err := resolveStartupPath(paths[0], base)
	if err != nil {
		return Startup{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Startup{}, fmt.Errorf("%s: %w", paths[0], err)
	}
	if info.IsDir() {
		return Startup{Dir: abs}, nil
	}
	if !info.Mode().IsRegular() {
		return Startup{}, fmt.Errorf("%s: not a file or folder", paths[0])
	}
	return Startup{Dir: filepath.Dir(abs), File: abs}, nil
}

// resolveStartupPath expands a leading ~ and makes p absolute against
// base, so the window opens the path the shell meant even when the tilde
// was quoted or the launch ran in another directory.
func resolveStartupPath(p, base string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			p = home
		} else {
			p = filepath.Join(home, p[2:])
		}
	}
	if base != "" && !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Abs(p)
}

// openStartupFile shows the file the command line named, once its project
// is loaded, in the pane the restored layout left focused.
func (a *app) openStartupFile() {
	if a.startup.File == "" || a.store == nil {
		return
	}
	// The command line names the file under the folder it named, which
	// may be a symlink the store opened at its target.
	base := a.startup.Dir
	if base == "" {
		base = a.store.Dir
	}
	rel, err := filepath.Rel(base, a.startup.File)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	a.openRel(rel)
}
