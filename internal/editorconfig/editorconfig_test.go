package editorconfig

import "testing"

func TestResolveHierarchyAndGlobs(t *testing.T) {
	files := map[string]string{
		".editorconfig": `
root = true

[*]
indent_style = space
indent_size = 2
end_of_line = lf
trim_trailing_whitespace = true
insert_final_newline = true

[*.go]
indent_style = tab
indent_size = tab
tab_width = 4

[*.{ts,tsx}]
indent_size = 2

[docs/**.md]
trim_trailing_whitespace = false
`,
		"sub/.editorconfig": `
[*.py]
indent_size = 4
end_of_line = crlf
`,
	}
	read := func(rel string) ([]byte, bool) {
		s, ok := files[rel]
		return []byte(s), ok
	}

	goProps := Resolve("cmd/main.go", read)
	if goProps.IndentStyle != "tab" || goProps.IndentSize != 4 || goProps.TabWidth != 4 || goProps.EndOfLine != "lf" {
		t.Fatalf("unexpected go props: %+v", goProps)
	}
	if goProps.TrimTrailingWhitespace == nil || !*goProps.TrimTrailingWhitespace {
		t.Fatalf("expected trim_trailing_whitespace=true for go")
	}

	tsProps := Resolve("web/App.tsx", read)
	if tsProps.IndentStyle != "space" || tsProps.IndentSize != 2 {
		t.Fatalf("unexpected tsx props: %+v", tsProps)
	}

	pyProps := Resolve("sub/tool.py", read)
	if pyProps.IndentStyle != "space" || pyProps.IndentSize != 4 || pyProps.EndOfLine != "crlf" {
		t.Fatalf("unexpected py props: %+v", pyProps)
	}

	mdProps := Resolve("docs/guide/intro.md", read)
	if mdProps.TrimTrailingWhitespace == nil || *mdProps.TrimTrailingWhitespace {
		t.Fatalf("expected trim_trailing_whitespace=false for docs/**.md, got %+v", mdProps.TrimTrailingWhitespace)
	}
}

func TestResolveStopsAtRootTrue(t *testing.T) {
	files := map[string]string{
		".editorconfig": `
[*]
indent_size = 8
`,
		"pkg/.editorconfig": `
root = true
[*]
indent_style = space
`,
	}
	read := func(rel string) ([]byte, bool) {
		s, ok := files[rel]
		return []byte(s), ok
	}
	p := Resolve("pkg/a.txt", read)
	if p.IndentSize != 0 || p.IndentStyle != "space" {
		t.Fatalf("expected root=true to stop inheritance, got %+v", p)
	}
}

func TestApplySaveTransforms(t *testing.T) {
	yes := true
	no := false
	got := ApplySaveTransforms("a  \t\nb \n", Props{
		TrimTrailingWhitespace: &yes,
		InsertFinalNewline:     &yes,
	})
	if got != "a\nb\n" {
		t.Fatalf("got %q, want %q", got, "a\nb\n")
	}

	got = ApplySaveTransforms("hello", Props{InsertFinalNewline: &yes})
	if got != "hello\n" {
		t.Fatalf("got %q, want %q", got, "hello\n")
	}

	got = ApplySaveTransforms("hello\n\n", Props{InsertFinalNewline: &no})
	if got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}
