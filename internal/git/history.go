package git

import (
	"errors"
	"strconv"
	"strings"
)

// Commit is one commit of the project's history.
type Commit struct {
	Hash    string // full object name, for Git
	Short   string // abbreviated object name, for people
	Subject string
	Author  string
	When    string // relative committer date, such as "2 days ago"
}

// HistoryData is the commit list of the repository that holds a project.
// Root and Prefix are the repository root the patches are read from and
// the project's path inside it, which scopes the list to the project.
type HistoryData struct {
	Root    string
	Prefix  string
	Commits []Commit
	Err     string
}

// historyLimit bounds the list: a hundred commits is more history than the
// sidebar shows, and reading them costs one cheap command.
const historyLimit = 100

// ReadHistory reads the newest commits that touched the project. A
// repository without commits is an empty list, not a failure.
func ReadHistory(r Runner, project string) HistoryData {
	if r == nil {
		r = LocalRunner{}
	}
	root, prefix, ok := repoRoot(r, project)
	if !ok {
		return HistoryData{Err: "This folder is not a Git repository."}
	}
	d := HistoryData{Root: root, Prefix: prefix}
	args := []string{"--literal-pathspecs", "-c", "core.quotePath=false", "log", "-z", "--no-color",
		"--max-count=" + strconv.Itoa(historyLimit),
		"--format=%H%x00%h%x00%an%x00%cr%x00%s"}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	out, err := r.Run(root, args...)
	if err != nil {
		if !strings.Contains(err.Error(), "does not have any commits yet") {
			d.Err = err.Error()
		}
		return d
	}
	d.Commits = parseHistory(out)
	return d
}

// parseHistory turns the NUL-separated log output into commits. The
// format writes five fields per commit and -z terminates each record,
// so a trailing empty field closes the last one.
func parseHistory(out string) []Commit {
	fields := strings.Split(out, "\x00")
	var commits []Commit
	for len(fields) >= 5 && fields[0] != "" {
		commits = append(commits, Commit{
			Hash:    fields[0],
			Short:   fields[1],
			Author:  fields[2],
			When:    fields[3],
			Subject: fields[4],
		})
		fields = fields[5:]
	}
	return commits
}

// ReadCommitDiff reads one commit's patch, limited to the project. A merge
// commit leaves Git no single patch to write, so its result is empty.
func ReadCommitDiff(r Runner, root, prefix, hash string) (string, error) {
	if r == nil {
		r = LocalRunner{}
	}
	if hash == "" {
		return "", errors.New("no commit to read")
	}
	args := []string{"--literal-pathspecs", "-c", "core.quotePath=false", "show", "--no-color",
		"--no-ext-diff", "--format=", "--patch", hash}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	return r.Run(root, args...)
}
