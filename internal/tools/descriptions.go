package tools

const BashDescription = `Execute a shell command and return stdout and stderr.

IMPORTANT: Avoid using this tool to run cat, head, tail, sed, awk, or echo commands. Instead use the dedicated ReadFile, EditFile, or WriteFile tools which provide a better experience.

Usage notes:
- The working directory persists between commands, but shell state does not.
- Always quote file paths containing spaces with double quotes.
- Try to maintain your current working directory using absolute paths; avoid cd unless the user explicitly requests it.
- Optional timeout in seconds (max 600). Default is 120s.
- When issuing multiple independent commands, make separate parallel tool calls instead of chaining with &&.
- Use && to chain sequential dependent commands. Use ; only when you don't care if earlier commands fail.
- DO NOT use newlines to separate commands.

Git Safety Protocol:
- NEVER run destructive git commands (push --force, reset --hard, checkout ., clean -f, branch -D) unless the user explicitly requests it.
- NEVER skip hooks (--no-verify) unless the user explicitly requests it.
- Prefer creating a new commit rather than amending an existing one.
- Before running destructive operations, consider safer alternatives.

Avoid unnecessary sleep commands. Do not retry failing commands in a sleep loop — diagnose the root cause instead.
When using find, search from "." or a specific path, not "/" — scanning the full filesystem is too expensive.`

const ReadFileDescription = `Read a file and return its contents with line numbers.

Usage notes:
- The file_path parameter should be an absolute path when possible.
- By default reads up to 2000 lines from the beginning of the file.
- Use offset and limit to read specific parts of large files. Only read what you need.
- Results are returned with line numbers (1-based) for easy reference.
- This tool can only read files, not directories. Use Glob to list directory contents.
- Do NOT re-read a file you just edited to verify — EditFile would have errored if the change failed.`

const EditFileDescription = `Replace an exact string in a file. The old_string must appear exactly once in the file.

Usage notes:
- You MUST read the file with ReadFile before editing. This tool will fail otherwise.
- When editing text from ReadFile output, preserve the exact indentation (tabs/spaces) as shown.
- ALWAYS prefer editing existing files over creating new ones.
- The edit will FAIL if old_string is not unique in the file. Provide more surrounding context to make it unique.
- Use the smallest old_string that is clearly unique — 2-4 adjacent lines is usually sufficient.
- The new_string must be different from old_string.`

const WriteFileDescription = `Write content to a file, creating parent directories if needed. Overwrites existing files.

Usage notes:
- If modifying an existing file, prefer EditFile over WriteFile — it only sends the diff.
- Use this tool only to create new files or for complete rewrites.
- You MUST read existing files with ReadFile before overwriting them.
- NEVER create documentation files (*.md) or README files unless explicitly requested.`

const GlobDescription = `Find files matching a glob pattern, returning relative paths sorted alphabetically.

Usage notes:
- Supports patterns like "**/*.py", "src/**/*.ts", "*.go".
- Search from "." or a specific path, never from "/".
- Automatically skips .git, node_modules, __pycache__, and similar directories.
- Use this instead of find or ls commands via Bash.`

const GrepDescription = `Search file contents using a regex pattern, returning file:line:content matches.

Usage notes:
- Supports full regex syntax (e.g. "log.*Error", "func\s+\w+").
- Filter files by the include parameter (e.g. "*.py", "*.go").
- Search from "." or a specific path, never from "/".
- Use this instead of grep or rg commands via Bash.
- Automatically skips .git, node_modules, __pycache__, and similar directories.`

const RagIndexDescription = `Index a file or directory into the RAG vector store for semantic search.

Usage notes:
- Pass a file or directory path (relative to cwd or absolute).
- Directories are scanned recursively; .git/node_modules/vendor etc. are skipped.
- Files are chunked (by sliding window for code, by paragraph for docs) and embedded.
- Requires embedding_model to be configured in .mewcode/config.yaml.
- Re-indexing the same path replaces all previous entries (full rebuild).`

const RagSearchDescription = `Semantic search over indexed chunks using a three-stage retrieval pipeline.

Usage notes:
- Query in natural language; returns top_k (default 5) most relevant chunks.
- Stage 1 (coarse): vector-similarity retrieval fetches up to 50 candidates.
- Stage 2 (rerank): if a reranker is configured (rerank_model), a cross-encoder re-scores the candidates.
- Stage 3 (LLM judge): an LLM re-evaluates the candidates against the query and selects the most relevant ones.
- Each result includes file_path, line range, content, and the final relevance score.
- If any stage fails, the pipeline falls back to the best available prior stage's results.
- Use this when grep/glob cannot find what you need (semantic rather than exact match).
- Requires the target files to have been indexed via RagIndex first.`

const RagClearDescription = `Clear the RAG vector store, removing all indexed chunks and metadata.`
