package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchDirFiltersAndModes(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".gitignore", "ignored/\n")
	mustWrite("ignored/secret.txt", "needle inside ignored dir\n")
	mustWrite("a.md", "First line\nHello Needle world\n")
	mustWrite("sub/b.go", "package sub\nconst x = \"needle\"\nconst y = \"NEEDLE\"\n")
	mustWrite("bin.dat", string([]byte{0x00, 0x01, 'n', 'e', 'e', 'd', 'l', 'e', 0x00}))

	res, err := SearchDir(context.Background(), dir, SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || len(res.Matches) != 3 {
		t.Fatalf("got %d matches in %d files, want 3 in 2: %+v", len(res.Matches), res.Files, res.Matches)
	}
	if res.Matches[0].Rel != "a.md" || res.Matches[0].Line != 2 || res.Matches[0].Col != 6 || res.Matches[0].End != 12 {
		t.Fatalf("unexpected first match: %+v", res.Matches[0])
	}

	// Case-sensitive search matches only lowercase needle in sub/b.go.
	cs, err := SearchDir(context.Background(), dir, SearchOptions{Query: "needle", CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Matches) != 1 || cs.Matches[0].Rel != "sub/b.go" || cs.Matches[0].Line != 2 {
		t.Fatalf("case-sensitive got %+v", cs.Matches)
	}

	// Regex search.
	reRes, err := SearchDir(context.Background(), dir, SearchOptions{Query: `const [xy] = "needle"`, Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(reRes.Matches) != 2 {
		t.Fatalf("regex got %+v", reRes.Matches)
	}

	// Invalid regex surfaces an error.
	if _, err := SearchDir(context.Background(), dir, SearchOptions{Query: "[unclosed", Regex: true}); err == nil {
		t.Fatal("expected error for invalid regex")
	}

	// Overrides replace disk content for the same relative path.
	over, err := SearchDirWithOverrides(context.Background(), dir, map[string]string{
		"a.md": "no match anymore\n",
	}, SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if over.Files != 1 || len(over.Matches) != 2 {
		t.Fatalf("override got %d matches in %d files: %+v", len(over.Matches), over.Files, over.Matches)
	}

	// Limit sets Truncated.
	lim, err := SearchDir(context.Background(), dir, SearchOptions{Query: "needle", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(lim.Matches) != 2 || !lim.Truncated {
		t.Fatalf("limit got len=%d truncated=%v", len(lim.Matches), lim.Truncated)
	}
}

func TestSearchLongLineExcerpt(t *testing.T) {
	line := strings.Repeat("a", 300) + "TARGET" + strings.Repeat("b", 300)
	ms, _, err := SearchText("min.js", line, SearchOptions{Query: "TARGET"}, 10)
	if err != nil || len(ms) != 1 {
		t.Fatalf("got %+v, err=%v", ms, err)
	}
	if !strings.Contains(ms[0].Text, "TARGET") || len(ms[0].Text) > 220 {
		t.Fatalf("unexpected excerpt: len=%d text=%q", len(ms[0].Text), ms[0].Text)
	}
}

func TestSearchWholeWordAndTargetedReplace(t *testing.T) {
	text := "foo foobar bar_foo (foo) foo\nFOO foo2\n"
	ms, _, err := SearchText("a.go", text, SearchOptions{Query: "foo", WholeWord: true, CaseSensitive: true}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 1 || ms[0].Col != 0 || ms[0].End != 3 {
		t.Fatalf("wholeWord case-sensitive line 1 = %+v", ms)
	}

	msCI, _, err := SearchText("a.go", text, SearchOptions{Query: "foo", WholeWord: true}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(msCI) != 2 || msCI[0].Line != 1 || msCI[1].Line != 2 || msCI[1].Col != 0 {
		t.Fatalf("wholeWord case-insensitive = %+v", msCI)
	}

	replacedAll, n, err := ReplaceAllText(text, "baz", SearchOptions{Query: "foo", WholeWord: true, CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || replacedAll != "baz foobar bar_foo (baz) baz\nFOO foo2\n" {
		t.Fatalf("ReplaceAllText wholeWord = n=%d text=%q", n, replacedAll)
	}

	// Regex whole-word with capture group expansion.
	reOut, reCount, err := ReplaceAllText("val_1 val_2 xval_3", "item$1", SearchOptions{Query: `val_(\d+)`, Regex: true, WholeWord: true})
	if err != nil || reCount != 2 || reOut != "item1 item2 xval_3" {
		t.Fatalf("regex wholeWord replace = count=%d out=%q err=%v", reCount, reOut, err)
	}

	// ReplaceOneInText replaces only the targeted match.
	targetMatch := SearchMatch{Rel: "a.go", Line: 1, Col: 20, End: 23, Text: text[:28]}
	one, ok, err := ReplaceOneInText(text, targetMatch, "QUX", SearchOptions{Query: "foo", WholeWord: true, CaseSensitive: true})
	if err != nil || !ok || one != "foo foobar bar_foo (QUX) foo\nFOO foo2\n" {
		t.Fatalf("ReplaceOneInText = ok=%v out=%q err=%v", ok, one, err)
	}

	before, oldT, newT, after, ok := PreviewMatchReplacement(SearchMatch{Text: "call(foo) + 1"}, "bar", SearchOptions{Query: "foo", WholeWord: true})
	if !ok || before != "call(" || oldT != "foo" || newT != "bar" || after != ") + 1" {
		t.Fatalf("PreviewMatchReplacement = (%q,%q,%q,%q,%v)", before, oldT, newT, after, ok)
	}
}
