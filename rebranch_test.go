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

	// Create mock editor that keeps all commits
	editor := &MockEditor{
		ModifyFunc: func(filePath string) error {
			// Don't modify - keep all picks as default
			return nil
		},
	}

	store, err := rebranch.NewFileStoreInPath(repoPath)
	require.NoError(t, err)

	// Start rebranch operation with mock editor
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
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
	err = rebranch.RunCmd([]string{"--done"}, rebranch.Options{})
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
	err = rebranch.RunCmd([]string{"nonexistent"}, rebranch.Options{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")

	// Test same branch as base
	cmd := exec.Command("git", "checkout", "main")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "same as base branch")

	// Switch back to feature branch
	cmd = exec.Command("git", "checkout", "feature")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	// Test dirty working directory
	dirtyFile := filepath.Join(repoPath, "dirty.txt")
	require.NoError(t, os.WriteFile(dirtyFile, []byte("dirty content"), 0644))

	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "working directory is not clean")

	// Clean up dirty file
	require.NoError(t, os.Remove(dirtyFile))

	// Test no commits to rebranch (create empty branch that has same commits as main)
	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "checkout", "-b", "empty-branch")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	// This should fail at the commit check
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{})
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

	// Create mock editor that keeps all commits
	editor := &MockEditor{
		ModifyFunc: func(filePath string) error {
			// Don't modify - keep all picks as default
			return nil
		},
	}

	store, err := rebranch.NewFileStoreInPath(repoPath)
	require.NoError(t, err)

	// Start rebranch operation with mock editor
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
	require.NoError(t, err)

	// Get temp branch name
	currentBranch, err := git.GetCurrentBranch()
	require.NoError(t, err)
	tempBranch := currentBranch

	// Verify store exists
	assert.True(t, store.StateExists())

	// Abort operation
	err = rebranch.RunCmd([]string{"--abort"}, rebranch.Options{})
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

	// Create mock editor that keeps all commits
	editor := &MockEditor{
		ModifyFunc: func(filePath string) error {
			// Don't modify - keep all picks as default
			return nil
		},
	}

	// Start rebranch operation with mock editor
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
	require.NoError(t, err)

	// Try to start another rebranch operation - should fail validation
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")

	// Finish the operation
	err = rebranch.RunCmd([]string{"--done"}, rebranch.Options{})
	require.NoError(t, err)

	// Now we should be able to start a new operation
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
	require.NoError(t, err)

	// Clean up
	err = rebranch.RunCmd([]string{"--done"}, rebranch.Options{})
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
	err = rebranch.RunCmd([]string{"--continue"}, rebranch.Options{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no rebranch operation in progress")

	// Try to finish without operation
	err = rebranch.RunCmd([]string{"--done"}, rebranch.Options{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no rebranch operation in progress")

	// Try to abort without operation
	err = rebranch.RunCmd([]string{"--abort"}, rebranch.Options{})
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

	// Create mock editor that keeps all commits
	editor := &MockEditor{
		ModifyFunc: func(filePath string) error {
			// Don't modify - keep all picks as default
			return nil
		},
	}

	// Start rebranch operation with mock editor
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
	require.NoError(t, err)

	// Manually modify store to wrong stage
	store, err := rebranch.NewFileStoreInPath(repoPath)
	require.NoError(t, err)
	state, err := store.LoadState()
	require.NoError(t, err)

	state.Stage = "picking"
	require.NoError(t, store.SaveState(state))

	// Try to finish from wrong stage
	err = rebranch.RunCmd([]string{"--done"}, rebranch.Options{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not ready to finish")

	// Clean up
	err = rebranch.RunCmd([]string{"--abort"}, rebranch.Options{})
	require.NoError(t, err)
}

func TestInteractiveCommitSelection(t *testing.T) {
	repoPath, cleanup := setupRebranchTestRepo(t)
	defer cleanup()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(repoPath))

	// Create mock editor that modifies pick file
	editor := &MockEditor{
		ModifyFunc: func(filePath string) error {
			// Read original file
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}

			lines := strings.Split(string(data), "\n")
			var modifiedLines []string

			for _, line := range lines {
				if strings.HasPrefix(line, "pick") && strings.Contains(line, "Add feature 2") {
					// Change second commit to drop (using abbreviation)
					modifiedLines = append(modifiedLines, strings.Replace(line, "pick", "d", 1))
				} else {
					modifiedLines = append(modifiedLines, line)
				}
			}

			return os.WriteFile(filePath, []byte(strings.Join(modifiedLines, "\n")), 0644)
		},
	}

	// Start rebranch with mock editor
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
	require.NoError(t, err)

	// Verify we're on temp branch
	git, err := rebranch.NewGitInPath(repoPath)
	require.NoError(t, err)
	currentBranch, err := git.GetCurrentBranch()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(currentBranch, rebranch.TempBranchPrefix))

	// Verify only 2 commits were applied (feature 1 and 3, not feature 2)
	featureFiles := []string{"feature1.txt", "feature3.txt"}
	for _, file := range featureFiles {
		filePath := filepath.Join(repoPath, file)
		_, err := os.Stat(filePath)
		assert.NoError(t, err)
	}

	// Verify feature2.txt was NOT applied
	feature2Path := filepath.Join(repoPath, "feature2.txt")
	_, err = os.Stat(feature2Path)
	assert.Error(t, err)

	// Finish operation
	err = rebranch.RunCmd([]string{"--done"}, rebranch.Options{})
	require.NoError(t, err)
}

func TestConflictResolution(t *testing.T) {
	// Create repository with conflicting changes
	tempDir, err := os.MkdirTemp("", "rebranch-conflict-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	// Configure git user
	configCommands := [][]string{
		{"git", "config", "user.name", "Test User"},
		{"git", "config", "user.email", "test@example.com"},
	}

	for _, cmdArgs := range configCommands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		cmd.Dir = tempDir
		require.NoError(t, cmd.Run())
	}

	// Create initial commit
	require.NoError(t, createCommitInRepo(tempDir, "conflict.txt", "original content\n", "Initial commit"))

	// Create main branch with conflicting change
	require.NoError(t, createCommitInRepo(tempDir, "conflict.txt", "main branch change\n", "Main branch change"))

	// Create feature branch from initial commit
	cmd = exec.Command("git", "checkout", "-b", "feature", "HEAD~1")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	require.NoError(t, createCommitInRepo(tempDir, "conflict.txt", "feature branch change\n", "Feature change"))

	// Change to repo directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	require.NoError(t, os.Chdir(tempDir))

	// Create mock editor that keeps all commits
	editor := &MockEditor{
		ModifyFunc: func(filePath string) error {
			// Don't modify - keep all picks
			return nil
		},
	}

	// Start rebranch - should conflict
	err = rebranch.RunCmd([]string{"main"}, rebranch.Options{Editor: editor})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")

	// Verify state is saved as conflicts stage
	store, err := rebranch.NewFileStoreInPath(tempDir)
	require.NoError(t, err)
	state, err := store.LoadState()
	require.NoError(t, err)
	assert.Equal(t, "conflicts", state.Stage)

	// Manually resolve conflict
	conflictContent := "resolved content\n"
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "conflict.txt"), []byte(conflictContent), 0644))

	// Stage resolved file
	cmd = exec.Command("git", "add", "conflict.txt")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	// Commit the resolved conflict
	cmd = exec.Command("git", "commit", "--no-edit")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	// Continue rebranch
	err = rebranch.RunCmd([]string{"--continue"}, rebranch.Options{})
	require.NoError(t, err)

	// Verify operation completed
	state, err = store.LoadState()
	require.NoError(t, err)
	assert.Equal(t, "done", state.Stage)

	// Finish operation
	err = rebranch.RunCmd([]string{"--done"}, rebranch.Options{})
	require.NoError(t, err)

	// Verify resolved content is in final branch
	finalContent, err := os.ReadFile(filepath.Join(tempDir, "conflict.txt"))
	require.NoError(t, err)
	assert.Equal(t, conflictContent, string(finalContent))
}

