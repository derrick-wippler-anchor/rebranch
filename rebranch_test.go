package rebranch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rebranch"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRebranchTestRepo creates a test repository with feature branch containing commits
func setupRebranchTestRepo(t *testing.T) (string, func()) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "rebranch-functional-test-*")
	require.NoError(t, err)

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	// Configure git user for commits
	configCommands := [][]string{
		{"git", "config", "user.name", "Test User"},
		{"git", "config", "user.email", "test@example.com"},
	}

	for _, cmdArgs := range configCommands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		cmd.Dir = tempDir
		require.NoError(t, cmd.Run())
	}

	// Create initial commit on main branch
	require.NoError(t, createCommitInRepo(tempDir, "initial.txt", "Initial content", "Initial commit"))

	// Create feature branch
	cmd = exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	// Add commits to feature branch
	commits := []struct {
		file    string
		content string
		message string
	}{
		{"feature1.txt", "Feature 1 content", "Add feature 1"},
		{"feature2.txt", "Feature 2 content", "Add feature 2"},
		{"feature3.txt", "Feature 3 content", "Add feature 3"},
	}

	for _, commit := range commits {
		require.NoError(t, createCommitInRepo(tempDir, commit.file, commit.content, commit.message))
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return tempDir, cleanup
}

// createCommitInRepo creates a file and commits it in the given repository
func createCommitInRepo(repoPath, filename, content, message string) error {
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

// runRebranchInDir runs rebranch command in specific directory
func runRebranchInDir(repoPath string, args ...string) (string, error) {
	originalDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(repoPath); err != nil {
		return "", err
	}

	return runRebranch(args...)
}

// runRebranch runs rebranch command and returns output
func runRebranch(args ...string) (string, error) {
	err := rebranch.RunCmd(args)
	return "", err // For now, just return error since we don't capture output
}

func TestBasicRebranchFlow(t *testing.T) {
	repoPath, cleanup := setupRebranchTestRepo(t)
	defer cleanup()

	// Change to repo directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(repoPath))

	// Verify initial state - we should be on feature branch
	git, err := rebranch.NewGitInPath(repoPath)
	require.NoError(t, err)

	currentBranch, err := git.GetCurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "feature", currentBranch)

	// Get commits between main and feature
	commits, err := git.GetCommitsBetween("main", "feature")
	require.NoError(t, err)
	assert.Len(t, commits, 3)

	// Start rebranch operation
	err = rebranch.RunCmd([]string{"main"})
	require.NoError(t, err)

	// Verify we're now on temp branch
	currentBranch, err = git.GetCurrentBranch()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(currentBranch, rebranch.TempBranchPrefix))

	// Verify all feature files exist on temp branch
	featureFiles := []string{"feature1.txt", "feature2.txt", "feature3.txt"}
	for _, file := range featureFiles {
		filePath := filepath.Join(repoPath, file)
		_, err := os.Stat(filePath)
		assert.NoError(t, err)
	}

	// Verify store file exists
	store, err := rebranch.NewFileStoreInPath(repoPath)
	require.NoError(t, err)
	assert.True(t, store.StateExists())

	// Load and verify state
	state, err := store.LoadState()
	require.NoError(t, err)
	assert.Equal(t, "feature", state.SourceBranch)
	assert.Equal(t, "main", state.BaseBranch)
	assert.Equal(t, currentBranch, state.TempBranch)
	assert.Equal(t, "done", state.Stage)
	assert.Len(t, state.CommitsToApply, 3)

	// Finish rebranch operation
	err = rebranch.RunCmd([]string{"--done"})
	require.NoError(t, err)

	// Verify we're back on feature branch
	currentBranch, err = git.GetCurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "feature", currentBranch)

	// Verify temp branch is deleted
	assert.False(t, git.BranchExists(state.TempBranch))

	// Verify store is cleaned up
	assert.False(t, store.StateExists())

	// Verify all feature files still exist
	for _, file := range featureFiles {
		filePath := filepath.Join(repoPath, file)
		_, err := os.Stat(filePath)
		assert.NoError(t, err)
	}
}

