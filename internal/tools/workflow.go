package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"repo-lens/internal/runner"
	"repo-lens/internal/sandbox"
)

//go:embed researcher_agent.md
var researcherAgentMD []byte

func (h *handlers) resolveJiraTag(ctx context.Context, _ *mcp.CallToolRequest, input ResolveJiraTagInput) (*mcp.CallToolResult, ResolveJiraTagOutput, error) {
	if input.Tag == "" {
		return nil, ResolveJiraTagOutput{}, fmt.Errorf("tag is required")
	}

	var repoPaths []struct{ name, path string }

	if input.Repo != "" {
		repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
		if err != nil {
			return nil, ResolveJiraTagOutput{}, err
		}
		repoPaths = append(repoPaths, struct{ name, path string }{input.Repo, repoPath})
	} else {
		repoPaths = h.discoverRepos()
	}

	var matches []JiraTagMatch
	for _, repo := range repoPaths {
		out, err := runner.RunGit(ctx, repo.path,
			"log", "--all",
			"--format=format:\x1e%H\x1f%an\x1f%aI\x1f%s\x1f%D",
			"--grep="+input.Tag,
		)
		if err != nil {
			continue
		}
		chunks := strings.Split(string(out), "\x1e")
		for _, chunk := range chunks {
			chunk = strings.TrimSpace(chunk)
			if chunk == "" {
				continue
			}
			parts := strings.Split(chunk, "\x1f")
			if len(parts) < 4 {
				continue
			}
			branch := ""
			if len(parts) >= 5 {
				branch = parseBranchFromDecoration(parts[4])
			}
			matches = append(matches, JiraTagMatch{
				Repo:    repo.name,
				Hash:    strings.TrimSpace(parts[0]),
				Author:  strings.TrimSpace(parts[1]),
				Date:    strings.TrimSpace(parts[2]),
				Message: strings.TrimSpace(parts[3]),
				Branch:  branch,
			})
		}
	}

	if matches == nil {
		matches = []JiraTagMatch{}
	}
	return nil, ResolveJiraTagOutput{Matches: matches}, nil
}

func parseBranchFromDecoration(decoration string) string {
	// %D format: "HEAD -> main, origin/main" or "origin/feat/foo" or "tag: v1.0"
	decoration = strings.TrimSpace(decoration)
	if decoration == "" {
		return ""
	}
	for _, part := range strings.Split(decoration, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "HEAD -> ") {
			return strings.TrimPrefix(part, "HEAD -> ")
		}
	}
	// Return first non-HEAD part
	for _, part := range strings.Split(decoration, ",") {
		part = strings.TrimSpace(part)
		if part != "HEAD" && !strings.HasPrefix(part, "tag: ") {
			return part
		}
	}
	return ""
}

