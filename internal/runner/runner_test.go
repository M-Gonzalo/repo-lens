package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// initGitRepo initialises a bare-minimum git repo in dir so git commands work.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
}

// --- RunGit tests ---

func TestRunGit_ValidCommand(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Create an initial commit so rev-parse HEAD works.
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = dir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	commitCmd := exec.Command("git", "commit", "-m", "init")
	commitCmd.Dir = dir
	// Env needed so commit doesn't fail with missing identity in CI.
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@t.com",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}

	out, err := RunGit(context.Background(), dir, "log", "--oneline")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output from git log")
	}
}

func TestRunGit_InvalidCommand(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	_, err := RunGit(context.Background(), dir, "not-a-real-subcommand")
	if err == nil {
		t.Fatal("expected error for invalid git subcommand, got nil")
	}
}

func TestRunGit_CancelledContext(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := RunGit(ctx, dir, "status")
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestRunGit_SetsGitOptionalLocks(t *testing.T) {
	// Verify that gitEnv includes GIT_OPTIONAL_LOCKS=0 so that the variable
	// is present in the environment we build for subprocesses.
	found := false
	for _, kv := range gitEnv {
		if kv == "GIT_OPTIONAL_LOCKS=0" {
			found = true
			break
		}
	}
	if !found {
		t.Error("gitEnv does not contain GIT_OPTIONAL_LOCKS=0")
	}
}

func TestRunGit_SetsGitTerminalPrompt(t *testing.T) {
	found := false
	for _, kv := range gitEnv {
		if kv == "GIT_TERMINAL_PROMPT=0" {
			found = true
			break
		}
	}
	if !found {
		t.Error("gitEnv does not contain GIT_TERMINAL_PROMPT=0")
	}
}

// --- RunRipgrep tests ---

func TestRunRipgrep_NoMatchesReturnsEmpty(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH, skipping ripgrep tests")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	// Search for a string that definitely doesn't appear → rg exits 1.
	out, err := RunRipgrep(context.Background(), dir, "ZZZNOMATCHZZZ", ".")
	if err != nil {
		t.Fatalf("expected nil error for no-match exit code 1, got %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output for no-match, got %q", out)
	}
}

func TestRunRipgrep_MatchReturnsOutput(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH, skipping ripgrep tests")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	out, err := RunRipgrep(context.Background(), dir, "hello", ".")
	if err != nil {
		t.Fatalf("expected no error for matching search, got %v", err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output for matching search")
	}
}

// --- RunCommand tests ---

func TestRunCommand_ValidCommand(t *testing.T) {
	out, err := RunCommand(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output from echo")
	}
}

func TestRunCommand_InvalidCommand(t *testing.T) {
	_, err := RunCommand(context.Background(), "this-command-does-not-exist-xyz")
	if err == nil {
		t.Fatal("expected error for non-existent command, got nil")
	}
}

func TestRunCommand_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the command runs

	_, err := RunCommand(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

// --- RunCommandWithTimeout tests ---

func TestRunCommandWithTimeout_CustomTimeout(t *testing.T) {
	out, err := RunCommandWithTimeout(context.Background(), 5*time.Second, "", "echo", "custom-timeout")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output")
	}
}

func TestRunCommandWithTimeout_ExceedsTimeout(t *testing.T) {
	_, err := RunCommandWithTimeout(context.Background(), 100*time.Millisecond, "", "sleep", "10")
	if err == nil {
		t.Fatal("expected error when command exceeds timeout, got nil")
	}
}
