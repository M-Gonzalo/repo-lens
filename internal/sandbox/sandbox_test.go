package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// setupWorkspace creates a workspace directory and returns its path.
// It also creates a valid repo subdirectory with a .git dir.
// On macOS, t.TempDir() returns a path under /var/... which is a symlink to
// /private/var/...; we resolve it up-front so isUnder comparisons work.
func setupWorkspace(t *testing.T) (workspace, validRepo string) {
	t.Helper()
	raw := t.TempDir()
	resolved, err := filepath.EvalSymlinks(raw)
	if err != nil {
		t.Fatalf("failed to resolve workspace symlinks: %v", err)
	}
	workspace = resolved
	validRepo = filepath.Join(workspace, "myrepo")
	if err := os.MkdirAll(filepath.Join(validRepo, ".git"), 0o755); err != nil {
		t.Fatalf("failed to create valid repo: %v", err)
	}
	return workspace, validRepo
}

// --- ValidateRepo tests ---

func TestValidateRepo_ValidRepo(t *testing.T) {
	workspace, _ := setupWorkspace(t)

	got, err := ValidateRepo(workspace, "myrepo")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// workspace is already symlink-resolved, so the join is the canonical path.
	want := filepath.Join(workspace, "myrepo")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValidateRepo_RepoNotFound(t *testing.T) {
	workspace, _ := setupWorkspace(t)

	_, err := ValidateRepo(workspace, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing repo, got nil")
	}
}

func TestValidateRepo_NoGitDir(t *testing.T) {
	workspace, _ := setupWorkspace(t)
	// Create a directory without a .git subdirectory.
	noGit := filepath.Join(workspace, "notarepo")
	if err := os.MkdirAll(noGit, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	_, err := ValidateRepo(workspace, "notarepo")
	if err == nil {
		t.Fatal("expected error for repo without .git, got nil")
	}
}

func TestValidateRepo_TraversalEscapesWorkspace(t *testing.T) {
	workspace, _ := setupWorkspace(t)

	// "../<something>" should resolve outside workspace.
	_, err := ValidateRepo(workspace, "../outside")
	if err == nil {
		t.Fatal("expected error for path escaping workspace, got nil")
	}
}

func TestValidateRepo_SymlinkOutsideWorkspace(t *testing.T) {
	workspace, _ := setupWorkspace(t)

	// Create a real repo outside the workspace (resolve symlinks for macOS).
	rawOutside := t.TempDir()
	outside, err := filepath.EvalSymlinks(rawOutside)
	if err != nil {
		t.Fatalf("failed to resolve outside dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outside, ".git"), 0o755); err != nil {
		t.Fatalf("failed to create outside repo: %v", err)
	}

	// Symlink inside workspace → outside workspace.
	link := filepath.Join(workspace, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("cannot create symlinks on this system, skipping symlink test")
	}

	_, err = ValidateRepo(workspace, "escape-link")
	if err == nil {
		t.Fatal("expected error for symlink escaping workspace, got nil")
	}
}

// --- ValidatePath tests ---

func TestValidatePath_ValidRelative(t *testing.T) {
	workspace, validRepo := setupWorkspace(t)

	// Create a real file inside the repo.
	filePath := filepath.Join(validRepo, "README.md")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	got, err := ValidatePath(workspace, validRepo, "README.md")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// workspace / validRepo are already symlink-resolved, so the join is canonical.
	if got != filePath {
		t.Errorf("got %q, want %q", got, filePath)
	}
}

func TestValidatePath_TraversalStaysInWorkspace(t *testing.T) {
	workspace, validRepo := setupWorkspace(t)

	// Create a sibling repo directory inside the workspace.
	sibling := filepath.Join(workspace, "sibling")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("failed to create sibling dir: %v", err)
	}
	siblingFile := filepath.Join(sibling, "file.txt")
	if err := os.WriteFile(siblingFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to create sibling file: %v", err)
	}

	// ../sibling/file.txt escapes the repo but stays in workspace — should be allowed.
	_, err := ValidatePath(workspace, validRepo, "../sibling/file.txt")
	if err != nil {
		t.Fatalf("expected no error for path within workspace, got %v", err)
	}
}

func TestValidatePath_TraversalEscapesWorkspace(t *testing.T) {
	workspace, validRepo := setupWorkspace(t)

	// Enough ".." segments to climb above the workspace.
	_, err := ValidatePath(workspace, validRepo, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path escaping workspace, got nil")
	}
}

func TestValidatePath_AbsolutePathRejected(t *testing.T) {
	workspace, validRepo := setupWorkspace(t)

	_, err := ValidatePath(workspace, validRepo, "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestValidatePath_NonExistentFileAllowed(t *testing.T) {
	workspace, validRepo := setupWorkspace(t)

	// A file that doesn't exist on disk (e.g. historical git path) should succeed
	// because ValidatePath falls back to filepath.Clean.
	_, err := ValidatePath(workspace, validRepo, "some/historic/file.go")
	if err != nil {
		t.Fatalf("expected no error for non-existent historical path, got %v", err)
	}
}

func TestValidatePath_SymlinkEscapingWorkspace(t *testing.T) {
	workspace, validRepo := setupWorkspace(t)

	// Create a symlink inside the repo that points outside the workspace.
	rawOutside := t.TempDir()
	outside, err := filepath.EvalSymlinks(rawOutside)
	if err != nil {
		t.Fatalf("failed to resolve outside dir: %v", err)
	}
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}
	link := filepath.Join(validRepo, "escape")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skip("cannot create symlinks on this system, skipping symlink test")
	}

	_, err = ValidatePath(workspace, validRepo, "escape")
	if err == nil {
		t.Fatal("expected error for symlink escaping workspace, got nil")
	}
}
