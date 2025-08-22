# Rebranch Tool - Technical Design Document

## Overview

`rebranch` is a Git workflow tool that allows users to cherry-pick commits from
the current branch to a new branch based on a different base, then safely
replace the original branch. It provides multi-step safety controls with
optional interactive commit selection.

## Core Workflow

1. **Start**: `rebranch <base-branch>` - Cherry-pick all commits to temp branch (with optional interactive selection in later phases)
2. **Continue**: `rebranch --continue` - Resume after resolving conflicts
3. **Finalize**: `rebranch --done` - Delete original branch, rename temp branch
4. **Abort**: `rebranch --abort` - Cancel operation and cleanup

## Architecture

### Package Structure
```
rebranch/
├── cmd/
│   └── main.go          # CLI entry point
├── rebranch.go          # Core logic and RunCmd()
├── git.go               # GitInterface and hybrid implementation
├── git_test.go          # Git implementation tests
├── editor.go            # EditorInterface and implementation (Phase 3)
├── store.go             # State persistence and management (Store)
├── validation.go        # Pre-flight checks
└── rebranch_test.go     # Functional tests
```

### Core Data Types

```go
// RebranchState represents the current operation state
type RebranchState struct {
    SourceBranch     string       `json:"source_branch"`
    BaseBranch       string       `json:"base_branch"`
    TempBranch       string       `json:"temp_branch"`
    CommitsToApply   []CommitInfo `json:"commits_to_apply"`
    CurrentCommitIdx int          `json:"current_commit_idx"`
    Stage            string       `json:"stage"` // "picking", "conflicts", "done"
}

// CommitInfo represents a commit in the interactive list
type CommitInfo struct {
    SHA     string `json:"sha"`
    Message string `json:"message"`
    Action  string `json:"action"` // "pick" or "drop"
}
```

### Interface Definitions

```go
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
}

// EditorInterface handles launching external editors
type EditorInterface interface {
    LaunchEditor(filepath string) error
}

// Store handles persistent state storage
type Store interface {
    SaveState(state *RebranchState) error
    LoadState() (*RebranchState, error)
    ClearState() error
    StateExists() bool
}
```

### Core Functions

