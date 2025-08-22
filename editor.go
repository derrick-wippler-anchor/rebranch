package rebranch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EditorInterface handles launching external editors
type EditorInterface interface {
	LaunchEditor(filepath string) error
}

// SystemEditor implements EditorInterface using system $EDITOR
type SystemEditor struct{}

// NewSystemEditor creates a new SystemEditor instance
func NewSystemEditor() EditorInterface {
	return &SystemEditor{}
}

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

// CreateInteractiveFile creates the pick file for interactive editing
func CreateInteractiveFile(commits []CommitInfo, filePath string) error {
	var lines []string
	
	// Add header comment
	lines = append(lines, "# Interactive rebranch - Edit the list of commits to apply")
	lines = append(lines, "# Commands:")
	lines = append(lines, "#  pick, p = apply this commit")
	lines = append(lines, "#  drop, d = skip this commit")
	lines = append(lines, "#")
	lines = append(lines, "# Lines starting with # are ignored.")
	lines = append(lines, "")

	// Add commits
	for _, commit := range commits {
		shortSHA := commit.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		line := fmt.Sprintf("pick %s %s", shortSHA, commit.Message)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(filePath, []byte(content), 0644)
}

// ParseInteractiveFile parses the edited pick file and returns selected commits
func ParseInteractiveFile(filePath string, originalCommits []CommitInfo) ([]CommitInfo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read pick file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var selectedCommits []CommitInfo
	commitMap := make(map[string]CommitInfo)

	// Create map for quick lookup
	for _, commit := range originalCommits {
		shortSHA := commit.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		commitMap[shortSHA] = commit
	}

	lineNum := 0
	for _, line := range lines {
		lineNum++
		line = strings.TrimSpace(line)
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse action and commit
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid line %d: %s", lineNum, line)
		}

		action := parts[0]
		shortSHA := parts[1]

		// Normalize action (support abbreviations)
		switch action {
		case "pick", "p":
			action = "pick"
		case "drop", "d":
			action = "drop"
		default:
			return nil, fmt.Errorf("invalid action '%s' on line %d (must be 'pick', 'p', 'drop', or 'd')", action, lineNum)
		}

		// Find original commit
		originalCommit, exists := commitMap[shortSHA]
		if !exists {
			return nil, fmt.Errorf("unknown commit %s on line %d", shortSHA, lineNum)
		}

		// Add to selected commits with updated action
		commit := CommitInfo{
			SHA:     originalCommit.SHA,
			Message: originalCommit.Message,
			Action:  action,
		}
		selectedCommits = append(selectedCommits, commit)
	}

	if len(selectedCommits) == 0 {
		return nil, fmt.Errorf("no commits selected (all lines were comments or invalid)")
	}

	return selectedCommits, nil
}

// GetPickFilePath returns the path to the interactive pick file
func GetPickFilePath(repoPath string) string {
	return filepath.Join(repoPath, ".git", PickFileName)
}