func (h *handlers) collectReviewBundle(ctx context.Context, _ *mcp.CallToolRequest, input CollectReviewBundleInput) (*mcp.CallToolResult, CollectReviewBundleOutput, error) {
	if input.JiraTag == "" && input.Commit == "" {
		return nil, CollectReviewBundleOutput{}, fmt.Errorf("one of jiraTag or commit is required")
	}
	if input.JiraTag != "" && input.Commit != "" {
		return nil, CollectReviewBundleOutput{}, fmt.Errorf("provide jiraTag or commit, not both")
	}

	// Collect commit hashes grouped by repo.
	// Each entry: {repo name, repo path, []commit hashes}
	type repoCommits struct {
		name, path string
		hashes     []string
	}
	var targets []repoCommits

	if input.JiraTag != "" {
		// Find all commits mentioning the tag across repos.
		_, jiraOut, err := h.resolveJiraTag(ctx, nil, ResolveJiraTagInput{
			Tag:  input.JiraTag,
			Repo: input.Repo,
		})
		if err != nil {
			return nil, CollectReviewBundleOutput{}, fmt.Errorf("resolveJiraTag: %w", err)
		}
		// Group matches by repo.
		grouped := map[string]*repoCommits{}
		for _, m := range jiraOut.Matches {
			rc, ok := grouped[m.Repo]
			if !ok {
				repoPath, err := sandbox.ValidateRepo(h.workspace, m.Repo)
				if err != nil {
					continue
				}
				rc = &repoCommits{name: m.Repo, path: repoPath}
				grouped[m.Repo] = rc
			}
			rc.hashes = append(rc.hashes, m.Hash)
		}
		for _, rc := range grouped {
			targets = append(targets, *rc)
		}
	} else {
		// Single commit mode.
		if input.Repo != "" {
			repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
			if err != nil {
				return nil, CollectReviewBundleOutput{}, err
			}
			targets = append(targets, repoCommits{
				name: input.Repo, path: repoPath, hashes: []string{input.Commit},
			})
		} else {
			// Search all repos for the commit.
			for _, r := range h.discoverRepos() {
				// Check if the commit exists in this repo.
				if _, err := runner.RunGit(ctx, r.path, "cat-file", "-t", input.Commit); err == nil {
					targets = append(targets, repoCommits{
						name: r.name, path: r.path, hashes: []string{input.Commit},
					})
				}
			}
		}
	}

	// Build the bundle: gitShow each commit.
	var out CollectReviewBundleOutput
	for _, rc := range targets {
		bundle := RepoBundle{Repo: rc.name}
		for _, hash := range rc.hashes {
			detail := h.showCommitDetail(ctx, rc.path, hash)
			out.Summary.TotalCommits++
			out.Summary.FilesChanged += detail.Stats.FilesChanged
			out.Summary.Insertions += detail.Stats.Insertions
			out.Summary.Deletions += detail.Stats.Deletions
			bundle.Commits = append(bundle.Commits, detail)
		}
		out.Repos = append(out.Repos, bundle)
	}

	// Jira metadata.
	if input.JiraTag != "" {
		jvOut, jvErr := runner.RunCommand(ctx, "jv", input.JiraTag)
		if jvErr != nil {
			out.Jira = &JiraInfo{Tag: input.JiraTag, Error: jvErr.Error()}
		} else {
			out.Jira = &JiraInfo{Tag: input.JiraTag, Output: stripANSI(string(jvOut))}
		}
	}

	if input.Format == "json" {
		return nil, out, nil
	}

	markdownStr := renderMarkdown(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: markdownStr},
		},
	}, CollectReviewBundleOutput{}, nil
}

