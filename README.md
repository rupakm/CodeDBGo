# CodeDB

A code indexing and search system that enables Sourcegraph-style queries against
repositories and their full history. Embedded, built in Go.

## Features

- **Sourcegraph-style search** — intuitive query language with filters like `repo:`, `lang:`, `file:`, `type:`, `calls:`, `returns:`
- **Full-text code search** — literal, regex, phrase, and term search over file contents via Bleve
- **Diff search** — find commits that introduced or modified specific code
- **Commit search** — search commit metadata by author, date range, and message
- **Symbol search** — find functions, structs, classes, traits, and other symbols via tree-sitter
- **Cross-reference queries** — `calls:fn` to find callers, `calledby:fn` to find callees
- **Type-aware search** — `returns:Type` to find functions by return type
- **Rich SQL queries** — filter by repo, branch, file path, language, commit metadata
- **Content-addressable dedup** — mirrors git's blob model; identical files across branches/commits are stored and indexed once
- **Incremental updates** — re-indexing is proportional to new commits, not total repo size
- **Embedded** — no external servers; everything runs in-process

## Quick Start

```bash
# Build (no CGO required — indexing and search work, but no symbol extraction)
make build

# Build with symbol extraction (requires C compiler)
make build-cgo

# Index a repository (clones, walks history, extracts symbols if CGO build)
codedb index https://github.com/user/repo

# Search for code (Sourcegraph-style query)
codedb search "function_name"

# Filtered search
codedb search "lang:rust file:*.rs -file:test serialize"

# Regex search
codedb search "/fn\s+process_\w+/"

# OR search: match either term
codedb search "serialize OR deserialize"

# Find symbols
codedb search "type:symbol select:symbol.function SFrame"

# Cross-reference: who calls groupby()?
codedb search "calls:groupby"

# Type info: functions returning BatchIterator
codedb search "returns:BatchIterator"

# Diff search: commits that touched "streaming"
codedb search "type:diff file:*.rs streaming"

# Commit search by author
codedb search "type:commit author:alice parallel"

# Show generated SQL instead of executing
codedb search --sql "lang:rust file:*.rs serialize"

# JSON output
codedb search --json "lang:go func main"

# Override result count
codedb search --count 50 "lang:go func main"

# Run arbitrary SQL
codedb sql "
  SELECT fr.path, b.language
  FROM blobs b
  JOIN file_revs fr ON fr.blob_id = b.id
  JOIN refs r ON r.commit_id = fr.commit_id
  WHERE r.name = 'refs/heads/main'
  ORDER BY fr.path
  LIMIT 10
"
```

## Search Query Syntax

