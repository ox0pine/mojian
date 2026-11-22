package lsp

import (
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"mojian/internal/platform"
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
}

// LanguageForPath maps a file extension to an LSP language ID (or "" if unsupported).
func LanguageForPath(path string) string {
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

// LookupLocal returns the first installed command for lang on PATH.
func LookupLocal(lang string) ([]string, string, bool) {
	spec, ok := SpecForLanguage(lang)
	if !ok {
		return nil, "", false
	}
	for _, argv := range spec.Commands {
		if len(argv) == 0 {
			continue
		}
		if bin, err := exec.LookPath(argv[0]); err == nil {
			out := append([]string{bin}, argv[1:]...)
			return out, spec.Name, true
		}
	}
	return nil, "", false
}

type procConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	once   sync.Once
}

func (p *procConn) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *procConn) Write(b []byte) (int, error) { return p.stdin.Write(b) }

func (p *procConn) Close() error {
	p.once.Do(func() {
		_ = p.stdin.Close()
		_ = p.stdout.Close()
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
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	return &procConn{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}
