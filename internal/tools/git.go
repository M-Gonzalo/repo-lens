package tools

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"repo-lens/internal/runner"
	"repo-lens/internal/sandbox"
)

// statRe matches the final summary line of git diff --stat / git show --stat output.
// Example: " 3 files changed, 10 insertions(+), 2 deletions(-)"
var statRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// parsePorcelainV2 parses git status --porcelain=v2 [--branch] output into a WipOutput.
// Untracked paths are raw (not sandbox-validated) — callers that need validation must do it themselves.
func parsePorcelainV2(lines []string) WipOutput {
	var out WipOutput
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			out.Branch = strings.TrimPrefix(line, "# branch.head ")
			if out.Branch == "(detached)" {
				out.Branch = "HEAD"
			}
		case strings.HasPrefix(line, "# branch.ab "):
			parts := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(parts) >= 2 {
				if n, err := strconv.Atoi(strings.TrimPrefix(parts[0], "+")); err == nil {
					out.Ahead = n
				}
				if n, err := strconv.Atoi(strings.TrimPrefix(parts[1], "-")); err == nil {
					out.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "):
			// Ordinary changed entry: 1 XY N... mode mode mode hash hash filename
			fields := strings.Fields(line)
			if len(fields) < 9 {
				continue
			}
			xy, filename := fields[1], fields[len(fields)-1]
			if len(xy) >= 1 && xy[0] != '.' {
				out.Staged = append(out.Staged, filename)
			}
			if len(xy) >= 2 && xy[1] != '.' {
				out.Modified = append(out.Modified, filename)
			}
		case strings.HasPrefix(line, "2 "):
			// Renamed/copied entry: 2 XY N... mode mode mode hash hash R/C score new\told
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			xy, combined := fields[1], fields[len(fields)-1]
			filename := combined
			if tabIdx := strings.IndexByte(combined, '\t'); tabIdx >= 0 {
				filename = combined[:tabIdx]
			}
			if len(xy) >= 1 && xy[0] != '.' {
				out.Staged = append(out.Staged, filename)
			}
			if len(xy) >= 2 && xy[1] != '.' {
				out.Modified = append(out.Modified, filename)
			}
		case strings.HasPrefix(line, "? "):
			out.Untracked = append(out.Untracked, strings.TrimPrefix(line, "? "))
		}
	}
	if out.Staged == nil {
		out.Staged = []string{}
	}
	if out.Modified == nil {
		out.Modified = []string{}
	}
	if out.Untracked == nil {
		out.Untracked = []string{}
	}
	return out
}

// parseCommitMeta parses the output of git show --format=format:%H%n%an%n%aI%n%B into its fields.
func parseCommitMeta(raw []byte) (hash, author, date, message string) {
	lines := splitLines(strings.TrimLeft(string(raw), "\n"))
	if len(lines) >= 1 {
		hash = lines[0]
	}
	if len(lines) >= 2 {
		author = lines[1]
	}
	if len(lines) >= 3 {
		date = lines[2]
	}
	if len(lines) >= 4 {
		message = strings.TrimRight(strings.Join(lines[3:], "\n"), "\n")
	}
	return
}

// parseStatLine extracts file/insertion/deletion counts from a git stat summary line.
func parseStatLine(line string) DiffStats {
	m := statRe.FindStringSubmatch(line)
	if m == nil {
		return DiffStats{}
	}
	files, _ := strconv.Atoi(m[1])
	ins, _ := strconv.Atoi(m[2])
	del, _ := strconv.Atoi(m[3])
	return DiffStats{FilesChanged: files, Insertions: ins, Deletions: del}
}

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	lines := splitLines(s)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

