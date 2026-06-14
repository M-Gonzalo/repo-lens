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
**Key params**:
- `repo` — the repository name
- `path` — relative path to the file
- `ref` — commit, branch, or tag (default: HEAD)
- `startLine` / `endLine` — line window range (optional)
- `format` — output format: `"text"` (default) or `"json"`

---

### `gitLog`
**When**: You want to understand recent activity, find when something changed, or filter commits by author/date/path.
**Key params**:
- `maxCount` — default 20; increase for broader history
- `offset` — for pagination
- `path` — restrict to commits touching a specific file
- `grep` — filter by commit message substring (e.g. a Jira tag)
- `author`, `since`, `until` — date and author filters
- `format` — output format: `"text"` (default) or `"json"`

---

### `gitDiff`
**When**: You want to see what changed between two points, or what's currently uncommitted in the working tree.
**Key params**:
- `dirty: true` — diff the working tree against HEAD; shows all uncommitted changes including untracked files. **Ignores `base` and `target`.** Use this to see what's in progress before it's committed.
- `base` — default `HEAD~1`; can be a branch name, commit hash, or tag
- `target` — default `HEAD`
- `stat: true` — returns only the summary (files changed, insertions, deletions) without the full diff
- `path` — scope to a single file when the full diff is too large
- `format` — output format: `"text"` (default) or `"json"`

**Pagination**: If `omitted.truncated` is true, use `path=` with each file from `omitted.affectedFiles` to fetch individual file diffs.

---

### `gitShow`
**When**: You want the full picture of a single commit — author, message, and diff.
**Key params**:
- `repo` — the repository name
- `ref` — commit hash or branch (default: HEAD)
- `path` — scope the diff to a single file (optional)
- `format` — output format: `"text"` (default) or `"json"`

**Pagination**: Same as `gitDiff` — use `path=` for large commits.

---

### `gitBlame`
**When**: You need to know who last touched specific lines and in which commit.
**Key params**:
- `repo` — the repository name
- `path` — relative path to the file
- `startLine` / `endLine` — line window range (optional)
- `format` — output format: `"text"` (default) or `"json"`

---

### `gitBranches`
**When**: You need to see what branches exist or find a feature branch.
**Key params**: `repo`, `remote: false` (local, default) or `remote: true` (remote-tracking).

---

### `gitStatus`
**When**: You want to know the current state of a repo — what's staged, modified, untracked.
**Key params**:
- `repo` — the repository name
- `format` — output format: `"text"` (default) or `"json"`
**Returns**: branch, ahead/behind counts, staged files, modified files, untracked files.

---

### `fileHistory`
**When**: You want a compact changelog for one specific file — how has it evolved?
**Key params**:
- `repo` — the repository name
- `path` — relative path to the file
- `maxEntries` / `offset` — pagination
- `since` / `until` — date filters
- `format` — output format: `"text"` (default) or `"json"`
**Follow-up**: Use `readFileAtRef` with the returned `hash` values to see the file at a specific point.

---

### `resolveJiraTag`
**When**: You have a Jira ticket number (e.g. `DEV-54608`) and want to find the associated commits.
**Key params**: `tag` (the Jira tag), `repo` (optional — omit to search all repos).
**Returns**: repo, hash, author, date, message, branch for each matching commit.

---

### `research`
**When**: You need to answer a question that would require reading many files to answer — architecture overviews, tracing a flow across layers, understanding how subsystems connect. Use this instead of opening files one by one.
**Key params**:
- `question` — the question to research; be specific with module names or file paths if known
- `repo` — optional; scope the investigation to a single repository
- `context` — optional array of file paths the agent should read before investigating; use this to provide any relevant background — prior research results, design docs, TDDs, specs, notes, or anything else that would help inform the answer

**Returns**: A structured answer with evidence and file references. The result is automatically saved to `{workspace}/.opencode/research/<timestamp>.md` and the path is appended to the response — pass it as `context` in follow-up calls to chain investigations.

**Tip**: The saved file path returned in the response can itself be passed as `context` in a follow-up call, along with any other relevant files.

**Note**: Requires `opencode` to be installed and in `PATH`.

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
