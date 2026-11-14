package git

import (
	"errors"
	"fmt"
	"strings"
)

// Branch is one local branch the project can switch to.
type Branch struct {
	Name    string
	Current bool
}

// BranchData is the repository a project belongs to and the branches it
// holds. Root is empty when the project is not inside a repository.
type BranchData struct {
	Root     string
	Current  string // the checked-out branch, or the short hash of a detached HEAD
	Detached bool
	Branches []Branch
	Err      string
}

// ReadBranches reads the repository root, the branch checked out there and
// the local branches ordered by latest commit. A folder that is not in a
// repository, or a failure while listing, leaves Err set and Root empty.
func ReadBranches(r Runner, project string) BranchData {
	if r == nil {
		r = LocalRunner{}
	}
	if resolved, err := r.RealPath(project); err == nil && resolved != "" {
		project = resolved
	}
	var d BranchData
	root, err := r.Run(project, "rev-parse", "--show-toplevel")
	if err != nil {
		d.Err = "This folder is not a Git repository."
		return d
	}
	d.Root = strings.TrimSpace(root)
	if d.Root == "" {
		d.Err = "This folder is not a Git repository."
		return d
	}
	if name, err := r.Run(d.Root, "symbolic-ref", "--short", "HEAD"); err == nil {
		d.Current = strings.TrimSpace(name)
	}
	if d.Current == "" {
		d.Detached = true
		if sha, err := r.Run(d.Root, "rev-parse", "--short", "HEAD"); err == nil {
			d.Current = strings.TrimSpace(sha)
		}
		if d.Current == "" {
			d.Current = "detached"
		}
	}
	out, err := r.Run(d.Root, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)", "refs/heads")
	if err != nil {
		d.Err = err.Error()
		return d
	}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		d.Branches = append(d.Branches, Branch{Name: name, Current: !d.Detached && name == d.Current})
	}
	return d
}

// SwitchBranch checks out an existing local branch.
func SwitchBranch(r Runner, root, name string) error {
	name, err := branchName(name)
	if err != nil {
		return err
	}
	if r == nil {
		r = LocalRunner{}
	}
	if _, err := r.Run(root, "switch", name); err != nil {
		return retryCheckout(r, err, root, "checkout", name)
	}
	return nil
}

// CreateBranch creates a branch at the current commit and checks it out.
func CreateBranch(r Runner, root, name string) error {
	name, err := branchName(name)
	if err != nil {
		return err
	}
	if r == nil {
		r = LocalRunner{}
	}
	if _, err := r.Run(root, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a valid branch name", name)
	}
	if _, err := r.Run(root, "switch", "-c", name); err != nil {
		return retryCheckout(r, err, root, "checkout", "-b", name)
	}
	return nil
}

// branchName trims a name typed into the picker and refuses the empty or
// option-shaped ones before Git sees them.
func branchName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "-") {
		return "", errors.New("a branch needs a name")
	}
	return name, nil
}

// retryCheckout falls back to checkout for Git older than 2.23, which has no
// switch subcommand, and passes any other failure through.
func retryCheckout(r Runner, err error, root string, args ...string) error {
	if !strings.Contains(err.Error(), "not a git command") {
		return err
	}
	_, err = r.Run(root, args...)
	return err
}
