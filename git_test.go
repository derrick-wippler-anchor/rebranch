package rebranch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"rebranch"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRepo creates a temporary git repository for testing
func setupTestRepo(t *testing.T) (string, rebranch.GitInterface, func()) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "rebranch-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user for commits
	configCommands := [][]string{
		{"git", "config", "user.name", "Test User"},
		{"git", "config", "user.email", "test@example.com"},
	}

	for _, cmdArgs := range configCommands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		cmd.Dir = tempDir
		if err := cmd.Run(); err != nil {
			os.RemoveAll(tempDir)
			t.Fatalf("Failed to configure git: %v", err)
		}
	}

	// Create initial commit
	if err := createCommit(tempDir, "initial.txt", "Initial content", "Initial commit"); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create initial commit: %v", err)
	}

	// Create Git instance
	git, err := rebranch.NewGitInPath(tempDir)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create Git instance: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return tempDir, git, cleanup
}

// createCommit creates a file and commits it
func createCommit(repoPath, filename, content, message string) error {
	// Create file
	filePath := filepath.Join(repoPath, filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return err
	}

	// Add file
	cmd := exec.Command("git", "add", filename)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return err
	}

	// Commit file
	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	return cmd.Run()
}

// createBranch creates a new branch and optionally switches to it
func createBranch(repoPath, branchName string, checkout bool) error {
	var cmd *exec.Cmd
	if checkout {
		cmd = exec.Command("git", "checkout", "-b", branchName)
	} else {
		cmd = exec.Command("git", "branch", branchName)
	}
	cmd.Dir = repoPath
	return cmd.Run()
}

func TestGetCurrentBranch(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	// Test default branch (should be 'main' or 'master')
	branch, err := git.GetCurrentBranch()
	require.NoError(t, err)
	assert.True(t, branch == "main" || branch == "master")

	// Create and switch to new branch
	err = createBranch(repoPath, "feature", true)
	require.NoError(t, err)

	branch, err = git.GetCurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "feature", branch)
}

func TestBranchExists(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	// Test existing branch
	currentBranch, _ := git.GetCurrentBranch()
	assert.True(t, git.BranchExists(currentBranch))

	// Test non-existing branch
	assert.False(t, git.BranchExists("nonexistent"))

	// Create new branch and test
	err := createBranch(repoPath, "test-branch", false)
	require.NoError(t, err)
	assert.True(t, git.BranchExists("test-branch"))
}

func TestCreateBranch(t *testing.T) {
	_, git, cleanup := setupTestRepo(t)
	defer cleanup()

	currentBranch, _ := git.GetCurrentBranch()

	// Create new branch based on current branch
	err := git.CreateBranch("new-branch", currentBranch)
	require.NoError(t, err)
	assert.True(t, git.BranchExists("new-branch"))

	// Test creating branch from non-existent base
	err = git.CreateBranch("invalid-branch", "nonexistent")
	assert.Error(t, err)
}

func TestCheckoutBranch(t *testing.T) {
	_, git, cleanup := setupTestRepo(t)
	defer cleanup()

	currentBranch, _ := git.GetCurrentBranch()

	// Create new branch
	err := git.CreateBranch("checkout-test", currentBranch)
	require.NoError(t, err)

	// Checkout the new branch
	err = git.CheckoutBranch("checkout-test")
	require.NoError(t, err)

	// Verify we're on the new branch
	branch, err := git.GetCurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "checkout-test", branch)

	// Test checking out non-existent branch
	err = git.CheckoutBranch("nonexistent")
	assert.Error(t, err)
}

func TestGetCommitsBetween(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	currentBranch, _ := git.GetCurrentBranch()

	// Create feature branch
	err := createBranch(repoPath, "feature", true)
	require.NoError(t, err)

	// Add commits to feature branch
	commits := []struct {
		file    string
		content string
		message string
	}{
		{"file1.txt", "Content 1", "Add file1"},
		{"file2.txt", "Content 2", "Add file2"},
		{"file3.txt", "Content 3", "Add file3"},
	}

	for _, commit := range commits {
		err := createCommit(repoPath, commit.file, commit.content, commit.message)
		require.NoError(t, err)
	}

	// Get commits between base and feature
	commitInfos, err := git.GetCommitsBetween(currentBranch, "feature")
	require.NoError(t, err)
	assert.Len(t, commitInfos, 3)

	// Verify commit messages (should be in chronological order)
	expectedMessages := []string{"Add file1", "Add file2", "Add file3"}
	for i, commitInfo := range commitInfos {
		assert.Contains(t, commitInfo.Message, expectedMessages[i])
		assert.Equal(t, "pick", commitInfo.Action)
	}

	// Test with same base and head (should return empty)
	commitInfos, err = git.GetCommitsBetween(currentBranch, currentBranch)
	require.NoError(t, err)
	assert.Len(t, commitInfos, 0)
}

