package rebranch

import (
	"errors"
	"fmt"
)

// validateStart performs pre-flight checks before starting a rebranch operation
func validateStart(baseBranch string, git GitInterface, state Store) error {
	// Check if repository is valid
	if err := git.IsValidRepository(); err != nil {
		return fmt.Errorf("invalid repository: %w", err)
	}

	// Check if there's already an ongoing rebranch operation
	if state.StateExists() {
		return errors.New("rebranch operation already in progress\n" +
			"\n" +
			"Available actions:\n" +
			"  • Continue: rebranch --continue (after resolving conflicts)\n" +
			"  • Complete: rebranch --done (if cherry-picking finished)\n" +
			"  • Cancel: rebranch --abort (revert to original state)")
	}

	// Check for other ongoing git operations
	hasOp, opType, err := git.HasOngoingOperation()
	if err != nil {
		return fmt.Errorf("failed to check for ongoing operations: %w", err)
	}
	if hasOp {
		return fmt.Errorf("cannot start rebranch: %s operation is in progress\n"+
			"\n"+
			"Please complete the ongoing %s operation first:\n"+
			"  • View status: git status\n"+
			"  • Complete or abort the current operation\n"+
			"  • Then retry rebranch", opType, opType)
	}

	// Check if working directory is clean
	isClean, err := git.IsCleanWorkingDirectory()
	if err != nil {
		return fmt.Errorf("failed to check working directory status: %w", err)
	}
	if !isClean {
		return errors.New("working directory is not clean\n" +
			"\n" +
			"Please resolve before rebranching:\n" +
			"  • Commit changes: git add . && git commit -m \"Your message\"\n" +
			"  • Or stash changes: git stash\n" +
			"  • Check status: git status")
	}

	// Check if base branch exists
	if !git.BranchExists(baseBranch) {
		return fmt.Errorf("base branch '%s' does not exist\n"+
			"\n"+
			"Suggestions:\n"+
			"  • Check branch name spelling\n"+
			"  • Run 'git branch -a' to see all available branches\n"+
			"  • Create the branch: git checkout -b %s", baseBranch, baseBranch)
	}

	// Get current branch
	currentBranch, err := git.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Check if current branch is different from base branch
	if currentBranch == baseBranch {
		return fmt.Errorf("current branch '%s' is the same as base branch '%s'\n"+
			"\n"+
			"Suggestions:\n"+
			"  • Create a feature branch: git checkout -b feature-branch\n"+
			"  • Or switch to a different branch: git checkout <branch-name>", 
			currentBranch, baseBranch)
	}

	return nil
}

// validateContinue performs checks before continuing a rebranch operation
func validateContinue(git GitInterface, state Store) error {
	// Check if repository is valid
	if err := git.IsValidRepository(); err != nil {
		return fmt.Errorf("invalid repository: %w", err)
	}

	// Check if there's a rebranch operation in progress
	if !state.StateExists() {
		return errors.New("no rebranch operation in progress")
	}

	// Load state to check stage
	rebranchState, err := state.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load rebranch state: %w", err)
	}

	// Only allow continue if we're in conflicts stage
	if rebranchState.Stage != "conflicts" {
		return fmt.Errorf("rebranch is not waiting for conflict resolution (current stage: %s)", rebranchState.Stage)
	}

	// Check if working directory is clean (conflicts should be resolved)
	isClean, err := git.IsCleanWorkingDirectory()
	if err != nil {
		return fmt.Errorf("failed to check working directory status: %w", err)
	}
	if !isClean {
		return errors.New("working directory is not clean. Please resolve conflicts and stage changes before continuing")
	}

	return nil
}

// validateFinish performs checks before finishing a rebranch operation
func validateFinish(git GitInterface, state Store) error {
	// Check if repository is valid
	if err := git.IsValidRepository(); err != nil {
		return fmt.Errorf("invalid repository: %w", err)
	}

	// Check if there's a rebranch operation in progress
	if !state.StateExists() {
		return errors.New("no rebranch operation in progress")
	}

	// Load state to check stage
	rebranchState, err := state.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load rebranch state: %w", err)
	}

	// Only allow finish if we're in done stage
	if rebranchState.Stage != "done" {
		return fmt.Errorf("rebranch is not ready to finish (current stage: %s). Run rebranch --continue first", rebranchState.Stage)
	}

	// Verify we're on the temp branch
	currentBranch, err := git.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if currentBranch != rebranchState.TempBranch {
		return fmt.Errorf("expected to be on temp branch '%s', but on '%s'", rebranchState.TempBranch, currentBranch)
	}

	// Check if working directory is clean
	isClean, err := git.IsCleanWorkingDirectory()
	if err != nil {
		return fmt.Errorf("failed to check working directory status: %w", err)
	}
	if !isClean {
		return errors.New("working directory is not clean. Please commit any remaining changes before finishing")
	}

	return nil
}

// validateAbort performs checks before aborting a rebranch operation
func validateAbort(git GitInterface, state Store) error {
	// Check if repository is valid
	if err := git.IsValidRepository(); err != nil {
		return fmt.Errorf("invalid repository: %w", err)
	}

	// Check if there's a rebranch operation in progress
	if !state.StateExists() {
		return errors.New("no rebranch operation in progress")
	}

	return nil
}
