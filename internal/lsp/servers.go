package lsp

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"mojian/internal/platform"
	"mojian/internal/prefs"
)

// ServerSpec describes a language server command for a language ID.
type ServerSpec struct {
	Language string
	Name     string
	Commands [][]string // tried in order
}

var knownServers = []ServerSpec{
	{Language: "go", Name: "gopls", Commands: [][]string{{"gopls"}}},
	{Language: "typescript", Name: "typescript-language-server", Commands: [][]string{{"typescript-language-server", "--stdio"}}},
	{Language: "javascript", Name: "typescript-language-server", Commands: [][]string{{"typescript-language-server", "--stdio"}}},
	{Language: "python", Name: "pyright", Commands: [][]string{
		{"basedpyright-langserver", "--stdio"},
		{"pyright-langserver", "--stdio"},
		{"ruff", "server"},
	}},
	{Language: "rust", Name: "rust-analyzer", Commands: [][]string{{"rust-analyzer"}}},
	{Language: "c", Name: "clangd", Commands: [][]string{{"clangd"}}},
	{Language: "cpp", Name: "clangd", Commands: [][]string{{"clangd"}}},
	{Language: "zig", Name: "zls", Commands: [][]string{{"zls"}}},
	{Language: "lua", Name: "lua-language-server", Commands: [][]string{{"lua-language-server"}}},
	{Language: "shellscript", Name: "bash-language-server", Commands: [][]string{{"bash-language-server", "start"}}},
	{Language: "yaml", Name: "yaml-language-server", Commands: [][]string{{"yaml-language-server", "--stdio"}}},
	{Language: "json", Name: "vscode-json-language-server", Commands: [][]string{{"vscode-json-language-server", "--stdio"}}},
	{Language: "css", Name: "vscode-css-language-server", Commands: [][]string{{"vscode-css-language-server", "--stdio"}}},
	{Language: "html", Name: "vscode-html-language-server", Commands: [][]string{{"vscode-html-language-server", "--stdio"}}},
	{Language: "swift", Name: "sourcekit-lsp", Commands: [][]string{{"sourcekit-lsp"}}},
	{Language: "ruby", Name: "ruby-lsp", Commands: [][]string{{"ruby-lsp"}}},
	{Language: "markdown", Name: "marksman", Commands: [][]string{{"marksman", "server"}}},
	{Language: "toml", Name: "taplo", Commands: [][]string{{"taplo", "lsp", "stdio"}}},
	{Language: "kotlin", Name: "kotlin-language-server", Commands: [][]string{{"kotlin-language-server"}}},
	{Language: "php", Name: "intelephense", Commands: [][]string{
		{"intelephense", "--stdio"},
		{"phpactor", "language-server"},
	}},
	{Language: "dockerfile", Name: "docker-langserver", Commands: [][]string{{"docker-langserver", "--stdio"}}},
}

// LanguageForPath maps a file extension or basename to an LSP language ID (or "" if unsupported).
func LanguageForPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") {
		return "dockerfile"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh":
		return "cpp"
	case ".zig":
		return "zig"
	case ".lua":
		return "lua"
	case ".sh", ".bash", ".zsh":
		return "shellscript"
	case ".yaml", ".yml":
		return "yaml"
	case ".json", ".jsonc":
		return "json"
	case ".css", ".scss", ".less":
		return "css"
	case ".html", ".htm":
		return "html"
	case ".swift":
		return "swift"
	case ".rb":
		return "ruby"
	case ".md", ".markdown":
		return "markdown"
	case ".toml":
		return "toml"
	case ".kt", ".kts":
		return "kotlin"
	case ".php":
		return "php"
	case ".dockerfile":
		return "dockerfile"
	default:
		return ""
	}
}

// ServerGroup normalizes language IDs that share a single server process
// (for example TypeScript and JavaScript, or C and C++).
func ServerGroup(lang string) string {
	switch lang {
	case "javascript":
		return "typescript"
	case "cpp":
		return "c"
	default:
		return lang
	}
}

