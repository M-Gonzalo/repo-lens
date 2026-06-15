package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

type handlers struct {
	workspace string
}

// Register wires all repo-lens tools onto s.
func Register(s *mcp.Server, workspace string) {
	h := &handlers{workspace: workspace}

	// Layer 1: Primitives
	mcp.AddTool(s, &mcp.Tool{
		Name:        "listRepos",
		Description: "Discover all git repositories under the workspace root. Returns name, path, current branch, last commit date, and remote URL for each.",
	}, h.listRepos)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Search for a string or pattern across git-tracked files in a repository using ripgrep. Respects .gitignore. Supports literal and regex queries, glob file filters, and pagination via offset.",
	}, h.search)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "readFileAtRef",
		Description: "Read a file's contents at any point in git history. Use ref to specify a commit hash, branch, or tag (defaults to HEAD). Supports line windowing via startLine/endLine.",
	}, h.readFileAtRef)

	// Layer 2: Git
	mcp.AddTool(s, &mcp.Tool{
		Name:        "gitLog",
		Description: "Show commit history with flexible filtering by author, date range, path, message, or ref. Returns hash, author, date, message, and changed files per commit. Supports pagination via offset.",
	}, h.gitLog)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gitDiff",
		Description: "Show the diff between two refs (defaults to HEAD~1..HEAD). Use stat:true for a summary only. Use path to scope to a single file. Returns diff text plus insertion/deletion stats.",
	}, h.gitDiff)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "wip",
		Description: "Show all uncommitted changes in the working tree: branch status (ahead/behind, staged, modified, untracked files) plus the full diff against HEAD including untracked files. Use stat:true for a summary without the diff.",
	}, h.wip)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gitShow",
		Description: "Show full details of a single commit: author, date, message, and diff. Use path to scope the diff to a single file.",
	}, h.gitShow)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gitBlame",
		Description: "Show line-by-line authorship for a file. Use startLine/endLine to restrict to a range. Returns line number, content, author, date, and commit hash per line.",
	}, h.gitBlame)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gitBranches",
		Description: "List local branches (or remote-tracking branches if remote:true). Returns name, current flag, last commit hash, and upstream branch.",
	}, h.gitBranches)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gitStatus",
		Description: "Show the working tree status of a repository: current branch, ahead/behind counts, staged changes, unstaged modifications, and untracked files.",
	}, h.gitStatus)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "fileHistory",
		Description: "Show a compact changelog for a single file: each commit that touched it, with lines added/removed. Follows renames. Use offset for pagination.",
	}, h.fileHistory)

	// Layer 3: Workflow
	mcp.AddTool(s, &mcp.Tool{
		Name:        "resolveJiraTag",
		Description: "Find all commits that mention a Jira ticket tag (e.g. DEV-53830) across all repos (or a specific repo). Searches all branches. Returns repo, hash, author, date, message, and branch per match.",
	}, h.resolveJiraTag)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "collectReviewBundle",
		Description: "Aggregate everything needed for a code review. Provide a jiraTag to find all commits mentioning that ticket across all repos, or a commit hash to review a single commit. Returns per-repo commit details with diffs, stats, and optional Jira ticket metadata from jv.",
	}, h.collectReviewBundle)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "research",
		Description: "Delegate a codebase research question to an AI agent that reads files and searches for patterns in its own context window, returning a concise answer. Use for questions that would require reading many files to answer. Requires opencode to be installed.",
	}, h.research)
}