func TestFileFormatFunctions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rebranch-format-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test createInteractiveFile
	commits := []rebranch.CommitInfo{
		{SHA: "abc1234567890", Message: "First commit", Action: "pick"},
		{SHA: "def1234567890", Message: "Second commit", Action: "pick"},
	}

	pickFile := filepath.Join(tempDir, "test_pick")
	err = rebranch.CreateInteractiveFile(commits, pickFile)
	require.NoError(t, err)

	// Read and verify file content
	content, err := os.ReadFile(pickFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "pick abc1234 First commit")
	assert.Contains(t, contentStr, "pick def1234 Second commit")
	assert.Contains(t, contentStr, "# Interactive rebranch")

	// Test parseInteractiveFile - modify content to test parsing with abbreviations
	modifiedContent := `# Interactive rebranch - Edit the list of commits to apply
# Commands:
#  pick, p = apply this commit
#  drop, d = skip this commit

p abc1234 First commit
d def1234 Second commit
`
	require.NoError(t, os.WriteFile(pickFile, []byte(modifiedContent), 0644))

	parsedCommits, err := rebranch.ParseInteractiveFile(pickFile, commits)
	require.NoError(t, err)
	require.Len(t, parsedCommits, 2)

	assert.Equal(t, "abc1234567890", parsedCommits[0].SHA)
	assert.Equal(t, "pick", parsedCommits[0].Action)
	assert.Equal(t, "def1234567890", parsedCommits[1].SHA)
	assert.Equal(t, "drop", parsedCommits[1].Action)

	// Test full words still work
	modifiedContentFull := `# Interactive rebranch - Edit the list of commits to apply
pick abc1234 First commit
drop def1234 Second commit
`
	require.NoError(t, os.WriteFile(pickFile, []byte(modifiedContentFull), 0644))

	parsedCommits, err = rebranch.ParseInteractiveFile(pickFile, commits)
	require.NoError(t, err)
	require.Len(t, parsedCommits, 2)

	assert.Equal(t, "abc1234567890", parsedCommits[0].SHA)
	assert.Equal(t, "pick", parsedCommits[0].Action)
	assert.Equal(t, "def1234567890", parsedCommits[1].SHA)
	assert.Equal(t, "drop", parsedCommits[1].Action)

	// Test invalid action
	invalidContent := `# Interactive rebranch - Edit the list of commits to apply
invalid abc1234 First commit
`
	require.NoError(t, os.WriteFile(pickFile, []byte(invalidContent), 0644))

	_, err = rebranch.ParseInteractiveFile(pickFile, commits)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid action 'invalid'")
}

// MockEditor implements EditorInterface for testing
type MockEditor struct {
	ModifyFunc func(filePath string) error
}

func (m *MockEditor) LaunchEditor(filePath string) error {
	if m.ModifyFunc != nil {
		return m.ModifyFunc(filePath)
	}
	return nil
}