// gitLog returns the commit history for a repository with flexible filtering.
func (h *handlers) gitLog(ctx context.Context, _ *mcp.CallToolRequest, input GitLogInput) (*mcp.CallToolResult, any, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, GitLogOutput{}, err
	}

	args := []string{
		"log",
		"--format=format:\x1e%H\x1f%h\x1f%an\x1f%aI\x1f%s",
		"--name-only",
	}

	if input.MaxCount > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", input.MaxCount))
	} else {
		args = append(args, "--max-count=20")
	}
	if input.Offset > 0 {
		args = append(args, fmt.Sprintf("--skip=%d", input.Offset))
	}
	if input.Author != "" {
		args = append(args, "--author="+input.Author)
	}
	if input.Since != "" {
		args = append(args, "--since="+input.Since)
	}
	if input.Until != "" {
		args = append(args, "--until="+input.Until)
	}
	if input.Grep != "" {
		args = append(args, "--grep="+input.Grep)
	}
	if input.Ref != "" {
		args = append(args, input.Ref)
	}
	if input.Path != "" {
		args = append(args, "--", input.Path)
	}

	raw, err := runner.RunGit(ctx, repoPath, args...)
	if err != nil {
		return nil, GitLogOutput{}, err
	}

	truncated := len(raw) > maxOutputBytes

	// Split on record separator 0x1E.
	chunks := bytes.Split(raw, []byte{0x1e})
	var commits []CommitInfo
	for _, chunk := range chunks {
		s := string(chunk)
		s = strings.TrimLeft(s, "\n")
		if strings.TrimSpace(s) == "" {
			continue
		}

		// First line is the format fields; remaining lines are file paths.
		nlIdx := strings.IndexByte(s, '\n')
		var headerLine, rest string
		if nlIdx < 0 {
			headerLine = s
		} else {
			headerLine = s[:nlIdx]
			rest = s[nlIdx+1:]
		}

		fields := strings.Split(headerLine, "\x1f")
		if len(fields) < 5 {
			continue
		}

		var files []string
		for _, line := range nonEmpty(splitLines(rest)) {
			files = append(files, strings.TrimSpace(line))
		}

		commits = append(commits, CommitInfo{
			Hash:         fields[0],
			ShortHash:    fields[1],
			Author:       fields[2],
			Date:         fields[3],
			Message:      fields[4],
			FilesChanged: files,
		})
	}

	var omitted *Omitted
	if truncated {
		omitted = &Omitted{
			Truncated: true,
			Reason:    "output exceeds 100 KB limit; use offset to paginate",
		}
	}

	if commits == nil {
		commits = []CommitInfo{}
	}
	out := GitLogOutput{Commits: commits, Omitted: omitted}
	if input.Format == "json" {
		return nil, out, nil
	}
	textStr := formatGitLog(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: textStr},
		},
	}, nil, nil
}

// gitDiff returns the diff between two refs, optionally scoped to a path.
func (h *handlers) gitDiff(ctx context.Context, _ *mcp.CallToolRequest, input GitDiffInput) (*mcp.CallToolResult, any, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, GitDiffOutput{}, err
	}

	base := input.Base
	if base == "" {
		base = "HEAD~1"
	}
	target := input.Target
	if target == "" {
		target = "HEAD"
	}
	rangeArg := base + ".." + target

	// --- stat ---
	statArgs := []string{"diff", "--stat", rangeArg}
	if input.Path != "" {
		statArgs = append(statArgs, "--", input.Path)
	}
	statOut, err := runner.RunGit(ctx, repoPath, statArgs...)
	if err != nil {
		return nil, GitDiffOutput{}, err
	}
	stats := parseStatLine(lastLine(string(statOut)))

	// If stat-only, return early.
	if input.Stat {
		return nil, GitDiffOutput{Stats: stats}, nil
	}

	// --- diff content ---
	diffArgs := []string{"diff", rangeArg}
	if input.Path != "" {
		diffArgs = append(diffArgs, "--", input.Path)
	}
	diffOut, err := runner.RunGit(ctx, repoPath, diffArgs...)
	if err != nil {
		return nil, GitDiffOutput{}, err
	}

	diffStr := string(diffOut)

	var omitted *Omitted

	if len(diffStr) > maxOutputBytes {
		nameArgs := []string{"diff", "--name-only", rangeArg}
		if input.Path != "" {
			nameArgs = append(nameArgs, "--", input.Path)
		}
		nameOut, _ := runner.RunGit(ctx, repoPath, nameArgs...)
		affectedFiles := nonEmpty(splitLines(string(nameOut)))

		truncated := diffStr[:maxOutputBytes]
		if idx := strings.LastIndex(truncated, "\n"); idx >= 0 {
			truncated = truncated[:idx+1]
		}
		diffStr = truncated

		omitted = &Omitted{
			Truncated:     true,
			Reason:        "diff exceeds 100 KB limit",
			AffectedFiles: affectedFiles,
		}
	}

	out := GitDiffOutput{Diff: diffStr, Stats: stats, Omitted: omitted}
	if input.Format == "json" {
		return nil, out, nil
	}
	textStr := formatGitDiff(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: textStr},
		},
	}, nil, nil
}

