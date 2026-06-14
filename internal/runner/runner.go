package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const DefaultTimeout = 60 * time.Second

var gitEnv = []string{
	"GIT_OPTIONAL_LOCKS=0",
	"GIT_TERMINAL_PROMPT=0",
}

// RunGit runs git with the given args in repoPath.
func RunGit(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), gitEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %v: %w: %s", args, err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// RunGitDiffNoIndex runs git diff --no-index /dev/null <relPath> in repoPath.
// It allows exit code 1 (which git diff --no-index returns when differences exist).
func RunGitDiffNoIndex(ctx context.Context, repoPath string, relPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "/dev/null", relPath)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), gitEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return stdout.Bytes(), nil
		}
		return nil, fmt.Errorf("git diff --no-index: %w: %s", err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}


// RunRipgrep runs rg with the given args in searchPath.
// Exit code 1 (no matches) is treated as success with empty output.
func RunRipgrep(ctx context.Context, searchPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = searchPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return stdout.Bytes(), nil
		}
		return nil, fmt.Errorf("rg: %w: %s", err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// RunCommand runs a specific external command (e.g. jv) with the default timeout.
func RunCommand(ctx context.Context, command string, args ...string) ([]byte, error) {
	return RunCommandWithTimeout(ctx, DefaultTimeout, "", command, args...)
}

// RunCommandWithTimeout runs an external command with a caller-specified timeout.
// If dir is non-empty, the command runs in that directory.
func RunCommandWithTimeout(ctx context.Context, timeout time.Duration, dir, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", command, err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}