func renderMarkdown(out CollectReviewBundleOutput) string {
	var sb strings.Builder

	// Count repositories with commits
	activeRepos := 0
	for _, rb := range out.Repos {
		if len(rb.Commits) > 0 {
			activeRepos++
		}
	}

	sb.WriteString("# Review Bundle\n\n")

	// Summary
	sb.WriteString("## Summary\n")
	sb.WriteString(fmt.Sprintf("%d commits across %d repos | +%d / -%d | %d files changed\n\n",
		out.Summary.TotalCommits,
		activeRepos,
		out.Summary.Insertions,
		out.Summary.Deletions,
		out.Summary.FilesChanged,
	))

	// Jira
	if out.Jira != nil {
		sb.WriteString(fmt.Sprintf("## Jira Ticket: %s\n", out.Jira.Tag))
		if out.Jira.Error != "" {
			sb.WriteString(fmt.Sprintf("Error fetching ticket: %s\n\n", out.Jira.Error))
		} else {
			sb.WriteString(out.Jira.Output)
			sb.WriteString("\n\n")
		}
	}

	// Repositories
	for _, rb := range out.Repos {
		if len(rb.Commits) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s (%d commit%s)\n", rb.Repo, len(rb.Commits), plural(len(rb.Commits))))
		for _, commit := range rb.Commits {
			shortHash := commit.Hash
			if len(shortHash) > 7 {
				shortHash = shortHash[:7]
			}
			sb.WriteString(fmt.Sprintf("### %s — %s\n", shortHash, commit.Message))
			sb.WriteString(fmt.Sprintf("*Author: %s | Date: %s*\n\n", commit.Author, commit.Date))

			if commit.Omitted != nil && commit.Omitted.Truncated {
				sb.WriteString(fmt.Sprintf("> [!WARNING]\n> Diff truncated: %s\n", commit.Omitted.Reason))
				if len(commit.Omitted.AffectedFiles) > 0 {
					sb.WriteString("> Files affected:\n")
					for _, f := range commit.Omitted.AffectedFiles {
						sb.WriteString(fmt.Sprintf("> - %s\n", f))
					}
				}
				sb.WriteString("\n")
			}

			if commit.Diff != "" {
				sb.WriteString("```diff\n")
				sb.WriteString(commit.Diff)
				if !strings.HasSuffix(commit.Diff, "\n") {
					sb.WriteString("\n")
				}
				sb.WriteString("```\n\n")
			}
		}
	}

	return sb.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// showCommitDetail runs git show on a single commit and returns structured details.
func (h *handlers) showCommitDetail(ctx context.Context, repoPath, ref string) ReviewCommitDetail {
	detail := ReviewCommitDetail{Hash: ref}

	// Metadata.
	metaOut, err := runner.RunGit(ctx, repoPath, "show", "--no-patch", "--format=format:%H%n%an%n%aI%n%B", ref)
	if err != nil {
		detail.Diff = fmt.Sprintf("error: %v", err)
		return detail
	}
	metaLines := splitLines(strings.TrimLeft(string(metaOut), "\n"))
	if len(metaLines) >= 1 {
		detail.Hash = metaLines[0]
	}
	if len(metaLines) >= 2 {
		detail.Author = metaLines[1]
	}
	if len(metaLines) >= 3 {
		detail.Date = metaLines[2]
	}
	if len(metaLines) >= 4 {
		detail.Message = strings.TrimRight(strings.Join(metaLines[3:], "\n"), "\n")
	}

	// Stat.
	statOut, err := runner.RunGit(ctx, repoPath, "show", "--stat", "--format=", ref)
	if err == nil {
		detail.Stats = parseStatLine(lastLine(string(statOut)))
	}

	// Diff.
	diffOut, err := runner.RunGit(ctx, repoPath, "show", "--format=", ref)
	if err == nil {
		diffStr := strings.TrimPrefix(string(diffOut), "\n")
		if len(diffStr) > maxOutputBytes {
			truncated := diffStr[:maxOutputBytes]
			if idx := strings.LastIndex(truncated, "\n"); idx >= 0 {
				truncated = truncated[:idx+1]
			}
			diffStr = truncated
			// Get affected files for pagination guidance.
			nameOut, _ := runner.RunGit(ctx, repoPath, "show", "--name-only", "--format=", ref)
			detail.Omitted = &Omitted{
				Truncated:     true,
				Reason:        "diff exceeds 100 KB limit; use gitShow with path= to fetch individual files",
				AffectedFiles: nonEmpty(splitLines(string(nameOut))),
			}
		}
		detail.Diff = diffStr
	}

	return detail
}

var ansiRegex = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

const researchTimeout = 180 * time.Second

// ensureResearcherAgent writes the embedded agent definition to the global
// OpenCode agent directory if it doesn't already exist.
func ensureResearcherAgent() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "opencode", "agent")
	path := filepath.Join(dir, "researcher.md")

	if _, err := os.Stat(path); err == nil {
		return nil // already installed
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return os.WriteFile(path, researcherAgentMD, 0o644)
}

func (h *handlers) research(ctx context.Context, _ *mcp.CallToolRequest, input ResearchInput) (*mcp.CallToolResult, any, error) {
	if input.Question == "" {
		return nil, nil, fmt.Errorf("question is required")
	}

	if err := ensureResearcherAgent(); err != nil {
		return nil, nil, fmt.Errorf("install agent: %w", err)
	}

	prompt := input.Question
	if len(input.Context) > 0 {
		var items string
		for _, f := range input.Context {
			items += "\n  - " + f
		}
		prompt = "First read these for context from previous investigations:" + items + "\n\nUsing that as a starting point, investigate and answer: " + input.Question
	}

	out, err := runner.RunCommandWithTimeout(ctx, researchTimeout, h.workspace, "opencode", "run", "--agent", "researcher", prompt)
	if err != nil {
		return nil, nil, fmt.Errorf("opencode: %w", err)
	}
	answer := string(out)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: answer},
		},
	}, nil, nil
}