// gitShow returns the full details of a single commit.
func (h *handlers) gitShow(ctx context.Context, _ *mcp.CallToolRequest, input GitShowInput) (*mcp.CallToolResult, any, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, GitShowOutput{}, err
	}

	ref := input.Ref
	if ref == "" {
		ref = "HEAD"
	}

	// --- metadata ---
	// Use %n (literal newline) so each field is on its own line, avoiding
	// the ambiguity of embedding %B (multi-line) inside a \x1f-delimited format.
	metaOut, err := runner.RunGit(ctx, repoPath, "show", "--no-patch", "--format=format:%H%n%an%n%aI%n%B", ref)
	if err != nil {
		return nil, GitShowOutput{}, err
	}
	hash, author, date, message := parseCommitMeta(metaOut)

	// --- stat ---
	statArgs := []string{"show", "--stat", "--format=", ref}
	if input.Path != "" {
		statArgs = append(statArgs, "--", input.Path)
	}
	statOut, err := runner.RunGit(ctx, repoPath, statArgs...)
	if err != nil {
		return nil, GitShowOutput{}, err
	}
	stats := parseStatLine(lastLine(string(statOut)))

	// --- diff ---
	diffArgs := []string{"show", "--format=", ref}
	if input.Path != "" {
		diffArgs = append(diffArgs, "--", input.Path)
	}
	diffOut, err := runner.RunGit(ctx, repoPath, diffArgs...)
	if err != nil {
		return nil, GitShowOutput{}, err
	}

	var omitted *Omitted
	diffStr := string(diffOut)
	// Strip leading blank line that --format= may emit.
	diffStr = strings.TrimPrefix(diffStr, "\n")

	if len(diffOut) > maxOutputBytes {
		truncated := diffStr[:maxOutputBytes]
		if idx := strings.LastIndex(truncated, "\n"); idx >= 0 {
			truncated = truncated[:idx+1]
		}
		diffStr = truncated

		// Get affected files so the caller knows what to re-request with path=.
		nameArgs := []string{"show", "--name-only", "--format=", ref}
		if input.Path != "" {
			nameArgs = append(nameArgs, "--", input.Path)
		}
		nameOut, _ := runner.RunGit(ctx, repoPath, nameArgs...)
		omitted = &Omitted{
			Truncated:     true,
			Reason:        "diff exceeds 100 KB limit",
			AffectedFiles: nonEmpty(splitLines(string(nameOut))),
		}
	}

	out := GitShowOutput{
		Hash:    hash,
		Author:  author,
		Date:    date,
		Message: message,
		Diff:    diffStr,
		Stats:   stats,
		Omitted: omitted,
	}
	if input.Format == "json" {
		return nil, out, nil
	}
	textStr := formatGitShow(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: textStr},
		},
	}, nil, nil
}

