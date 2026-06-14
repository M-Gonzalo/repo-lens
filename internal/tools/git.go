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
func (h *handlers) gitLog(ctx context.Context, _ *mcp.CallToolRequest, input GitLogInput) (*mcp.CallToolResult, GitLogOutput, error) {
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
	}, GitLogOutput{}, nil
}

// gitDiff returns the diff between two refs, optionally scoped to a path.
func (h *handlers) gitDiff(ctx context.Context, _ *mcp.CallToolRequest, input GitDiffInput) (*mcp.CallToolResult, GitDiffOutput, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, GitDiffOutput{}, err
	}

	var rangeArgs []string
	if input.Dirty {
		rangeArgs = []string{"HEAD"}
	} else {
		base := input.Base
		if base == "" {
			base = "HEAD~1"
		}
		target := input.Target
		if target == "" {
			target = "HEAD"
		}
		rangeArgs = []string{base + ".." + target}
	}

	// Gather untracked files if dirty is true
	var untrackedFiles []string
	if input.Dirty {
		statusArgs := []string{"status", "--porcelain=v2", "-uall"}
		if input.Path != "" {
			statusArgs = append(statusArgs, "--", input.Path)
		}
		statusOut, err := runner.RunGit(ctx, repoPath, statusArgs...)
		if err == nil {
			lines := splitLines(string(statusOut))
			for _, line := range lines {
				if strings.HasPrefix(line, "? ") {
					untrackedFile := strings.TrimPrefix(line, "? ")
					if _, err := sandbox.ValidatePath(h.workspace, repoPath, untrackedFile); err == nil {
						untrackedFiles = append(untrackedFiles, untrackedFile)
					}
				}
			}
		}
	}

	// --- stat ---
	statArgs := append([]string{"diff", "--stat"}, rangeArgs...)
	if input.Path != "" {
		statArgs = append(statArgs, "--", input.Path)
	}
	statOut, err := runner.RunGit(ctx, repoPath, statArgs...)
	if err != nil {
		return nil, GitDiffOutput{}, err
	}
	stats := parseStatLine(lastLine(string(statOut)))

	// Limit and process untracked files
	const maxUntracked = 50
	untrackedCount := len(untrackedFiles)
	untrackedOmitted := false
	var allUntrackedFiles []string
	if untrackedCount > maxUntracked {
		allUntrackedFiles = untrackedFiles
		untrackedFiles = untrackedFiles[:maxUntracked]
		untrackedOmitted = true
	} else {
		allUntrackedFiles = untrackedFiles
	}

	type untrackedDiff struct {
		filePath   string
		diffText   string
		insertions int
	}

	var untrackedDiffs []untrackedDiff
	for _, file := range untrackedFiles {
		diffTextBytes, err := runner.RunGitDiffNoIndex(ctx, repoPath, file)
		if err != nil {
			continue
		}
		diffText := string(diffTextBytes)
		insertions := 0
		lines := splitLines(diffText)
		for _, line := range lines {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ") {
				insertions++
			}
		}
		untrackedDiffs = append(untrackedDiffs, untrackedDiff{
			filePath:   file,
			diffText:   diffText,
			insertions: insertions,
		})

		stats.FilesChanged++
		stats.Insertions += insertions
	}

	// If stat-only, return early.
	if input.Stat {
		return nil, GitDiffOutput{Stats: stats}, nil
	}

	// --- diff content ---
	diffArgs := append([]string{"diff"}, rangeArgs...)
	if input.Path != "" {
		diffArgs = append(diffArgs, "--", input.Path)
	}
	diffOut, err := runner.RunGit(ctx, repoPath, diffArgs...)
	if err != nil {
		return nil, GitDiffOutput{}, err
	}

	diffStr := string(diffOut)

	// Append untracked diffs to diffStr
	for _, ud := range untrackedDiffs {
		if diffStr != "" && !strings.HasSuffix(diffStr, "\n") {
			diffStr += "\n"
		}
		diffStr += ud.diffText
	}

	var omitted *Omitted

	if len(diffStr) > maxOutputBytes || untrackedOmitted {
		// Get list of affected files.
		nameArgs := append([]string{"diff", "--name-only"}, rangeArgs...)
		if input.Path != "" {
			nameArgs = append(nameArgs, "--", input.Path)
		}
		nameOut, _ := runner.RunGit(ctx, repoPath, nameArgs...)
		affectedFiles := nonEmpty(splitLines(string(nameOut)))

		// Append all processed untracked files to affectedFiles
		for _, file := range allUntrackedFiles {
			affectedFiles = append(affectedFiles, file)
		}

		reason := "diff exceeds 100 KB limit"
		if len(diffStr) > maxOutputBytes {
			// Truncate to byte limit, preserving whole lines.
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

		omitted = &Omitted{
			Truncated:     true,
			Reason:        reason,
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
	}, GitDiffOutput{}, nil
}

// gitShow returns the full details of a single commit.
func (h *handlers) gitShow(ctx context.Context, _ *mcp.CallToolRequest, input GitShowInput) (*mcp.CallToolResult, GitShowOutput, error) {
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
	metaArgs := []string{"show", "--no-patch", "--format=format:%H%n%an%n%aI%n%B", ref}
	metaOut, err := runner.RunGit(ctx, repoPath, metaArgs...)
	if err != nil {
		return nil, GitShowOutput{}, err
	}
	metaLines := splitLines(strings.TrimLeft(string(metaOut), "\n"))
	var hash, author, date, message string
	if len(metaLines) >= 1 {
		hash = metaLines[0]
	}
	if len(metaLines) >= 2 {
		author = metaLines[1]
	}
	if len(metaLines) >= 3 {
		date = metaLines[2]
	}
	if len(metaLines) >= 4 {
		message = strings.TrimRight(strings.Join(metaLines[3:], "\n"), "\n")
	}

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
	}, GitShowOutput{}, nil
}

