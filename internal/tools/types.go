package tools

import "strings"

const maxOutputBytes = 100 * 1024 // 100 KB

// Omitted describes truncation that occurred in a tool response.
// Every tool that can produce large output must include this when it truncates.
type Omitted struct {
	Truncated     bool     `json:"truncated,omitempty"`
	TotalLines    int      `json:"totalLines,omitempty"`
	ShownLines    int      `json:"shownLines,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	AffectedFiles []string `json:"affectedFiles,omitempty"`
}

// — listRepos —

type ListReposInput struct{}

type RepoInfo struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Branch         string `json:"branch"`
	LastCommitDate string `json:"lastCommitDate"`
	RemoteURL      string `json:"remoteUrl"`
}

type ListReposOutput struct {
	Repos []RepoInfo `json:"repos"`
}

// — search —

type SearchInput struct {
	Repo       string   `json:"repo"                  jsonschema:"name of the repository under workspace"`
	Query      string   `json:"query"                 jsonschema:"search term or pattern"`
	Regex      bool     `json:"regex,omitempty"       jsonschema:"treat query as a regex pattern; false means literal match"`
	Includes   []string `json:"includes,omitempty"    jsonschema:"optional glob patterns to restrict searched files (e.g. *.go)"`
	MaxResults int      `json:"maxResults,omitempty"  jsonschema:"maximum number of results to return; defaults to 50"`
	Offset     int      `json:"offset,omitempty"      jsonschema:"skip first N results for pagination"`
}

type SearchResult struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

type SearchOutput struct {
	Results []SearchResult `json:"results"`
	Omitted int            `json:"omitted,omitempty"`
}

// — readFileAtRef —

type ReadFileAtRefInput struct {
	Repo      string `json:"repo"                jsonschema:"name of the repository under workspace"`
	Path      string `json:"path"                jsonschema:"relative path to the file within the repo"`
	Ref       string `json:"ref,omitempty"       jsonschema:"commit hash, branch, or tag; defaults to HEAD"`
	StartLine int    `json:"startLine,omitempty" jsonschema:"first line to return (1-indexed); 0 means beginning"`
	EndLine   int    `json:"endLine,omitempty"   jsonschema:"last line to return (inclusive); 0 means end of file"`
	Format    string `json:"format,omitempty"    jsonschema:"Output format: 'text' (default) or 'json'"`
}

type ReadFileAtRefOutput struct {
	Content    string   `json:"content"`
	TotalLines int      `json:"totalLines"`
	Ref        string   `json:"ref"`
	Omitted    *Omitted `json:"omitted,omitempty"`
}

// — gitLog —

type GitLogInput struct {
	Repo     string `json:"repo"              jsonschema:"name of the repository under workspace"`
	MaxCount int    `json:"maxCount,omitempty" jsonschema:"maximum number of commits to return; defaults to 20"`
	Offset   int    `json:"offset,omitempty"   jsonschema:"skip first N commits for pagination"`
	Author   string `json:"author,omitempty"   jsonschema:"filter by author name or email substring"`
	Since    string `json:"since,omitempty"    jsonschema:"show commits more recent than this date (e.g. 2024-01-01)"`
	Until    string `json:"until,omitempty"    jsonschema:"show commits older than this date"`
	Path     string `json:"path,omitempty"     jsonschema:"restrict to commits that touch this path"`
	Grep     string `json:"grep,omitempty"     jsonschema:"filter by commit message substring"`
	Ref      string `json:"ref,omitempty"      jsonschema:"branch, tag, or commit range (e.g. main or HEAD~10..HEAD)"`
	Format   string `json:"format,omitempty"   jsonschema:"Output format: 'text' (default) or 'json'"`
}

type CommitInfo struct {
	Hash         string   `json:"hash"`
	ShortHash    string   `json:"shortHash"`
	Author       string   `json:"author"`
	Date         string   `json:"date"`
	Message      string   `json:"message"`
	FilesChanged []string `json:"filesChanged"`
}

type GitLogOutput struct {
	Commits []CommitInfo `json:"commits"`
	Omitted *Omitted     `json:"omitted,omitempty"`
}

// — gitDiff —

type GitDiffInput struct {
	Repo   string `json:"repo"            jsonschema:"name of the repository under workspace"`
	Base   string `json:"base,omitempty"   jsonschema:"base ref; defaults to HEAD~1"`
	Target string `json:"target,omitempty" jsonschema:"target ref; defaults to HEAD"`
	Dirty  bool   `json:"dirty,omitempty"  jsonschema:"diff the working tree against HEAD (uncommitted changes); ignores base and target"`
	Path   string `json:"path,omitempty"   jsonschema:"restrict diff to this path"`
	Stat   bool   `json:"stat,omitempty"   jsonschema:"return only the stat summary, not the full diff"`
	Format string `json:"format,omitempty"   jsonschema:"Output format: 'text' (default) or 'json'"`
}

type DiffStats struct {
	FilesChanged int `json:"filesChanged"`
	Insertions   int `json:"insertions"`
	Deletions    int `json:"deletions"`
}

type GitDiffOutput struct {
	Diff    string    `json:"diff"`
	Stats   DiffStats `json:"stats"`
	Omitted *Omitted  `json:"omitted,omitempty"`
}

// — gitShow —

type GitShowInput struct {
	Repo   string `json:"repo"          jsonschema:"name of the repository under workspace"`
	Ref    string `json:"ref,omitempty" jsonschema:"commit hash, branch, or tag; defaults to HEAD"`
	Path   string `json:"path,omitempty" jsonschema:"restrict diff to this file path"`
	Format string `json:"format,omitempty" jsonschema:"Output format: 'text' (default) or 'json'"`
}

type GitShowOutput struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Date    string    `json:"date"`
	Message string    `json:"message"`
	Diff    string    `json:"diff"`
	Stats   DiffStats `json:"stats"`
	Omitted *Omitted  `json:"omitted,omitempty"`
}

// — gitBlame —

type GitBlameInput struct {
	Repo      string `json:"repo"                jsonschema:"name of the repository under workspace"`
	Path      string `json:"path"                jsonschema:"relative path to the file"`
	StartLine int    `json:"startLine,omitempty" jsonschema:"first line to blame (1-indexed); 0 means beginning"`
	EndLine   int    `json:"endLine,omitempty"   jsonschema:"last line to blame (inclusive); 0 means end of file"`
	Format    string `json:"format,omitempty"    jsonschema:"Output format: 'text' (default) or 'json'"`
}

type BlameEntry struct {
	Line       int    `json:"line"`
	Content    string `json:"content"`
	Author     string `json:"author"`
	Date       string `json:"date"`
	CommitHash string `json:"commitHash"`
}

type GitBlameOutput struct {
	Entries []BlameEntry `json:"entries"`
	Omitted *Omitted     `json:"omitted,omitempty"`
}

// — gitBranches —

type GitBranchesInput struct {
	Repo   string `json:"repo"            jsonschema:"name of the repository under workspace"`
	Remote bool   `json:"remote,omitempty" jsonschema:"list remote-tracking branches instead of local branches"`
}

type BranchInfo struct {
	Name       string `json:"name"`
	Current    bool   `json:"current"`
	LastCommit string `json:"lastCommit"`
	Upstream   string `json:"upstream,omitempty"`
}

type GitBranchesOutput struct {
	Branches []BranchInfo `json:"branches"`
}

// — gitStatus —

type GitStatusInput struct {
	Repo   string `json:"repo" jsonschema:"name of the repository under workspace"`
	Format string `json:"format,omitempty" jsonschema:"Output format: 'text' (default) or 'json'"`
}

type GitStatusOutput struct {
	Branch    string   `json:"branch"`
	Ahead     int      `json:"ahead"`
	Behind    int      `json:"behind"`
	Staged    []string `json:"staged"`
	Modified  []string `json:"modified"`
	Untracked []string `json:"untracked"`
}

// — fileHistory —

type FileHistoryInput struct {
	Repo       string `json:"repo"                jsonschema:"name of the repository under workspace"`
	Path       string `json:"path"                jsonschema:"relative path to the file"`
	Since      string `json:"since,omitempty"      jsonschema:"show history more recent than this date"`
	Until      string `json:"until,omitempty"      jsonschema:"show history older than this date"`
	MaxEntries int    `json:"maxEntries,omitempty" jsonschema:"maximum entries to return; defaults to 20"`
	Offset     int    `json:"offset,omitempty"     jsonschema:"skip first N entries for pagination"`
	Format     string `json:"format,omitempty"     jsonschema:"Output format: 'text' (default) or 'json'"`
}

type FileHistoryEntry struct {
	Hash         string `json:"hash"`
	Date         string `json:"date"`
	Author       string `json:"author"`
	Message      string `json:"message"`
	LinesAdded   int    `json:"linesAdded"`
	LinesRemoved int    `json:"linesRemoved"`
}

type FileHistoryOutput struct {
	Entries []FileHistoryEntry `json:"entries"`
	Omitted *Omitted           `json:"omitted,omitempty"`
}

// — resolveJiraTag —

type ResolveJiraTagInput struct {
	Tag  string `json:"tag"           jsonschema:"Jira ticket tag to search for (e.g. DEV-53830)"`
	Repo string `json:"repo,omitempty" jsonschema:"restrict search to this repository; searches all repos if omitted"`
}

type JiraTagMatch struct {
	Repo    string `json:"repo"`
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
	Branch  string `json:"branch,omitempty"`
}

type ResolveJiraTagOutput struct {
	Matches []JiraTagMatch `json:"matches"`
}

// — collectReviewBundle —

type CollectReviewBundleInput struct {
	JiraTag string `json:"jiraTag,omitempty" jsonschema:"Jira ticket tag (e.g. DEV-54554). Finds all commits mentioning this tag across all repos and aggregates their diffs."`
	Commit  string `json:"commit,omitempty"  jsonschema:"single commit hash to review. Use with repo to scope; without repo searches all repos."`
	Repo    string `json:"repo,omitempty"    jsonschema:"restrict search to this repository (optional)."`
	Format  string `json:"format,omitempty"  jsonschema:"Output format: 'json' or 'markdown' (default)."`
}

type ReviewCommitDetail struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Date    string    `json:"date"`
	Message string    `json:"message"`
	Diff    string    `json:"diff"`
	Stats   DiffStats `json:"stats"`
	Omitted *Omitted  `json:"omitted,omitempty"`
}

type RepoBundle struct {
	Repo    string               `json:"repo"`
	Commits []ReviewCommitDetail `json:"commits"`
}

type ReviewSummary struct {
	TotalCommits int `json:"totalCommits"`
	FilesChanged int `json:"filesChanged"`
	Insertions   int `json:"insertions"`
	Deletions    int `json:"deletions"`
}

type JiraInfo struct {
	Tag    string `json:"tag"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

type CollectReviewBundleOutput struct {
	Summary ReviewSummary `json:"summary"`
	Repos   []RepoBundle  `json:"repos"`
	Jira    *JiraInfo     `json:"jira,omitempty"`
}

// — research —

type ResearchInput struct {
	Question string   `json:"question" jsonschema:"the question to research about the codebase; be specific with module names or file paths if known"`
	Context  []string `json:"context,omitempty" jsonschema:"file paths to previous research results to build upon"`
	Repo     string   `json:"repo,omitempty"    jsonschema:"optional name of the repository to scope research to; if omitted, researches from workspace root"`
}

// — shared helpers —

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