// gitBlame returns line-by-line authorship for a file, optionally restricted to a range.
func (h *handlers) gitBlame(ctx context.Context, _ *mcp.CallToolRequest, input GitBlameInput) (*mcp.CallToolResult, any, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, GitBlameOutput{}, err
	}
	if _, err := sandbox.ValidatePath(h.workspace, repoPath, input.Path); err != nil {
		return nil, GitBlameOutput{}, err
	}

	args := []string{"blame", "--line-porcelain"}

	// Add -L range flag if requested.
	if input.StartLine > 0 && input.EndLine > 0 {
		args = append(args, "-L", fmt.Sprintf("%d,%d", input.StartLine, input.EndLine))
	} else if input.StartLine > 0 {
		args = append(args, "-L", fmt.Sprintf("%d,", input.StartLine))
	} else if input.EndLine > 0 {
		args = append(args, "-L", fmt.Sprintf("1,%d", input.EndLine))
	}

	args = append(args, "--", input.Path)

	raw, err := runner.RunGit(ctx, repoPath, args...)
	if err != nil {
		return nil, GitBlameOutput{}, err
	}

	// Parse porcelain blame output.
	lines := splitLines(string(raw))

	var entries []BlameEntry
	var curHash string
	var curAuthor string
	var curDate string
	var curLineNum int

	for _, line := range lines {
		switch {
		case len(line) == 0:
			// blank line between groups — skip
		case len(line) >= 40 && isHexString(line[:40]) && (len(line) == 40 || line[40] == ' '):
			// Header line: "<hash> <orig-line> <final-line> [count]"
			curHash = line[:40]
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				n, _ := strconv.Atoi(fields[2])
				curLineNum = n
			}
		case strings.HasPrefix(line, "author "):
			curAuthor = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			ts, _ := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
			curDate = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		case strings.HasPrefix(line, "\t"):
			// Content line.
			content := line[1:] // strip leading tab
			entries = append(entries, BlameEntry{
				Line:       curLineNum,
				Content:    content,
				Author:     curAuthor,
				Date:       curDate,
				CommitHash: curHash,
			})
		}
	}

	// Rough size limit: ~100 bytes per entry.
	const maxEntries = maxOutputBytes / 100
	var omitted *Omitted
	if len(entries) > maxEntries {
		omitted = &Omitted{
			Truncated:  true,
			TotalLines: len(entries),
			ShownLines: maxEntries,
			Reason:     "blame output exceeds 100 KB limit; use startLine/endLine to narrow the range",
		}
		entries = entries[:maxEntries]
	}

	if entries == nil {
		entries = []BlameEntry{}
	}
	out := GitBlameOutput{Entries: entries, Omitted: omitted}
	if input.Format == "json" {
		return nil, out, nil
	}
	textStr := formatGitBlame(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: textStr},
		},
	}, nil, nil
}

// isHexString returns true if all bytes in s are valid lowercase/uppercase hex digits.
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// gitBranches lists local or remote-tracking branches.
func (h *handlers) gitBranches(ctx context.Context, _ *mcp.CallToolRequest, input GitBranchesInput) (*mcp.CallToolResult, any, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, GitBranchesOutput{}, err
	}

	var args []string
	if input.Remote {
		args = []string{
			"for-each-ref",
			"--format= |%(refname:short)|%(objectname:short)||%(creatordate:iso-strict)",
			"refs/remotes",
		}
	} else {
		args = []string{
			"for-each-ref",
			"--format=%(HEAD)|%(refname:short)|%(objectname:short)|%(upstream:short)|%(creatordate:iso-strict)",
			"refs/heads",
		}
	}

	raw, err := runner.RunGit(ctx, repoPath, args...)
	if err != nil {
		return nil, GitBranchesOutput{}, err
	}

	lines := nonEmpty(splitLines(string(raw)))
	var branches []BranchInfo
	for _, line := range lines {
		fields := strings.Split(line, "|")
		if len(fields) < 4 {
			continue
		}
		branch := BranchInfo{
			Current:    strings.TrimSpace(fields[0]) == "*",
			Name:       fields[1],
			LastCommit: fields[2],
			Upstream:   fields[3],
		}
		branches = append(branches, branch)
	}

	if branches == nil {
		branches = []BranchInfo{}
	}
	return nil, GitBranchesOutput{Branches: branches}, nil
}