// gitBlame returns line-by-line authorship for a file, optionally restricted to a range.
func (h *handlers) gitBlame(ctx context.Context, _ *mcp.CallToolRequest, input GitBlameInput) (*mcp.CallToolResult, GitBlameOutput, error) {
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
	}, GitBlameOutput{}, nil
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
func (h *handlers) gitBranches(ctx context.Context, _ *mcp.CallToolRequest, input GitBranchesInput) (*mcp.CallToolResult, GitBranchesOutput, error) {
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

// gitStatus returns the working-tree status of a repository.
func (h *handlers) gitStatus(ctx context.Context, _ *mcp.CallToolRequest, input GitStatusInput) (*mcp.CallToolResult, GitStatusOutput, error) {
	repoPath, err := sandbox.ValidateRepo(h.workspace, input.Repo)
	if err != nil {
		return nil, GitStatusOutput{}, err
	}

	raw, err := runner.RunGit(ctx, repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil, GitStatusOutput{}, err
	}

	var out GitStatusOutput
	lines := splitLines(string(raw))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			out.Branch = strings.TrimPrefix(line, "# branch.head ")
			if out.Branch == "(detached)" {
				out.Branch = "HEAD"
			}
		case strings.HasPrefix(line, "# branch.ab "):
			// Format: "+N -M"
			rest := strings.TrimPrefix(line, "# branch.ab ")
			parts := strings.Fields(rest)
			if len(parts) >= 2 {
				if n, err := strconv.Atoi(strings.TrimPrefix(parts[0], "+")); err == nil {
					out.Ahead = n
				}
				if n, err := strconv.Atoi(strings.TrimPrefix(parts[1], "-")); err == nil {
					out.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "):
			// Ordinary changed entry.
			// Format: 1 XY N... mode mode mode hash hash filename
			fields := strings.Fields(line)
			if len(fields) < 9 {
				continue
			}
			xy := fields[1]
			filename := fields[len(fields)-1]
			if len(xy) >= 1 && xy[0] != '.' {
				out.Staged = append(out.Staged, filename)
			}
			if len(xy) >= 2 && xy[1] != '.' {
				out.Modified = append(out.Modified, filename)
			}
		case strings.HasPrefix(line, "2 "):
			// Renamed/copied entry.
			// Format: 2 XY N... mode mode mode hash hash R/C score new\told
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			xy := fields[1]
			// Last field is "newpath\toldpath" — take part before tab.
			combined := fields[len(fields)-1]
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
	if input.Format == "json" {
		return nil, out, nil
	}
	textStr := formatGitStatus(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: textStr},
		},
	}, GitStatusOutput{}, nil
}

// fileHistory returns a compact changelog for a single file.
func (h *handlers) fileHistory(ctx context.Context, _ *mcp.CallToolRequest, input FileHistoryInput) (*mcp.CallToolResult, FileHistoryOutput, error) {
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
	}, FileHistoryOutput{}, nil
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

func formatGitLog(out GitLogOutput) string {
	var sb strings.Builder
	if out.Omitted != nil && out.Omitted.Truncated {
		sb.WriteString(fmt.Sprintf("[Warning: Commit list truncated. Reason: %s]\n\n", out.Omitted.Reason))
	}
	for i, c := range out.Commits {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("commit %s (%s)\n", c.Hash, c.ShortHash))
		sb.WriteString(fmt.Sprintf("Author: %s\n", c.Author))
		sb.WriteString(fmt.Sprintf("Date:   %s\n\n", c.Date))
		sb.WriteString(fmt.Sprintf("    %s\n", c.Message))
		if len(c.FilesChanged) > 0 {
			sb.WriteString("\nFiles Changed:\n")
			for _, f := range c.FilesChanged {
				sb.WriteString(fmt.Sprintf("  %s\n", f))
			}
		}
	}
	return sb.String()
}

