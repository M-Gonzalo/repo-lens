# repo-lens

A read-only MCP server that gives AI agents fast, approval-free access to read git history, search code, and inspect files across a workspace.

## Why

When an agent needs to inspect code or git history via shell commands, the user must approve each one. repo-lens exposes only safe, read-only operations as MCP tools — once registered, the agent can call them freely.

## Setup

```bash
# Build
cd repo-lens && go build -o repo-lens .

# Or via Makefile
make build
```

Register in `.mcp.json`:

```json
{
  "mcpServers": {
    "repo-lens": {
      "type": "stdio",
      "command": "/path/to/repo-lens/repo-lens",
      "args": ["--workspace", "/path/to/your/workspace"]
    }
  }
}
```

The workspace is the root directory that contains your git repositories. repo-lens will discover all git repos nested within it.

## Tools

| Tool | Description |
|------|-------------|
| `listRepos` | Discover all git repos under the workspace |
| `search` | ripgrep across tracked files in a repo |
| `readFileAtRef` | Read a file at any git ref (time machine) |
| `gitLog` | Commit history with filtering and pagination |
| `gitDiff` | Diff between two committed refs, with stat summary option |
| `wip` | All uncommitted working-tree changes: branch status (ahead/behind, staged/modified/untracked) + full diff against HEAD including untracked files |
| `gitShow` | Full details of a single commit |
| `gitBlame` | Line-by-line authorship |
| `gitBranches` | List local or remote branches |
| `fileHistory` | Compact changelog for a single file |
| `resolveJiraTag` | Find commits mentioning a Jira tag across all repos |
| `collectReviewBundle` | Aggregate everything needed for a code review (outputs markdown by default, supports json) |
| `research` | Delegate a codebase question to an AI sub-agent that reads files and searches for patterns, returning a structured answer. Accepts a `context` array of file paths (prior research, design docs, specs, notes — anything relevant) the agent reads before investigating. Results are saved to `{workspace}/.opencode/research/<timestamp>.md`. Requires `opencode`. |

## Security

- All paths are validated to stay within the workspace root (symlinks resolved)
- No arbitrary shell: tools construct argument lists for `git`, `rg`, and `jv` only
- Output is capped at 100KB per call; truncated responses include an `omitted` field with pagination guidance

## Dependencies

- `git` (required)
- `rg` / ripgrep (required for `search`)
- `jv` (optional — for Jira ticket lookup in `collectReviewBundle`)
- `opencode` (optional — required for `research`)

## Flags

```
--workspace   Path to workspace root (required, or set REPO_LENS_WORKSPACE)
--version     Print version and exit
--help        Show usage
```
