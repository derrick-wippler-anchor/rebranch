package rebranch

import (
	"errors"
	"fmt"
	"time"
)

const (
	TempBranchPrefix = "rebranch-temp-"
	StateFileName    = "REBRANCH_STATE"
	PickFileName     = "REBRANCH_PICK"
)

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

// Options provides configuration for RunCmd
type Options struct {
	Editor EditorInterface
}

// RunCmd is the main entry point called from cmd/main.go
func RunCmd(args []string, opts Options) error {
	if len(args) == 0 {
		return errors.New("usage: rebranch <base-branch> | --continue | --done | --abort")
	}

	git, err := NewGit()
	if err != nil {
		return fmt.Errorf("failed to initialize git: %w", err)
	}

	editor := opts.Editor
	if editor == nil {
		editor = NewSystemEditor()
	}

	state, err := NewFileStore()
	if err != nil {
		return fmt.Errorf("failed to initialize state manager: %w", err)
	}

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

	fmt.Printf("Found %d commits to rebranch from %s onto %s\n", len(commits), sourceBranch, baseBranch)
	for i, commit := range commits {
		fmt.Printf("  %d. %s %s\n", i+1, commit.SHA[:7], commit.Message)
	}

	// Create and edit interactive file
	pickFilePath := GetPickFilePath(git.GetRepoPath())
	if err := CreateInteractiveFile(commits, pickFilePath); err != nil {
		return fmt.Errorf("failed to create pick file: %w", err)
	}

	fmt.Printf("\nEdit the commit list and save to continue...\n")
	if err := editor.LaunchEditor(pickFilePath); err != nil {
		return fmt.Errorf("failed to launch editor: %w", err)
	}

	// Parse edited file
	selectedCommits, err := ParseInteractiveFile(pickFilePath, commits)
	if err != nil {
		return fmt.Errorf("failed to parse pick file: %w", err)
	}

	fmt.Printf("\nSelected %d commits to apply\n", countPickedCommits(selectedCommits))

	// Create temporary branch
	tempBranch := fmt.Sprintf("%s%d", TempBranchPrefix, time.Now().Unix())
	if err := git.CreateBranch(tempBranch, baseBranch); err != nil {
		return err
	}

	if err := git.CheckoutBranch(tempBranch); err != nil {
		return err
	}

	// Save initial state with selected commits
	state := &RebranchState{
		SourceBranch:     sourceBranch,
		BaseBranch:       baseBranch,
		TempBranch:       tempBranch,
		Stage:            "picking",
		CommitsToApply:   selectedCommits,
		CurrentCommitIdx: 0,
	}

	if err := store.SaveState(state); err != nil {
		return err
	}

	// Start cherry-picking
	return ApplyCherryPicks(git, store, state)
}

// continueRebranch resumes after conflict resolution
func continueRebranch(git GitInterface, state Store) error {
	if err := validateContinue(git, state); err != nil {
		return err
	}

	rebranchState, err := state.LoadState()
	if err != nil {
		return err
	}

	rebranchState.CurrentCommitIdx++ // Move to next commit
	rebranchState.Stage = "picking"

	return ApplyCherryPicks(git, state, rebranchState)
}

// ApplyCherryPicks applies remaining commits from current index
func ApplyCherryPicks(git GitInterface, store Store, state *RebranchState) error {
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
			return fmt.Errorf("conflict during cherry-pick of %s\n"+
				"Resolve conflicts and run: rebranch --continue", commit.SHA[:7])
		}

		state.CurrentCommitIdx = i
		if err := store.SaveState(state); err != nil {
			return err
		}
	}

	// All commits applied successfully
	fmt.Printf("Successfully applied %d commits to %s\n",
		countPickedCommits(state.CommitsToApply), state.TempBranch)
	fmt.Printf("Review the new branch history and run: rebranch --done\n")

	state.Stage = "done"
	return store.SaveState(state)
}

// finishRebranch completes the rebranch by replacing original branch
func finishRebranch(git GitInterface, store Store) error {
	// Validate preconditions
	if err := validateFinish(git, store); err != nil {
		return err
	}

	state, err := store.LoadState()
	if err != nil {
		return err
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
	// Validate preconditions
	if err := validateAbort(git, store); err != nil {
		return err
	}

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

// countPickedCommits counts commits with "pick" action
func countPickedCommits(commits []CommitInfo) int {
	count := 0
	for _, commit := range commits {
		if commit.Action == "pick" {
			count++
		}
	}
	return count
}
