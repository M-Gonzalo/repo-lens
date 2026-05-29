# repo-lens Usage Guide

This server gives you read-only access to the git repositories inside the workspace. Use it instead of composing shell commands — no approval prompts needed.

## Starting point: always call `listRepos` first

Before using any other tool, call `listRepos` to discover what repos exist and their current branches. Use the `name` field as the `repo` parameter for all subsequent calls.

---

## Tool Reference

### `listRepos`
**When**: At the start of any investigation to orient yourself.
**Returns**: name, path, branch, last commit date, remote URL for each repo.

---

### `search`
**When**: You need to find where a function, class, string, or pattern is defined.
**Key params**:
- `repo` — which repo to search
- `query` — the search term
- `regex: false` (default) — literal match; `regex: true` for regex
- `includes` — glob patterns to restrict files, e.g. `["*.rb", "*.ex"]`
- `maxResults` / `offset` — pagination

**Note**: Searches only git-tracked files. Respects `.gitignore`.

---

### `readFileAtRef`
**When**: You need to see a file's contents — either current (`ref: "HEAD"`) or historical.
**Why not just use your built-in file reader?** Use this when you need to read a file *as it was at a specific commit*. For current files, your built-in tool is fine.
**Key params**: `repo`, `path` (relative to repo root), `ref` (default: HEAD), `startLine`/`endLine` for windowing.

---

### `gitLog`
**When**: You want to understand recent activity, find when something changed, or filter commits by author/date/path.
**Key params**:
- `maxCount` — default 20; increase for broader history
- `offset` — for pagination
- `path` — restrict to commits touching a specific file
- `grep` — filter by commit message substring (e.g. a Jira tag)
- `author`, `since`, `until` — date and author filters

---

### `gitDiff`
**When**: You want to see what changed between two points.
**Key params**:
- `base` — default `HEAD~1`; can be a branch name, commit hash, or tag
- `target` — default `HEAD`
- `stat: true` — returns only the summary (files changed, insertions, deletions) without the full diff
- `path` — scope to a single file when the full diff is too large

**Pagination**: If `omitted.truncated` is true, use `path=` with each file from `omitted.affectedFiles` to fetch individual file diffs.

---

### `gitShow`
**When**: You want the full picture of a single commit — author, message, and diff.
**Key params**: `repo`, `ref` (commit hash or branch), `path` (optional, scopes the diff).

**Pagination**: Same as `gitDiff` — use `path=` for large commits.

---

### `gitBlame`
**When**: You need to know who last touched specific lines and in which commit.
**Key params**: `repo`, `path`, `startLine`/`endLine` (1-indexed, both optional).

---

### `gitBranches`
**When**: You need to see what branches exist or find a feature branch.
**Key params**: `repo`, `remote: false` (local, default) or `remote: true` (remote-tracking).

---

### `gitStatus`
**When**: You want to know the current state of a repo — what's staged, modified, untracked.
**Returns**: branch, ahead/behind counts, staged files, modified files, untracked files.

---

### `fileHistory`
**When**: You want a compact changelog for one specific file — how has it evolved?
**Key params**: `repo`, `path`, `maxEntries` (default 20), `offset` (pagination), `since`/`until`.
**Follow-up**: Use `readFileAtRef` with the returned `hash` values to see the file at a specific point.

---

### `resolveJiraTag`
**When**: You have a Jira ticket number (e.g. `DEV-54608`) and want to find the associated commits.
**Key params**: `tag` (the Jira tag), `repo` (optional — omit to search all repos).
**Returns**: repo, hash, author, date, message, branch for each matching commit.

---

### `collectReviewBundle`
**When**: You're doing a full code review and want everything in one call.
**Key params**:
- `jiraTag` — find all commits across repos that mention this tag (e.g. `DEV-54554`)
- `commit` — review a single commit hash
- `repo` — restrict search to a specific repository (optional)
- `format` — output format: `"markdown"` (default) or `"json"`
**Returns**: By default, returns a fully formatted Markdown document with commit stats, Jira metadata (fetched using `jv`), and the diffs. If `format: "json"` is requested, returns the structured JSON payload.

**Workflow**:
1. Call `collectReviewBundle` to get the Markdown overview.
2. If any diff was truncated (indicated in a warning box in the markdown), use `gitDiff` or `gitShow` with `path=` to fetch individual file diffs.
3. For context on specific lines, use `gitBlame`.
4. For a file's full history, use `fileHistory`.

---

## Output size limits

Every tool caps output at 100KB. When truncation occurs, the response includes an `omitted` field explaining what was cut and how to fetch the rest. Never assume a truncated response is complete — always check `omitted`.

## Error handling

Tool errors are returned as `isError: true` in the MCP response with a message describing the problem. Common causes: invalid repo name, path escaping the workspace, git command failure (e.g. invalid ref).