// SpecForLanguage returns the server spec for lang.
func SpecForLanguage(lang string) (ServerSpec, bool) {
	for _, s := range knownServers {
		if s.Language == lang {
			return s, true
		}
	}
	return ServerSpec{}, false
}

// ResolveServer resolves the effective server specification and initializationOptions
// for lang, applying any user override from settings.json (keyed by lang or ServerGroup(lang)).
func ResolveServer(lang string, overrides map[string]prefs.LSPServerOverride) (spec ServerSpec, initOpts map[string]any, disabled bool, ok bool) {
	baseSpec, hasKnown := SpecForLanguage(lang)
	spec = baseSpec
	ok = hasKnown

	var ov prefs.LSPServerOverride
	hasOv := false
	if len(overrides) > 0 {
		if v, exists := overrides[lang]; exists {
			ov = v
			hasOv = true
		} else if grp := ServerGroup(lang); grp != "" && grp != lang {
			if v, exists := overrides[grp]; exists {
				ov = v
				hasOv = true
			}
		}
	}
	if hasOv {
		if ov.Disabled {
			if spec.Name == "" {
				spec.Language = lang
				spec.Name = lang
			}
			return spec, nil, true, true
		}
		if len(ov.Command) > 0 {
			spec = ServerSpec{
				Language: lang,
				Name:     filepath.Base(ov.Command[0]),
				Commands: [][]string{append([]string(nil), ov.Command...)},
			}
			ok = true
		}
		if len(ov.InitializationOptions) > 0 {
			initOpts = ov.InitializationOptions
		}
	}
	return spec, initOpts, false, ok
}

// LookupLocal returns the first installed command for lang on PATH.
func LookupLocal(lang string) ([]string, string, bool) {
	spec, ok := SpecForLanguage(lang)
	if !ok {
		return nil, "", false
	}
	argv, found := LookupLocalCommands(spec.Commands)
	if !found {
		return nil, "", false
	}
	return argv, spec.Name, true
}

// ExtraToolDirs returns existing standard language-server and toolchain bin
// directories that GUI or non-login shells frequently omit from PATH.
func ExtraToolDirs() []string {
	var raw []string
	if gobin := strings.TrimSpace(os.Getenv("GOBIN")); gobin != "" {
		raw = append(raw, gobin)
	}
	if gopath := strings.TrimSpace(os.Getenv("GOPATH")); gopath != "" {
		for _, entry := range filepath.SplitList(gopath) {
			if strings.TrimSpace(entry) != "" {
				raw = append(raw, filepath.Join(entry, "bin"))
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		raw = append(raw,
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".mise", "shims"),
			filepath.Join(home, ".asdf", "shims"),
			filepath.Join(home, ".npm-global", "bin"),
		)
	}
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			raw = append(raw, filepath.Join(appData, "npm"))
		}
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			raw = append(raw, filepath.Join(localAppData, "Microsoft", "WinGet", "Links"))
		}
	} else {
		raw = append(raw,
			"/opt/homebrew/bin",
			"/opt/homebrew/sbin",
			"/usr/local/bin",
			"/usr/local/go/bin",
		)
	}

	seen := map[string]bool{}
	var dirs []string
	for _, dir := range raw {
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		key := clean
		if runtime.GOOS == "windows" {
			key = strings.ToLower(clean)
		}
		if seen[key] {
			continue
		}
		info, err := os.Stat(clean)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[key] = true
		dirs = append(dirs, clean)
	}
	return dirs
}