```go
// RunCmd is the main entry point called from cmd/main.go
func RunCmd(args []string) error {
    if len(args) == 0 {
        return errors.New("usage: rebranch <base-branch> | --continue | --done | --abort")
    }

    git := NewGit()
    editor := NewSystemEditor()
    state := NewFileStore()

    switch args[0] {
    case "--continue":
        return continueRebranch(git, state)
    case "--done":
        return finishRebranch(git, state)
    case "--abort":
        return abortRebranch(git, state)
    default:
        return startRebranch(args[0], git, editor, state)
    }
}

// startRebranch begins interactive rebranching process
func startRebranch(baseBranch string, git GitInterface, editor EditorInterface, store Store) error {
    // Validate preconditions
    if err := validateStart(baseBranch, git, store); err != nil {
        return err
    }

    // Get current branch and commits
    sourceBranch, err := git.GetCurrentBranch()
    if err != nil {
        return err
    }

    commits, err := git.GetCommitsBetween(baseBranch, sourceBranch)
    if err != nil {
        return err
    }

    if len(commits) == 0 {
        return errors.New("no commits to rebranch")
    }

    // Create and edit interactive file
    if err := createInteractiveFile(commits); err != nil {
        return err
    }

    if err := editor.LaunchEditor(".git/REBRANCH_PICK"); err != nil {
        return err
    }

    // Parse edited file
    selectedCommits, err := parseInteractiveFile(".git/REBRANCH_PICK")
    if err != nil {
        return err
    }

    // Create temporary branch
    tempBranch := fmt.Sprintf("rebranch-temp-%d", time.Now().Unix())
    if err := git.CreateBranch(tempBranch, baseBranch); err != nil {
        return err
    }

    if err := git.CheckoutBranch(tempBranch); err != nil {
        return err
    }

    // Save initial state
    state := &RebranchState{
        SourceBranch:     sourceBranch,
        BaseBranch:       baseBranch,
        TempBranch:       tempBranch,
        CommitsToApply:   selectedCommits,
        CurrentCommitIdx: 0,
        Stage:           "picking",
    }

    if err := state.SaveState(state); err != nil {
        return err
    }

    // Start cherry-picking
    return applyCherryPicks(git, state, state)
}

// continueRebranch resumes after conflict resolution
func continueRebranch(git GitInterface, store Store) error {
    state, err := store.LoadState()
    if err != nil {
        return err
    }

    if state.Stage != "conflicts" {
        return errors.New("no rebranch in progress or not waiting for conflict resolution")
    }

    state.Stage = "picking"
    state.CurrentCommitIdx++ // Move to next commit

    return applyCherryPicks(git, state, state)
}

// applyCherryPicks applies remaining commits from current index
func applyCherryPicks(git GitInterface, store Store, state *RebranchState) error {
    for i := state.CurrentCommitIdx; i < len(state.CommitsToApply); i++ {
        commit := state.CommitsToApply[i]
        if commit.Action == "drop" {
            continue
        }

        err := git.CherryPick(commit.SHA)
        if err != nil {
            state.CurrentCommitIdx = i
            state.Stage = "conflicts"
            if saveErr := store.SaveState(state); saveErr != nil {
                return fmt.Errorf("cherry-pick failed and could not save state: %v", saveErr)
            }
            return fmt.Errorf("conflict during cherry-pick of %s\nResolve conflicts and run: rebranch --continue", commit.SHA[:7])
        }

        state.CurrentCommitIdx = i
        if err := store.SaveState(state); err != nil {
            return err
        }
    }

    // All commits applied successfully
    fmt.Printf("Successfully applied %d commits to %s\n", countPickedCommits(state.CommitsToApply), state.TempBranch)
    fmt.Printf("Review the new branch history and run: rebranch --done\n")

    state.Stage = "done"
    return state.SaveState(state)
}

// finishRebranch completes the rebranch by replacing original branch
func finishRebranch(git GitInterface, store Store) error {
    state, err := store.LoadState()
    if err != nil {
        return err
    }

    if state.Stage != "done" {
        return errors.New("rebranch not ready to finish - run rebranch --continue first")
    }

    // Delete original branch
    if err := git.DeleteBranch(state.SourceBranch); err != nil {
        return fmt.Errorf("failed to delete original branch %s: %v", state.SourceBranch, err)
    }

    // Rename temp branch to original name
    if err := git.RenameBranch(state.TempBranch, state.SourceBranch); err != nil {
        return fmt.Errorf("failed to rename %s to %s: %v", state.TempBranch, state.SourceBranch, err)
    }

    // Cleanup state
    if err := store.ClearState(); err != nil {
        return err
    }

    fmt.Printf("Successfully rebranched %s onto %s\n", state.SourceBranch, state.BaseBranch)
    return nil
}

// abortRebranch cancels the operation and cleans up
func abortRebranch(git GitInterface, store Store) error {
    state, err := store.LoadState()
    if err != nil {
        return err
    }

    // Switch back to original branch
    if err := git.CheckoutBranch(state.SourceBranch); err != nil {
        return err
    }

    // Delete temp branch
    if err := git.DeleteBranch(state.TempBranch); err != nil {
        // Log warning but don't fail
        fmt.Printf("Warning: failed to delete temp branch %s: %v\n", state.TempBranch, err)
    }

    // Clear state
    if err := store.ClearState(); err != nil {
        return err
    }

    fmt.Printf("Rebranch aborted\n")
    return nil
}
```

### Implementation Classes

```go
// Git implements GitInterface using hybrid go-git + exec.Command approach
type Git struct {
    repo     *git.Repository
    repoPath string
}

func (g *Git) GetCurrentBranch() (string, error) {
    // Implementation using go-git for reading operations
}

func (g *Git) CherryPick(sha string) error {
    // Implementation using exec.Command("git", "cherry-pick", sha)
}

// SystemEditor implements EditorInterface
type SystemEditor struct{}

func (e *SystemEditor) LaunchEditor(filepath string) error {
    editor := os.Getenv("EDITOR")
    if editor == "" {
        editor = "vi"
    }

    cmd := exec.Command(editor, filepath)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    return cmd.Run()
}

// FileStore implements Store
type FileStore struct{}

func (f *FileStore) SaveState(state *RebranchState) error {
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return err
    }

    return os.WriteFile(".git/REBRANCH_STATE", data, 0644)
}

func (f *FileStore) LoadState() (*RebranchState, error) {
    data, err := os.ReadFile(".git/REBRANCH_STATE")
    if err != nil {
        return nil, err
    }

    var state RebranchState
    if err := json.Unmarshal(data, &state); err != nil {
        return nil, err
    }

    return &state, nil
}
```

## Implementation Phases

### Phase 1: Git Interface Implementation and Testing
**Deliverable**: Fully tested git.go with hybrid implementation

**Scope**:
- Hybrid GitInterface implementation (go-git + exec.Command)
- Comprehensive git_test.go with real repository tests
- Git operation validation and error handling
- Test utilities for creating repositories in /tmp

**Acceptance Criteria**:
- All GitInterface methods implemented and tested
- Tests use real Git repositories in temporary directories
- Cherry-pick operations work via exec.Command
- Branch operations work via go-git where appropriate
- Error handling provides clear messages
- All git state validations work correctly

