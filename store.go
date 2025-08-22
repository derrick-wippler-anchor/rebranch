package rebranch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Store handles persistent state storage
type Store interface {
	SaveState(state *RebranchState) error
	LoadState() (*RebranchState, error)
	ClearState() error
	StateExists() bool
}

// FileStore implements Store using filesystem storage
type FileStore struct {
	stateFilePath string
}

// NewFileStore creates a new Store
func NewFileStore() (Store, error) {
	// Find .git directory
	gitDir, err := findGitDir()
	if err != nil {
		return nil, fmt.Errorf("failed to find .git directory: %w", err)
	}

	stateFilePath := filepath.Join(gitDir, StateFileName)
	return &FileStore{
		stateFilePath: stateFilePath,
	}, nil
}

// NewFileStoreInPath creates a new Store for a specific repository path
func NewFileStoreInPath(repoPath string) (Store, error) {
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("not a git repository: %s", repoPath)
	}

	stateFilePath := filepath.Join(gitDir, StateFileName)
	return &FileStore{
		stateFilePath: stateFilePath,
	}, nil
}

func (f *FileStore) SaveState(state *RebranchState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	err = os.WriteFile(f.stateFilePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

func (f *FileStore) LoadState() (*RebranchState, error) {
	data, err := os.ReadFile(f.stateFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state RebranchState
	err = json.Unmarshal(data, &state)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

func (f *FileStore) ClearState() error {
	if !f.StateExists() {
		return nil // Nothing to clear
	}

	err := os.Remove(f.stateFilePath)
	if err != nil {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}

func (f *FileStore) StateExists() bool {
	_, err := os.Stat(f.stateFilePath)
	return err == nil
}

// findGitDir finds the .git directory starting from current directory
func findGitDir() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Walk up the directory tree looking for .git
	for {
		gitDir := filepath.Join(currentDir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return gitDir, nil
		}

		parent := filepath.Dir(currentDir)
		if parent == currentDir {
			// Reached root directory
			break
		}
		currentDir = parent
	}

	return "", fmt.Errorf(".git directory not found")
}