// fileHistory returns a compact changelog for a single file.
func (h *handlers) fileHistory(ctx context.Context, _ *mcp.CallToolRequest, input FileHistoryInput) (*mcp.CallToolResult, any, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, FileHistoryOutput{}, err
	}
	if _, err := sandbox.ValidatePath(h.workspace, repoPath, input.Path); err != nil {
		return nil, FileHistoryOutput{}, err
	}

	maxEntries := input.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 20
	}
	// Fetch enough entries to satisfy offset + limit.
	fetchCount := maxEntries + input.Offset

	args := []string{
		"log",
		"--follow",
		"--numstat",
		"--format=format:\x1e%H\x1f%aI\x1f%an\x1f%s",
		fmt.Sprintf("--max-count=%d", fetchCount),
	}
	if input.Since != "" {
		args = append(args, "--since="+input.Since)
	}
	if input.Until != "" {
		args = append(args, "--until="+input.Until)
	}
	args = append(args, "--", input.Path)

	raw, err := runner.RunGit(ctx, repoPath, args...)
	if err != nil {
		return nil, FileHistoryOutput{}, err
	}

	// Split on record separator 0x1E.
	chunks := bytes.Split(raw, []byte{0x1e})

	var all []FileHistoryEntry
	for _, chunk := range chunks {
		s := string(chunk)
		s = strings.TrimLeft(s, "\n")
		if strings.TrimSpace(s) == "" {
			continue
		}

		nlIdx := strings.IndexByte(s, '\n')
		var headerLine, rest string
		if nlIdx < 0 {
			headerLine = s
		} else {
			headerLine = s[:nlIdx]
			rest = s[nlIdx+1:]
		}

		fields := strings.Split(headerLine, "\x1f")
		if len(fields) < 4 {
			continue
		}

		entry := FileHistoryEntry{
			Hash:    fields[0],
			Date:    fields[1],
			Author:  fields[2],
			Message: fields[3],
		}

		// Parse numstat lines: "N\tM\tpath"
		for _, line := range nonEmpty(splitLines(rest)) {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 2 {
				continue
			}
			added, _ := parseNumstatField(parts[0])
			removed, _ := parseNumstatField(parts[1])
			entry.LinesAdded += added
			entry.LinesRemoved += removed
		}

		all = append(all, entry)
	}

	// Apply offset.
	if input.Offset > len(all) {
		input.Offset = len(all)
	}
	all = all[input.Offset:]

	var omitted *Omitted
	if len(all) > maxEntries {
		omitted = &Omitted{
			Truncated:  true,
			TotalLines: len(all),
			ShownLines: maxEntries,
			Reason:     "results exceed requested limit; use offset to paginate",
		}
		all = all[:maxEntries]
	}

	if all == nil {
		all = []FileHistoryEntry{}
	}
	out := FileHistoryOutput{Entries: all, Omitted: omitted}
	if input.Format == "json" {
		return nil, out, nil
	}
	textStr := formatFileHistory(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: textStr},
		},
	}, nil, nil
}