**Functional Tests**:
```go
func TestGitOperations(t *testing.T) {
    // Test all GitInterface methods with real repos:
    // - GetCurrentBranch, BranchExists
    // - GetCommitsBetween, CreateBranch, CheckoutBranch
    // - CherryPick, DeleteBranch, RenameBranch
    // - HasUncommittedChanges, IsCleanWorkingDirectory
}

func TestGitValidation(t *testing.T) {
    // Test all validation scenarios:
    // - Dirty working directory detection
    // - Branch existence checking
    // - Commit range calculation
    // - Repository state validation
}
```

### Phase 2: Basic Rebranch Operation (No Interactive Selection)
**Deliverable**: Simple rebranch operation without interactive editing

**Scope**:
- Core rebranch.go implementation
- Simple workflow: cherry-pick ALL commits from current branch
- State management and persistence
- Basic validation and safety checks
- `--done` and `--abort` functionality

**Acceptance Criteria**:
- `rebranch <base>` applies all commits to new base
- State persistence works correctly
- `rebranch --done` replaces original branch safely
- `rebranch --abort` cleans up properly
- All validation checks prevent invalid operations

**Functional Tests**:
```go
func TestBasicRebranchFlow(t *testing.T) {
    // Setup: Create repo with commits on feature branch
    // Execute: rebranch main (applies all commits)
    // Verify: New branch created, all commits applied
    // Execute: rebranch --done
    // Verify: Original branch deleted, temp branch renamed
}

func TestValidationChecks(t *testing.T) {
    // Test all validation scenarios:
    // - Dirty working directory
    // - Base branch doesn't exist
    // - Current branch equals base branch
    // - No commits to rebranch
}
```

### Phase 3: Interactive Selection and Conflict Resolution
**Deliverable**: Full-featured tool with interactive editing and conflict handling

**Scope**:
- Interactive commit selection with simplified editor format
- Conflict detection during cherry-pick
- `rebranch --continue` implementation
- EditorInterface implementation
- Comprehensive error messages and user guidance

**Acceptance Criteria**:
- Interactive file editing works with simple format
- Conflicts are detected and reported clearly
- `rebranch --continue` resumes from correct commit
- User gets clear instructions for conflict resolution
- Multiple conflicts in sequence are handled correctly

**Functional Tests**:
```go
func TestInteractiveCommitSelection(t *testing.T) {
    // Setup: Create repo with 5 commits on feature branch
    // Execute: rebranch main, modify pick file to drop 2 commits
    // Verify: Only selected commits are applied to new branch
}

func TestConflictHandling(t *testing.T) {
    // Setup: Create repo where cherry-pick will conflict
    // Execute: rebranch main
    // Verify: Process stops, reports conflict, saves state
    // Manually resolve conflict and stage
    // Execute: rebranch --continue
    // Verify: Process resumes and completes successfully
}
```

### Phase 4: Polish and Production Readiness
**Deliverable**: Production-ready tool with comprehensive error handling

**Scope**:
- Comprehensive error messages with suggestions
- Edge case handling (empty commits, merge commits, etc.)
- Performance optimization for large commit ranges
- CLI help and flag parsing with standard library
- Documentation and usage examples

**Acceptance Criteria**:
- All error messages are actionable and helpful
- Tool handles edge cases gracefully
- Performance is acceptable for 100+ commit ranges
- Help documentation is complete and clear
- Tool is ready for production use

## Testing Strategy

### Functional Test Approach
All tests focus on end-to-end behavior through the public `RunCmd()` API:

```go
// Test helper for setting up Git repos
func setupTestRepo(t *testing.T) (string, func()) {
    // Create temporary Git repository
    // Return repo path and cleanup function
}

// Example functional test
func TestCompleteRebranchWorkflow(t *testing.T) {
    repoPath, cleanup := setupTestRepo(t)
    defer cleanup()

    // Setup: Create commits on feature branch
    // Execute: Full rebranch workflow
    // Verify: Final state is correct
    // Test all state transitions and file system changes
}
```

### Integration Points
- Git repository operations (via hybrid go-git + exec.Command)
- File system state management (`.git/REBRANCH_*` files)
- External editor integration (via `$EDITOR`) - Phase 3
- Command-line argument parsing with standard flag library

### Test Database Requirements
Not applicable - this tool operates entirely on Git repositories.

---

## Summary

This design provides a complete, testable implementation of the `rebranch` tool that:

1. **Hybrid approach** - Uses go-git for reading operations, exec.Command for complex operations like cherry-pick
2. **Maintains safety** - Multiple validation points and abort capability
3. **Test-driven development** - Comprehensive testing with real Git repositories in Phase 1
4. **Incremental delivery** - Four-phase approach from git operations → basic rebranch → interactive selection → production polish
5. **Follows CLAUDE.md guidelines** - Proper Go patterns and code structure

The four-phase approach delivers working increments while building toward a
production-ready tool. Phase 1 focuses on getting the git operations solid with
comprehensive testing, then builds incrementally toward the full feature set.
