package git

import (
	"path/filepath"
)

// ReadHeadFile returns the HEAD version of rel (relative to projectDir)
// for gutter diff markers. ok is false outside a repository, on an unborn
// HEAD, or when the file is not tracked at HEAD.
func ReadHeadFile(r Runner, projectDir, rel string) (text string, ok bool, err error) {
	if r == nil {
		r = LocalRunner{}
	}
	if projectDir == "" || rel == "" {
		return "", false, nil
	}
	root, prefix, inRepo := repoRoot(r, projectDir)
	if !inRepo {
		return "", false, nil
	}
	repoRel := filepath.ToSlash(filepath.Clean(rel))
	if prefix != "" {
		repoRel = prefix + "/" + repoRel
	}
	out, runErr := r.Run(root, "show", "HEAD:"+repoRel)
	if runErr != nil {
		// Untracked file, deleted from HEAD, or unborn HEAD.
		return "", false, nil
	}
	return out, true, nil
}
