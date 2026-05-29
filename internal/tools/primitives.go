package tools

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"repo-lens/internal/runner"
	"repo-lens/internal/sandbox"
)

// skippedDirs are directory names the workspace walker will not descend into.
var skippedDirs = map[string]bool{
	"node_modules": true,
	"_build":       true,
	"deps":         true,
	".git":         true,
	"vendor":       true,
}

// discoverRepos returns all git repositories found under root.
// It does not recurse into found repos, but does continue past the root itself
// so that repos nested directly under root are all discovered.
func (h *handlers) discoverRepos() []struct{ name, path string } {
	var repos []struct{ name, path string }
	_ = filepath.WalkDir(h.workspace, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != h.workspace && skippedDirs[d.Name()] {
			return filepath.SkipDir
		}
		if _, serr := os.Stat(filepath.Join(p, ".git")); serr == nil {
			name, _ := filepath.Rel(h.workspace, p)
			repos = append(repos, struct{ name, path string }{name, p})
			if p == h.workspace {
				return nil
			}
			return filepath.SkipDir
		}
		return nil
	})
	return repos
}

// listRepos walks h.workspace and returns metadata for every git repository found.
func (h *handlers) listRepos(ctx context.Context, _ *mcp.CallToolRequest, _ ListReposInput) (*mcp.CallToolResult, ListReposOutput, error) {
	discovered := h.discoverRepos()
	repos := make([]RepoInfo, 0, len(discovered))

	for _, r := range discovered {
		info := RepoInfo{Name: r.name, Path: r.path}

		if out, err := runner.RunGit(ctx, r.path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			info.Branch = strings.TrimSpace(string(out))
		}
		if out, err := runner.RunGit(ctx, r.path, "log", "-1", "--format=%aI"); err == nil {
			info.LastCommitDate = strings.TrimSpace(string(out))
		}
		if out, err := runner.RunGit(ctx, r.path, "remote", "get-url", "origin"); err == nil {
			info.RemoteURL = strings.TrimSpace(string(out))
		}

		repos = append(repos, info)
	}

	return nil, ListReposOutput{Repos: repos}, nil
}

// search runs ripgrep against a validated repository and returns paginated results.
func (h *handlers) search(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, SearchOutput{}, err
	}

	// Build rg argument list.
	args := []string{
		"--line-number",
		"--with-filename",
		"--no-heading",
		"--color=never",
	}
	if !input.Regex {
		args = append(args, "--fixed-strings")
	}
	for _, inc := range input.Includes {
		args = append(args, "-g", inc)
	}
	args = append(args, input.Query)

	out, err := runner.RunRipgrep(ctx, repoPath, args...)
	if err != nil {
		return nil, SearchOutput{}, err
	}

	// Parse rg output: each non-empty line is "file:linenum:content".
	lines := nonEmpty(splitLines(string(out)))
	var all []SearchResult
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		lineNum, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		// Make file path relative to repoPath.
		filePath := parts[0]
		if rel, err := filepath.Rel(repoPath, filepath.Join(repoPath, filePath)); err == nil {
			filePath = rel
		}
		all = append(all, SearchResult{
			File:    filePath,
			Line:    lineNum,
			Content: parts[2],
		})
	}

	// Apply offset.
	offset := input.Offset
	if offset > len(all) {
		offset = len(all)
	}
	all = all[offset:]

	// Apply maxResults (default 50).
	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	var omitted int
	if len(all) > maxResults {
		omitted = len(all) - maxResults
		all = all[:maxResults]
	}

	if all == nil {
		all = []SearchResult{}
	}
	return nil, SearchOutput{Results: all, Omitted: omitted}, nil
}

// readFileAtRef reads a file from git history at the given ref and returns a
// (possibly windowed and truncated) view of its contents.
func (h *handlers) readFileAtRef(ctx context.Context, _ *mcp.CallToolRequest, input ReadFileAtRefInput) (*mcp.CallToolResult, ReadFileAtRefOutput, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, ReadFileAtRefOutput{}, err
	}

	// Security check: validate the path doesn't escape the workspace.
	if _, err := sandbox.ValidatePath(h.workspace, repoPath, input.Path); err != nil {
		return nil, ReadFileAtRefOutput{}, err
	}

	ref := input.Ref
	if ref == "" {
		ref = "HEAD"
	}

	raw, err := runner.RunGit(ctx, repoPath, "show", ref+":"+input.Path)
	if err != nil {
		return nil, ReadFileAtRefOutput{}, err
	}

	lines := splitLines(string(raw))
	totalLines := len(lines)

	// Compute 0-based slice indices from 1-based StartLine/EndLine.
	startIdx := 0
	if input.StartLine > 0 {
		startIdx = input.StartLine - 1
	}
	endIdx := totalLines - 1
	if input.EndLine > 0 {
		endIdx = input.EndLine - 1
	}

	// Clamp to valid range.
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= totalLines {
		startIdx = totalLines
	}
	if endIdx >= totalLines {
		endIdx = totalLines - 1
	}
	if endIdx < startIdx {
		endIdx = startIdx - 1
	}

	var window []string
	if startIdx <= endIdx && startIdx < totalLines {
		window = lines[startIdx : endIdx+1]
	}

	content := strings.Join(window, "\n")
	if len(window) > 0 {
		content += "\n"
	}

	var omitted *Omitted
	if len(content) > maxOutputBytes {
		// Truncate to maxOutputBytes while preserving whole lines.
		truncated := content[:maxOutputBytes]
		// Walk back to the last newline so we don't split a line.
		if idx := strings.LastIndex(truncated, "\n"); idx >= 0 {
			truncated = truncated[:idx+1]
		}
		shownLines := len(splitLines(truncated))
		omitted = &Omitted{
			Truncated:  true,
			TotalLines: totalLines,
			ShownLines: shownLines,
			Reason:     "content exceeds 100 KB limit",
		}
		content = truncated
	}

	return nil, ReadFileAtRefOutput{
		Content:    content,
		TotalLines: totalLines,
		Ref:        ref,
		Omitted:    omitted,
	}, nil
}
