package preview

import "testing"

func TestRemoteMarkdownLoadsRelativeImages(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><rect width="16" height="16"/></svg>`)
	var gotRel string
	load := MarkdownImages("/srv/repo/docs", false, "docs/guide.md", func(rel string) ([]byte, error) {
		gotRel = rel
		return svg, nil
	})
	src, err := load("./img/diagram.svg?v=1")
	if err != nil {
		t.Fatalf("remote image load failed: %v", err)
	}
	if src == nil || gotRel != "docs/img/diagram.svg" {
		t.Fatalf("src=%v rel=%q, want docs/img/diagram.svg", src, gotRel)
	}

	// Escaping the project root must be refused.
	if _, err := load("../../../etc/passwd"); err == nil {
		t.Fatal("expected path traversal image to be rejected")
	}
}
