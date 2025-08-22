package rebranch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// GitInterface abstracts Git operations
type GitInterface interface {
	GetCurrentBranch() (string, error)
	BranchExists(branch string) bool
	GetCommitsBetween(base, head string) ([]CommitInfo, error)
	CreateBranch(name, base string) error
	CheckoutBranch(name string) error
	CherryPick(sha string) error
	DeleteBranch(name string) error
	RenameBranch(oldName, newName string) error
	HasUncommittedChanges() (bool, error)
	IsCleanWorkingDirectory() (bool, error)
	HasOngoingOperation() (bool, string, error)
	IsValidRepository() error
	GetRepoPath() string
}

// Git implements GitInterface using hybrid go-git + exec.Command approach
type Git struct {
	repo     *git.Repository
	repoPath string
}

// NewGit creates a new Git instance
func NewGit() (GitInterface, error) {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Open repository with go-git
	repo, err := git.PlainOpen(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository: %w", err)
	}

	return &Git{
		repo:     repo,
		repoPath: cwd,
	}, nil
}

// NewGitInPath creates a new Git instance for a specific path
func NewGitInPath(path string) (GitInterface, error) {
	// Open repository with go-git
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository at %s: %w", path, err)
	}

	return &Git{
		repo:     repo,
		repoPath: path,
	}, nil
}

func (g *Git) GetCurrentBranch() (string, error) {
	head, err := g.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	if !head.Name().IsBranch() {
		return "", errors.New("HEAD is not pointing to a branch (detached HEAD state)")
	}

	return head.Name().Short(), nil
}

func (g *Git) BranchExists(branch string) bool {
	_, err := g.repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	return err == nil
}

func (g *Git) GetCommitsBetween(base, head string) ([]CommitInfo, error) {
	// Get references for both branches
	baseRef, err := g.repo.Reference(plumbing.NewBranchReferenceName(base), true)
	if err != nil {
		return nil, fmt.Errorf("base branch %s not found: %w", base, err)
	}

	headRef, err := g.repo.Reference(plumbing.NewBranchReferenceName(head), true)
	if err != nil {
		return nil, fmt.Errorf("head branch %s not found: %w", head, err)
	}

	// Get commit objects
	baseCommit, err := g.repo.CommitObject(baseRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get base commit: %w", err)
	}

	headCommit, err := g.repo.CommitObject(headRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get head commit: %w", err)
	}

	// If base and head are the same, return empty list
	if baseCommit.Hash == headCommit.Hash {
		return []CommitInfo{}, nil
	}

	// Get commits from head to base (exclusive of base)
	commits := []CommitInfo{}
	commitIter, err := g.repo.Log(&git.LogOptions{
		From: headCommit.Hash,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get commit log: %w", err)
	}
	defer commitIter.Close()

	err = commitIter.ForEach(func(commit *object.Commit) error {
		// Stop when we reach the base commit
		if commit.Hash == baseCommit.Hash {
			return nil
		}

		// Check if this commit is reachable from base
		isAncestor, err := g.isAncestor(baseCommit.Hash, commit.Hash)
		if err != nil {
			return err
		}

		// Only include commits that are NOT ancestors of base
		// (i.e., commits that are unique to the head branch)
		if !isAncestor {
			// Prepend to maintain chronological order (oldest first)
			commits = append([]CommitInfo{{
				SHA:     commit.Hash.String(),
				Message: strings.TrimSpace(commit.Message),
				Action:  "pick",
			}}, commits...)
		}

		return nil
	})

	return commits, err
}

func (g *Git) CreateBranch(name, base string) error {
	// Get the base reference
	baseRef, err := g.repo.Reference(plumbing.NewBranchReferenceName(base), true)
	if err != nil {
		return fmt.Errorf("base branch %s not found: %w", base, err)
	}

	// Create new branch reference
	branchRef := plumbing.NewBranchReferenceName(name)
	ref := plumbing.NewHashReference(branchRef, baseRef.Hash())

	return g.repo.Storer.SetReference(ref)
}

func (g *Git) CheckoutBranch(name string) error {
	// Use git command for checkout to handle working directory properly
	cmd := exec.Command("git", "checkout", name)
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w\nOutput: %s", name, err, string(output))
	}
	return nil
}

func (g *Git) CherryPick(sha string) error {
	// Use git command for cherry-pick since go-git doesn't support it
	cmd := exec.Command("git", "cherry-pick", sha)
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a conflict (exit code 1) vs other error
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return fmt.Errorf("cherry-pick conflict for %s: %w", sha, err)
		}
		return fmt.Errorf("failed to cherry-pick %s: %w\nOutput: %s", sha, err, string(output))
	}
	return nil
}

func (g *Git) DeleteBranch(name string) error {
	// Use git command to delete branch properly
	cmd := exec.Command("git", "branch", "-D", name)
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete branch %s: %w\nOutput: %s", name, err, string(output))
	}
	return nil
}

func (g *Git) RenameBranch(oldName, newName string) error {
	// Use git command for branch rename
	cmd := exec.Command("git", "branch", "-m", oldName, newName)
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to rename branch %s to %s: %w\nOutput: %s", oldName, newName, err, string(output))
	}
	return nil
}

func (g *Git) HasUncommittedChanges() (bool, error) {
	worktree, err := g.repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return false, fmt.Errorf("failed to get status: %w", err)
	}

	return !status.IsClean(), nil
}

func (g *Git) IsCleanWorkingDirectory() (bool, error) {
	hasChanges, err := g.HasUncommittedChanges()
	if err != nil {
		return false, err
	}
	return !hasChanges, nil
}

func (g *Git) HasOngoingOperation() (bool, string, error) {
	gitDir := filepath.Join(g.repoPath, ".git")

	// Check for various ongoing operations
	operations := map[string]string{
		"REBASE_HEAD":     "rebase",
		"MERGE_HEAD":      "merge",
		"CHERRY_PICK_HEAD": "cherry-pick",
		"REVERT_HEAD":     "revert",
		StateFileName:     "rebranch",
	}

	for file, operation := range operations {
		if _, err := os.Stat(filepath.Join(gitDir, file)); err == nil {
			return true, operation, nil
		}
	}

	return false, "", nil
}

func (g *Git) IsValidRepository() error {
	// Check if .git directory exists
	gitDir := filepath.Join(g.repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return errors.New("not a git repository (no .git directory found)")
	}

	// Try to get HEAD
	_, err := g.repo.Head()
	if err != nil {
		return fmt.Errorf("invalid git repository: %w", err)
	}

	return nil
}

// isAncestor checks if ancestor is an ancestor of descendant
func (g *Git) isAncestor(ancestor, descendant plumbing.Hash) (bool, error) {
	if ancestor == descendant {
		return true, nil
	}

	descendantCommit, err := g.repo.CommitObject(descendant)
	if err != nil {
		return false, err
	}

	ancestorCommit, err := g.repo.CommitObject(ancestor)
	if err != nil {
		return false, err
	}

	return descendantCommit.IsAncestor(ancestorCommit)
}

func (g *Git) GetRepoPath() string {
	return g.repoPath
}