// parseNumstatField parses a numstat added/removed field.
// "-" (binary files) returns 0 with no error.
func parseNumstatField(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "-" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

// wip returns the current working-tree state: status summary + full diff against HEAD.
func (h *handlers) wip(ctx context.Context, _ *mcp.CallToolRequest, input WipInput) (*mcp.CallToolResult, any, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, nil, err
	}

	// --- Status + untracked file discovery ---
	statusArgs := []string{"status", "--porcelain=v2", "--branch", "-uall"}
	if input.Path != "" {
		statusArgs = append(statusArgs, "--", input.Path)
	}
	statusOut, err := runner.RunGit(ctx, repoPath, statusArgs...)
	if err != nil {
		return nil, nil, err
	}

	status := parsePorcelainV2(splitLines(string(statusOut)))
	var out WipOutput
	out.Branch = status.Branch
	out.Ahead = status.Ahead
	out.Behind = status.Behind
	out.Staged = status.Staged
	out.Modified = status.Modified

	// Validate untracked paths against the sandbox before using them for diffs.
	var untrackedFiles []string
	for _, f := range status.Untracked {
		if _, err := sandbox.ValidatePath(h.workspace, repoPath, f); err == nil {
			untrackedFiles = append(untrackedFiles, f)
		}
	}
	out.Untracked = untrackedFiles
	if out.Untracked == nil {
		out.Untracked = []string{}
	}

	// --- Stat ---
	statArgs := []string{"diff", "--stat", "HEAD"}
	if input.Path != "" {
		statArgs = append(statArgs, "--", input.Path)
	}
	statOut, err := runner.RunGit(ctx, repoPath, statArgs...)
	if err != nil {
		return nil, nil, err
	}
	out.Stats = parseStatLine(lastLine(string(statOut)))

	// Untracked file diffs
	const maxUntracked = 50
	untrackedCount := len(untrackedFiles)
	untrackedOmitted := false
	allUntrackedFiles := untrackedFiles
	if untrackedCount > maxUntracked {
		untrackedFiles = untrackedFiles[:maxUntracked]
		untrackedOmitted = true
	}

	type untrackedDiff struct{ diffText string; insertions int }
	var untrackedDiffs []untrackedDiff
	for _, file := range untrackedFiles {
		diffBytes, err := runner.RunGitDiffNoIndex(ctx, repoPath, file)
		if err != nil {
			continue
		}
		diffText := string(diffBytes)
		insertions := 0
		for _, l := range splitLines(diffText) {
			if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++ ") {
				insertions++
			}
		}
		untrackedDiffs = append(untrackedDiffs, untrackedDiff{diffText, insertions})
		out.Stats.FilesChanged++
		out.Stats.Insertions += insertions
	}

	if input.Stat {
		if input.Format == "json" {
			return nil, out, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: formatWip(out)}}}, nil, nil
	}

	// --- Diff ---
	diffArgs := []string{"diff", "HEAD"}
	if input.Path != "" {
		diffArgs = append(diffArgs, "--", input.Path)
	}
	diffOut, err := runner.RunGit(ctx, repoPath, diffArgs...)
	if err != nil {
		return nil, nil, err
	}
	diffStr := string(diffOut)
	for _, ud := range untrackedDiffs {
		if diffStr != "" && !strings.HasSuffix(diffStr, "\n") {
			diffStr += "\n"
		}
		diffStr += ud.diffText
	}

	if len(diffStr) > maxOutputBytes || untrackedOmitted {
		nameArgs := []string{"diff", "--name-only", "HEAD"}
		if input.Path != "" {
			nameArgs = append(nameArgs, "--", input.Path)
		}
		nameOut, _ := runner.RunGit(ctx, repoPath, nameArgs...)
		affectedFiles := append(nonEmpty(splitLines(string(nameOut))), allUntrackedFiles...)

		reason := "diff exceeds 100 KB limit"
		if len(diffStr) > maxOutputBytes {
			truncated := diffStr[:maxOutputBytes]
			if idx := strings.LastIndex(truncated, "\n"); idx >= 0 {
				truncated = truncated[:idx+1]
			}
			diffStr = truncated
			if untrackedOmitted {
				reason = fmt.Sprintf("diff exceeds 100 KB limit; too many untracked files (showing diffs for first %d of %d)", maxUntracked, untrackedCount)
			}
		} else if untrackedOmitted {
			reason = fmt.Sprintf("too many untracked files (showing diffs for first %d of %d)", maxUntracked, untrackedCount)
		}
		out.Omitted = &Omitted{Truncated: true, Reason: reason, AffectedFiles: affectedFiles}
	}
	out.Diff = diffStr

	if input.Format == "json" {
		return nil, out, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: formatWip(out)}}}, nil, nil
}