// LookupExecutable locates name on PATH, falling back to ExtraToolDirs when
// name does not contain a path separator.
func LookupExecutable(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if bin, err := exec.LookPath(name); err == nil {
		return bin, true
	}
	if strings.ContainsAny(name, `/\`) {
		return "", false
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = windowsExecCandidates(name)
	} else {
		candidates = []string{name}
	}
	for _, dir := range ExtraToolDirs() {
		for _, cand := range candidates {
			full := filepath.Join(dir, cand)
			info, err := os.Stat(full)
			if err != nil || info.IsDir() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
				continue
			}
			return full, true
		}
	}
	return "", false
}

func windowsExecCandidates(name string) []string {
	if filepath.Ext(name) != "" {
		return []string{name}
	}
	exts := []string{".exe", ".cmd", ".bat"}
	if raw := strings.TrimSpace(os.Getenv("PATHEXT")); raw != "" {
		var parsed []string
		for _, ext := range strings.Split(raw, ";") {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				parsed = append(parsed, ext)
			}
		}
		if len(parsed) > 0 {
			exts = parsed
		}
	}
	out := make([]string, 0, len(exts)+1)
	for _, ext := range exts {
		out = append(out, name+strings.ToLower(ext))
	}
	return out
}

// WithAugmentedPATH returns a copy of env with missing ExtraToolDirs appended to PATH.
func WithAugmentedPATH(env []string) []string {
	extra := ExtraToolDirs()
	out := append([]string(nil), env...)
	if len(extra) == 0 {
		return out
	}

	pathIdx := -1
	pathKey := "PATH"
	current := ""
	for i, kv := range out {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if k == "PATH" || (runtime.GOOS == "windows" && strings.EqualFold(k, "PATH")) {
			pathIdx = i
			pathKey = k
			current = v
			break
		}
	}

	parts := filepath.SplitList(current)
	have := make(map[string]bool, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		key := filepath.Clean(p)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		have[key] = true
	}
	var added []string
	for _, dir := range extra {
		key := filepath.Clean(dir)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if !have[key] {
			have[key] = true
			added = append(added, dir)
		}
	}
	if len(added) == 0 {
		return out
	}

	joined := current
	for _, dir := range added {
		if joined == "" {
			joined = dir
		} else {
			joined += string(os.PathListSeparator) + dir
		}
	}
	if pathIdx >= 0 {
		out[pathIdx] = pathKey + "=" + joined
	} else {
		out = append(out, pathKey+"="+joined)
	}
	return out
}

// LookupLocalCommands returns the first installed command from cmds on PATH or standard tool dirs.
func LookupLocalCommands(cmds [][]string) ([]string, bool) {
	for _, argv := range cmds {
		if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
			continue
		}
		if bin, ok := LookupExecutable(argv[0]); ok {
			out := append([]string{bin}, argv[1:]...)
			return out, true
		}
	}
	return nil, false
}

type procConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	once   sync.Once
}

func (p *procConn) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *procConn) Write(b []byte) (int, error) { return p.stdin.Write(b) }

func (p *procConn) Close() error {
	p.once.Do(func() {
		_ = p.stdin.Close()
		_ = p.stdout.Close()
		if p.stderr != nil {
			_ = p.stderr.Close()
		}
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		if p.cmd != nil {
			_ = p.cmd.Wait()
		}
	})
	return nil
}

// StartLocal spawns a local language server process in dir.
func StartLocal(dir string, argv []string) (io.ReadWriteCloser, error) {
	return StartLocalWithLog(dir, argv, nil)
}

// StartLocalWithLog spawns a local language server process in dir and streams
// stderr lines to onLog (when non-nil).
func StartLocalWithLog(dir string, argv []string, onLog func(string)) (io.ReadWriteCloser, error) {
	if len(argv) == 0 {
		return nil, errors.New("lsp: empty command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = WithAugmentedPATH(os.Environ())
	platform.HideConsole(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	var stderr io.ReadCloser
	if onLog != nil {
		stderr, err = cmd.StderrPipe()
		if err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			return nil, err
		}
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		if stderr != nil {
			_ = stderr.Close()
		}
		return nil, err
	}
	if stderr != nil {
		go func() {
			sc := bufio.NewScanner(stderr)
			sc.Buffer(make([]byte, 4096), 64*1024)
			for sc.Scan() {
				onLog(sc.Text())
			}
		}()
	}
	return &procConn{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}