func TestValidationChecks(t *testing.T) {
	repoPath, cleanup := setupRebranchTestRepo(t)
	defer cleanup()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(repoPath))

	// Test non-existent base branch
	err = rebranch.RunCmd([]string{"nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")

	// Test same branch as base
	cmd := exec.Command("git", "checkout", "main")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	err = rebranch.RunCmd([]string{"main"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "same as base branch")

	// Switch back to feature branch
	cmd = exec.Command("git", "checkout", "feature")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	// Test dirty working directory
	dirtyFile := filepath.Join(repoPath, "dirty.txt")
	require.NoError(t, os.WriteFile(dirtyFile, []byte("dirty content"), 0644))

	err = rebranch.RunCmd([]string{"main"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "working directory is not clean")

	// Clean up dirty file
	require.NoError(t, os.Remove(dirtyFile))

	// Test no commits to rebranch
	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "checkout", "-b", "empty-branch")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	err = rebranch.RunCmd([]string{"main"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no commits to rebranch")
}

func TestAbortOperation(t *testing.T) {
	repoPath, cleanup := setupRebranchTestRepo(t)
	defer cleanup()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(repoPath))

	git, err := rebranch.NewGitInPath(repoPath)
	require.NoError(t, err)

	// Start rebranch operation
	err = rebranch.RunCmd([]string{"main"})
	require.NoError(t, err)

	// Get temp branch name
	currentBranch, err := git.GetCurrentBranch()
	require.NoError(t, err)
	tempBranch := currentBranch

	// Verify store exists
	store, err := rebranch.NewFileStoreInPath(repoPath)
	require.NoError(t, err)
	assert.True(t, store.StateExists())

	// Abort operation
	err = rebranch.RunCmd([]string{"--abort"})
	require.NoError(t, err)

	// Verify we're back on feature branch
	currentBranch, err = git.GetCurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "feature", currentBranch)

	// Verify temp branch is deleted
	assert.False(t, git.BranchExists(tempBranch))

	// Verify state file is cleaned up
	assert.False(t, store.StateExists())
}

func TestOperationInProgress(t *testing.T) {
	repoPath, cleanup := setupRebranchTestRepo(t)
	defer cleanup()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(repoPath))

	// Start rebranch operation
	err = rebranch.RunCmd([]string{"main"})
	require.NoError(t, err)

	// Try to start another rebranch operation
	err = rebranch.RunCmd([]string{"main"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")

	// Finish the operation
	err = rebranch.RunCmd([]string{"--done"})
	require.NoError(t, err)

	// Now we should be able to start a new operation
	err = rebranch.RunCmd([]string{"main"})
	require.NoError(t, err)

	// Clean up
	err = rebranch.RunCmd([]string{"--done"})
	require.NoError(t, err)
}

func TestContinueWithoutOperation(t *testing.T) {
	repoPath, cleanup := setupRebranchTestRepo(t)
	defer cleanup()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(repoPath))

	// Try to continue without operation
	err = rebranch.RunCmd([]string{"--continue"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no rebranch operation in progress")

	// Try to finish without operation
	err = rebranch.RunCmd([]string{"--done"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no rebranch operation in progress")

	// Try to abort without operation
	err = rebranch.RunCmd([]string{"--abort"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no rebranch operation in progress")
}

func TestFinishFromWrongStage(t *testing.T) {
	repoPath, cleanup := setupRebranchTestRepo(t)
	defer cleanup()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(repoPath))

	// Start rebranch operation
	err = rebranch.RunCmd([]string{"main"})
	require.NoError(t, err)

	// Manually modify store to wrong stage
	store, err := rebranch.NewFileStoreInPath(repoPath)
	require.NoError(t, err)
	state, err := store.LoadState()
	require.NoError(t, err)

	state.Stage = "picking"
	require.NoError(t, store.SaveState(state))

	// Try to finish from wrong stage
	err = rebranch.RunCmd([]string{"--done"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not ready to finish")

	// Clean up
	err = rebranch.RunCmd([]string{"--abort"})
	require.NoError(t, err)
}