The `search` command uses a [Sourcegraph-compatible query language](#differences-from-sourcegraph).
Bare words are search terms; filters use `key:value` syntax.

### Filters

| Filter | Description | Example |
|--------|-------------|---------|
| `repo:` / `-repo:` | Include / exclude by repository name; supports `@rev` | `repo:myrepo@main` |
| `file:` / `-file:` | Include / exclude file paths | `file:*.rs -file:test` |
| `lang:` / `-lang:` | Include / exclude by language | `lang:rust` / `-lang:python` |
| `type:` | Search type: `code`, `diff`, `commit`, `symbol` | `type:symbol` |
| `rev:` | Branch or ref (default: `refs/heads/main`) | `rev:develop` |
| `select:` | Output format: `repo`, `file`, `symbol`, `symbol.KIND` | `select:symbol.function` |
| `count:` | Max results (default: 20) | `count:50` |
| `case:` | Case sensitivity (`yes`/`no`) | `case:yes` |
| `author:` / `-author:` | Include / exclude commit/diff author | `author:alice` / `-author:bot` |
| `before:` / `after:` | Date range for commits/diffs | `after:2024-01-01` |
| `message:` / `-message:` | Include / exclude by commit message | `message:refactor` / `-message:WIP` |
| `calls:` | Find functions that call a given function | `calls:groupby` |
| `calledby:` | Find functions called by a given function | `calledby:groupby` |
| `returns:` | Find functions returning a given type | `returns:SFrame` |
| `patterntype:` | Pattern interpretation: `literal`, `keyword`, `regexp` | `patterntype:regexp` |

### Search Patterns

- **Bare words** — `foo bar` matches files containing both terms (implicit AND).
- **Quoted phrases** — `"error handling"` matches the exact phrase.
- **OR operator** — `foo OR bar` matches files containing either term. Works in all search types.
- **Regex** — `/pattern/` or `patterntype:regexp pattern` for regex matching (code and diff search only).

### Search Types

- **`type:code`** (default) — Full-text search across file contents. Returns path, score, and snippet.
- **`type:symbol`** — Search extracted symbols. Use `select:symbol.KIND` to filter by kind (function, struct, class, etc.).
- **`type:diff`** — Search within commit diffs. Supports `author:`, `before:`, `after:`, `file:` filters.
- **`type:commit`** — Search commit metadata. Supports `author:`, `before:`, `after:`, `message:` filters.

## Differences from Sourcegraph

CodeDB implements a subset of the
[Sourcegraph query language](https://sourcegraph.com/docs/code-search/queries),
tailored for searching one or a few locally-indexed repositories rather than
a large multi-tenant instance. Most simple Sourcegraph queries work unchanged.

### What works the same

The core query experience is compatible:

- **Bare search terms** — `error handling` searches file contents, just like Sourcegraph.
- **Quoted phrases** — `"parse error"` matches the exact phrase.
- **Filters** — `repo:`, `file:`, `lang:` (alias `l:`), `rev:`, `case:`,
  `type:` (code/diff/commit/symbol), `select:`, `count:`, `author:`,
  `before:`, `after:`, `message:` all work as expected.
- **Negation** — `-file:`, `-repo:`, `-lang:`, `-author:`, `-message:` all supported.
- **`@revision`** — `repo:foo@branch` works, equivalent to `repo:foo rev:branch`.
- **`patterntype:`** — `literal`, `keyword`, and `regexp` all supported.
- **OR operator** — `foo OR bar` works across all search types.
- **Regex** — `/pattern/` syntax for code and diff search.

### What's different

| Area | Sourcegraph | CodeDB |
|------|------------|--------|
| **Scope** | Searches across thousands of repositories on a hosted instance. Has `fork:`, `archived:`, `visibility:`, `repogroup:`, `repo:has.file()`, `repo:has.path()`, `file:has.owner()` for filtering across a large corpus. | Designed for one or a few locally-indexed repos. Repository-level metadata filters (`fork:`, `archived:`, `visibility:`, `repogroup:`, `repo:has.*`, `file:has.*`) are not supported — they don't apply at this scale. |
| **Regex** | Supports `/regex/` patterns and `patterntype:regexp`. | Supported for code and diff search via `/pattern/` or `patterntype:regexp`. Not available for symbol or commit search (use raw SQL). |
| **Boolean operators** | Full `AND`, `OR`, `NOT` with parenthesized grouping. | `OR` supported between search terms (`foo OR bar`). All other terms are implicitly AND'ed. No `NOT` or parentheses. Negation supported for filters (`-file:`, `-repo:`, `-lang:`, `-author:`, `-message:`). |
| **Structural search** | `type:structural` with [Comby](https://comby.dev/) patterns for syntax-aware matching. | Not supported. CodeDB addresses similar use cases through symbol extraction and cross-reference queries instead. |
| **Code intelligence** | Separate LSIF/SCIP-based precise code navigation (go-to-definition, find-references). Not part of the query language. | Built into the query language via `calls:`, `calledby:`, and `returns:` filters. Uses tree-sitter extraction — less precise than LSIF/SCIP but requires no separate indexing pipeline. |
| **`timeout:`, `stable:`** | Query execution controls. | Not supported (queries run locally and complete fast). |

### Porting queries from Sourcegraph

Most queries port directly:

| Sourcegraph query | CodeDB equivalent |
|---|---|
| `lang:go fmt.Sprintf` | `lang:go fmt.Sprintf` (identical) |
| `file:\.py$ import requests` | `file:*.py import requests` (use GLOB wildcards for file filters) |
| `repo:myorg/myrepo error` | `repo:myrepo error` (substring match on repo name) |
| `type:diff author:alice fix` | `type:diff author:alice fix` (identical) |
| `type:symbol lang:rust Iterator` | `type:symbol lang:rust Iterator` (identical) |
| `repo:foo@develop query` | `repo:foo@develop query` (identical — `@` syntax supported) |
| `patterntype:regexp err\d+` | `patterntype:regexp err\d+` (identical) or `/err\d+/` |
| `foo OR bar lang:go` | `lang:go foo OR bar` (identical) |
| `(foo OR bar) AND baz` | Not supported — no parenthesized grouping |

**When the query language isn't enough**, use `codedb search --sql` to see the
generated SQL, then adapt it with `codedb sql` for full control — including
JOINs, aggregations, and anything else SQLite supports.

## Symbol Extraction

CodeDB uses tree-sitter to extract symbols from source code during indexing.
Symbol extraction requires building with CGO enabled (`make build-cgo`).
When built without CGO (the default), indexing and search still work but
symbol-related features (`type:symbol`, `calls:`, `calledby:`, `returns:`)
return no results.

### Supported Languages

| Language | Symbol Types |
|----------|-------------|
| Rust | function, struct, enum, trait, impl, const, static, module |
| Python | function, class |
| JavaScript | function, class, method, interface, enum, type_alias |
| TypeScript | function, class, method, interface, enum, type_alias |
| TSX | function, class, method, interface, enum, type_alias |
| Go | function, method, type |
| C | function, struct, enum |
| C++ | function, struct, enum, class, namespace |

### What's Extracted

- **Symbols** — name, kind, location (line/column), full signature
- **Type info** — return types, parameter lists
- **Scope nesting** — methods within classes/impls tracked via parent relationships
- **Call references** — function call sites with containing symbol context

## Architecture

```
┌─────────────────────────────────────┐
│          codedb (CLI)               │
├─────────────────────────────────────┤
│          codedb (library)           │
│  ┌───────────┐  ┌────────────────┐  │
│  │  SQLite   │  │     Bleve      │  │
│  │ (metadata,│  │ (code search,  │  │
│  │  DAG,     │  │  diff search)  │  │
│  │  file_revs│  │                │  │
│  └───────────┘  └────────────────┘  │
│  ┌───────────┐  ┌────────────────┐  │
│  │  go-git   │  │  tree-sitter   │  │
│  │ (git ops) │  │ (symbols,      │  │
│  │           │  │  call refs)    │  │
│  │           │  │ CGO only       │  │
│  └───────────┘  └────────────────┘  │
├─────────────────────────────────────┤
│   Planner (SQL / Bleve / Intersect) │
└─────────────────────────────────────┘
```

The query planner determines execution strategy:

| Strategy | When | How |
|----------|------|-----|
| **SQL only** | Commit, symbol, and call queries | Direct SQL against SQLite |
| **Bleve only** | Code/diff search with no metadata filters | Full-text search via Bleve |
| **Intersect** | Code/diff search with metadata filters | Bleve for relevance, SQL for filtering |

## Database Schema

| Table | Description |
|-------|-------------|
| `repos` | Indexed repositories |
| `commits` | Commit metadata (hash, author, message, timestamp) |
| `commit_parents` | Commit parent relationships (DAG) |
| `refs` | Branch/tag refs pointing to commits |
| `blobs` | Unique file contents (content-addressable by SHA) |
| `file_revs` | Files present at each ref tip |
| `diffs` | Per-file diffs for each commit |
| `symbols` | Extracted symbols (name, kind, signature, return type, params) |
| `symbol_refs` | Call sites and references between symbols |

## Data Directory Layout

```
~/.local/share/sageox/codedb/
  metadata.db                        # SQLite database
  bleve/code/                        # Bleve index for file contents
  bleve/diff/                        # Bleve index for commit diffs
  repos/{directory}.git/             # Bare git clones
```

## Example SQL Queries

```sql
-- Most called functions
SELECT sr.ref_name AS function, COUNT(*) AS calls
FROM symbol_refs sr
JOIN blobs b ON b.id = sr.blob_id
JOIN file_revs fr ON fr.blob_id = b.id
JOIN refs r ON r.commit_id = fr.commit_id
WHERE r.name = 'refs/heads/main'
  AND sr.kind = 'call'
GROUP BY sr.ref_name
ORDER BY calls DESC
LIMIT 15

-- Functions with specific parameter types
SELECT DISTINCT fr.path || ':' || s.line AS location, s.params
FROM symbols s
JOIN blobs b ON b.id = s.blob_id
JOIN file_revs fr ON fr.blob_id = b.id
JOIN refs r ON r.commit_id = fr.commit_id
WHERE s.params LIKE '%SFrame%'
  AND s.kind = 'function'
  AND r.name = 'refs/heads/main'

-- Language breakdown of a repo
SELECT b.language, COUNT(*) as file_count
FROM blobs b
JOIN file_revs fr ON fr.blob_id = b.id
JOIN refs r ON r.commit_id = fr.commit_id
WHERE r.name = 'refs/heads/main'
GROUP BY b.language
ORDER BY file_count DESC
```

## Library Usage

```go
import (
	"context"
	"github.com/sageox/codedbgo/internal/codedb"
	"github.com/sageox/codedbgo/internal/codedb/index"
)

db, err := codedb.Open("/path/to/data")
if err != nil {
    log.Fatal(err)
}
defer db.Close()

ctx := context.Background()

// Index a repository (clones bare, walks history, extracts symbols)
err = db.IndexRepo(ctx, "https://github.com/user/repo", index.IndexOptions{})

// Re-index later (incremental — only processes new commits)
err = db.IndexRepo(ctx, "https://github.com/user/repo", index.IndexOptions{})

// Sourcegraph-style search
results, err := db.Search(ctx, "lang:rust type:symbol SFrame")

// Or query via SQL directly
cols, rows, err := db.RawSQL("SELECT fr.path FROM file_revs fr LIMIT 10")
```

## Building

```bash
# Default build (CGO_ENABLED=0, no tree-sitter symbol extraction)
make build

# Build with tree-sitter symbol extraction (requires C compiler)
make build-cgo
```

Requires Go 1.25+. The default build uses `CGO_ENABLED=0` and needs no C
compiler — SQLite uses a pure-Go driver and symbol extraction is disabled.

To enable tree-sitter symbol extraction, build with `make build-cgo` (requires
a C compiler for the tree-sitter bindings).

```bash
# Run tests (no CGO)
make test

# Run all tests including tree-sitter symbol tests
make test-cgo

# Install to $GOPATH/bin
make install
```

## License

BSD-3-Clause