func TestCherryPick(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	currentBranch, _ := git.GetCurrentBranch()

	// Create feature branch and add a commit
	err := createBranch(repoPath, "feature", true)
	require.NoError(t, err)

	err = createCommit(repoPath, "cherry.txt", "Cherry content", "Cherry commit")
	require.NoError(t, err)

	// Get the commit SHA
	commits, err := git.GetCommitsBetween(currentBranch, "feature")
	require.NoError(t, err)
	require.Len(t, commits, 1)

	commitSHA := commits[0].SHA

	// Switch back to base branch
	err = git.CheckoutBranch(currentBranch)
	require.NoError(t, err)

	// Cherry-pick the commit
	err = git.CherryPick(commitSHA)
	require.NoError(t, err)

	// Verify the file was cherry-picked
	cherryFile := filepath.Join(repoPath, "cherry.txt")
	_, err = os.Stat(cherryFile)
	assert.NoError(t, err)
}

func TestDeleteBranch(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	currentBranch, _ := git.GetCurrentBranch()

	// Create branch to delete
	err := createBranch(repoPath, "to-delete", false)
	require.NoError(t, err)
	assert.True(t, git.BranchExists("to-delete"))

	// Delete branch
	err = git.DeleteBranch("to-delete")
	require.NoError(t, err)
	assert.False(t, git.BranchExists("to-delete"))

	// Test deleting current branch (should fail)
	err = git.DeleteBranch(currentBranch)
	assert.Error(t, err)
}

func TestRenameBranch(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	// Create branch to rename
	err := createBranch(repoPath, "old-name", false)
	require.NoError(t, err)

	// Rename branch
	err = git.RenameBranch("old-name", "new-name")
	require.NoError(t, err)

	// Verify old name doesn't exist and new name exists
	assert.False(t, git.BranchExists("old-name"))
	assert.True(t, git.BranchExists("new-name"))
}

func TestHasUncommittedChanges(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	// Initially should be clean
	hasChanges, err := git.HasUncommittedChanges()
	require.NoError(t, err)
	assert.False(t, hasChanges)

	// Create uncommitted file
	filePath := filepath.Join(repoPath, "uncommitted.txt")
	err = os.WriteFile(filePath, []byte("uncommitted content"), 0644)
	require.NoError(t, err)

	// Should now have changes
	hasChanges, err = git.HasUncommittedChanges()
	require.NoError(t, err)
	assert.True(t, hasChanges)
}

func TestIsCleanWorkingDirectory(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	// Initially should be clean
	isClean, err := git.IsCleanWorkingDirectory()
	require.NoError(t, err)
	assert.True(t, isClean)

	// Create uncommitted file
	filePath := filepath.Join(repoPath, "dirty.txt")
	err = os.WriteFile(filePath, []byte("dirty content"), 0644)
	require.NoError(t, err)

	// Should now be dirty
	isClean, err = git.IsCleanWorkingDirectory()
	require.NoError(t, err)
	assert.False(t, isClean)
}

func TestHasOngoingOperation(t *testing.T) {
	repoPath, git, cleanup := setupTestRepo(t)
	defer cleanup()

	// Initially should have no ongoing operations
	hasOp, opType, err := git.HasOngoingOperation()
	require.NoError(t, err)
	assert.False(t, hasOp)
	assert.Empty(t, opType)

	// Simulate ongoing rebranch operation by creating state file
	gitDir := filepath.Join(repoPath, ".git")
	stateFile := filepath.Join(gitDir, "REBRANCH_STATE")
	err = os.WriteFile(stateFile, []byte("{}"), 0644)
	require.NoError(t, err)

	// Should now detect ongoing operation
	hasOp, opType, err = git.HasOngoingOperation()
	require.NoError(t, err)
	assert.True(t, hasOp)
	assert.Equal(t, "rebranch", opType)
}

func TestIsValidRepository(t *testing.T) {
	_, git, cleanup := setupTestRepo(t)
	defer cleanup()

	// Should be valid repository
	err := git.IsValidRepository()
	assert.NoError(t, err)

	// Test with non-git directory
	tempDir, err := os.MkdirTemp("", "not-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Try to create Git instance in non-git directory (should fail)
	_, err = rebranch.NewGitInPath(tempDir)
	assert.Error(t, err)
}

func TestNewGitErrors(t *testing.T) {
	// Test NewGit with invalid directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Change to non-existent directory (this should fail)
	tempDir, err := os.MkdirTemp("", "test-*")
	require.NoError(t, err)
	os.RemoveAll(tempDir) // Remove it so it doesn't exist

	_, err = rebranch.NewGitInPath(tempDir)
	assert.Error(t, err)
}