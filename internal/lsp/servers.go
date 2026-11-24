package lsp

import (
	"bufio"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
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

// LookupLocalCommands returns the first installed command from cmds on PATH.
func LookupLocalCommands(cmds [][]string) ([]string, bool) {
	for _, argv := range cmds {
		if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
			continue
		}
		if bin, err := exec.LookPath(argv[0]); err == nil {
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
