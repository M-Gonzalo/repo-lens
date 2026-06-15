package tools

import (
	"fmt"
	"strings"
)

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
			shortHash, entry.Author, entry.Date, entry.Line, entry.Content))
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

func formatWip(out WipOutput) string {
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
		return sb.String()
	}
	if out.Omitted != nil && out.Omitted.Truncated {
		sb.WriteString(fmt.Sprintf("[Warning: Diff truncated. Reason: %s]\n", out.Omitted.Reason))
		if len(out.Omitted.AffectedFiles) > 0 {
			sb.WriteString("Files affected:\n")
			for _, f := range out.Omitted.AffectedFiles {
				sb.WriteString(fmt.Sprintf("- %s\n", f))
			}
		}
		sb.WriteString("\n")
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