func formatGitDiff(out GitDiffOutput) string {
	var sb strings.Builder
	if out.Omitted != nil && out.Omitted.Truncated {
		sb.WriteString(fmt.Sprintf("[Warning: Diff truncated. Reason: %s]\n", out.Omitted.Reason))
		if len(out.Omitted.AffectedFiles) > 0 {
			sb.WriteString("Files affected:\n")
			for _, f := range out.Omitted.AffectedFiles {
				sb.WriteString(fmt.Sprintf("- %s\n", f))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(fmt.Sprintf("Stats: %d files changed, +%d insertions, -%d deletions\n\n",
		out.Stats.FilesChanged, out.Stats.Insertions, out.Stats.Deletions))

	if out.Diff != "" {
		sb.WriteString("```diff\n")
		sb.WriteString(out.Diff)
		if !strings.HasSuffix(out.Diff, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}
	return sb.String()
}

func formatGitShow(out GitShowOutput) string {
	var sb strings.Builder
	if out.Omitted != nil && out.Omitted.Truncated {
		sb.WriteString(fmt.Sprintf("[Warning: Diff truncated. Reason: %s]\n", out.Omitted.Reason))
		if len(out.Omitted.AffectedFiles) > 0 {
			sb.WriteString("Files affected:\n")
			for _, f := range out.Omitted.AffectedFiles {
				sb.WriteString(fmt.Sprintf("- %s\n", f))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(fmt.Sprintf("commit %s\n", out.Hash))
	sb.WriteString(fmt.Sprintf("Author: %s\n", out.Author))
	sb.WriteString(fmt.Sprintf("Date:   %s\n\n", out.Date))
	sb.WriteString(fmt.Sprintf("    %s\n\n", out.Message))

	sb.WriteString(fmt.Sprintf("Stats: %d files changed, +%d insertions, -%d deletions\n\n",
		out.Stats.FilesChanged, out.Stats.Insertions, out.Stats.Deletions))

	if out.Diff != "" {
		sb.WriteString("```diff\n")
		sb.WriteString(out.Diff)
		if !strings.HasSuffix(out.Diff, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}
	return sb.String()
}

func formatGitBlame(out GitBlameOutput) string {
	var sb strings.Builder
	if out.Omitted != nil && out.Omitted.Truncated {
		sb.WriteString(fmt.Sprintf("[Warning: Blame output truncated. Reason: %s]\n\n", out.Omitted.Reason))
	}
	for _, entry := range out.Entries {
		shortHash := entry.CommitHash
		if len(shortHash) > 8 {
			shortHash = shortHash[:8]
		}
		sb.WriteString(fmt.Sprintf("%s (%s %s %d) %s\n",
			shortHash,
			entry.Author,
			entry.Date,
			entry.Line,
			entry.Content,
		))
	}
	return sb.String()
}

func formatGitStatus(out GitStatusOutput) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("On branch %s\n", out.Branch))
	if out.Ahead > 0 || out.Behind > 0 {
		sb.WriteString(fmt.Sprintf("Your branch is ahead by %d commits and behind by %d commits.\n", out.Ahead, out.Behind))
	}
	sb.WriteString("\n")

	if len(out.Staged) > 0 {
		sb.WriteString("Changes to be committed:\n")
		for _, f := range out.Staged {
			sb.WriteString(fmt.Sprintf("  staged:    %s\n", f))
		}
		sb.WriteString("\n")
	}
	if len(out.Modified) > 0 {
		sb.WriteString("Changes not staged for commit:\n")
		for _, f := range out.Modified {
			sb.WriteString(fmt.Sprintf("  modified:  %s\n", f))
		}
		sb.WriteString("\n")
	}
	if len(out.Untracked) > 0 {
		sb.WriteString("Untracked files:\n")
		for _, f := range out.Untracked {
			sb.WriteString(fmt.Sprintf("  untracked: %s\n", f))
		}
		sb.WriteString("\n")
	}
	if len(out.Staged) == 0 && len(out.Modified) == 0 && len(out.Untracked) == 0 {
		sb.WriteString("nothing to commit, working tree clean\n")
	}
	return sb.String()
}

func formatFileHistory(out FileHistoryOutput) string {
	var sb strings.Builder
	if out.Omitted != nil && out.Omitted.Truncated {
		sb.WriteString(fmt.Sprintf("[Warning: History truncated. Reason: %s]\n\n", out.Omitted.Reason))
	}
	for i, entry := range out.Entries {
		if i > 0 {
			sb.WriteString("\n")
		}
		shortHash := entry.Hash
		if len(shortHash) > 7 {
			shortHash = shortHash[:7]
		}
		sb.WriteString(fmt.Sprintf("commit %s\n", shortHash))
		sb.WriteString(fmt.Sprintf("Author: %s\n", entry.Author))
		sb.WriteString(fmt.Sprintf("Date:   %s\n", entry.Date))
		sb.WriteString(fmt.Sprintf("Stats:  +%d, -%d lines\n", entry.LinesAdded, entry.LinesRemoved))
		sb.WriteString(fmt.Sprintf("Message: %s\n", entry.Message))
	}
	return sb.String()
}

