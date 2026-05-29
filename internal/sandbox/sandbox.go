package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateRepo confirms workspaceRoot/repoName exists and contains .git.
// Returns the resolved absolute path to the repo.
func ValidateRepo(workspaceRoot, repoName string) (string, error) {
	repoPath := filepath.Join(workspaceRoot, repoName)
	resolved, err := filepath.EvalSymlinks(repoPath)
	if err != nil {
		return "", fmt.Errorf("repo %q not found", repoName)
	}
	if !isUnder(workspaceRoot, resolved) {
		return "", fmt.Errorf("repo %q resolves outside workspace", repoName)
	}
	if _, err := os.Stat(filepath.Join(resolved, ".git")); err != nil {
		return "", fmt.Errorf("%q is not a git repository", repoName)
	}
	return resolved, nil
}

// ValidatePath confirms that relPath within repoPath does not escape the workspace.
// The file need not exist on disk (e.g. for git show of a historical path).
func ValidatePath(workspaceRoot, repoPath, relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("path must be relative, got %q", relPath)
	}
	abs := filepath.Join(repoPath, relPath)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File may not exist (historical ref) — use cleaned path.
		resolved = filepath.Clean(abs)
	}
	if !isUnder(workspaceRoot, resolved) {
		return "", fmt.Errorf("path %q escapes workspace", relPath)
	}
	return resolved, nil
}

func isUnder(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}
