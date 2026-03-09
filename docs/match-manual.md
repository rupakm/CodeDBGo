# codedb match -- Structural Pattern Matching

`codedb match` finds code patterns across indexed repositories using
structure-aware matching. Unlike text search (`codedb search`), it understands
code structure -- function calls, assignments, conditionals, and nesting.

## Quick Start

```bash
# Index a repository first
codedb index https://github.com/user/repo

# Find all calls to foo() with any arguments
codedb match 'foo(...)' --lang python

# Find calls where the same argument is passed twice
codedb match 'foo($X, ..., $X)' --lang go

# Find self-assignments (likely bugs)
codedb match '$X = $X' --lang typescript
```

## Pattern Syntax

Patterns look like the code you're searching for, with special operators that
act as placeholders.

### Metavariables: `$X`

A `$` followed by uppercase letters matches any single expression and binds it
to a name. The same metavariable used twice in a pattern must match the same
text (unification).

```bash
# Match any function call and capture the function name
codedb match '$F()' --lang python

# Match calls where the first and last arguments are identical
codedb match '$F($X, ..., $X)' --lang go
# Matches: process(config, validate(), config)
# Skips:   process(config, validate(), other)
```

Metavariables can appear anywhere an expression or identifier would:

```bash
# Object method calls
codedb match '$OBJ.save()' --lang python

# Binary operations
codedb match '$X == $X' --lang go        # always-true comparison

# Assignments
codedb match '$X = None' --lang python   # assigned to None
```

### Anonymous Metavariable: `$_`

Matches any single expression without binding. Use when you need to match
something but don't care about its value.

```bash
# Function with exactly 3 arguments, don't care what they are
codedb match 'foo($_, $_, $_)' --lang python
```

### Ellipsis: `...`

Matches zero or more items in a sequence -- arguments, statements, or fields.

```bash
# Any call to requests.get with verify=False anywhere in the args
codedb match 'requests.get(..., verify=False, ...)' --lang python

# A function that calls dangerous_op somewhere in its body
codedb match 'def $F(...): ... dangerous_op(...) ...' --lang python

# Any call with at least one argument
codedb match 'foo($X, ...)' --lang go
```

The ellipsis is greedy but backtracks -- it tries consuming as many items as
possible, then fewer, until the rest of the pattern matches.

### Ellipsis Metavariable: `$...ARGS`

Like `...` but captures the matched items.

```bash
# Capture all arguments before a specific one
codedb match 'foo($...BEFORE, "sentinel", $...AFTER)' --lang python
```

### Deep Expression: `<... P ...>`

Matches a pattern nested arbitrarily deep inside an expression. Useful for
finding a specific call buried in a complex condition.

```bash
# Find if-statements that check is_admin() anywhere in the condition
codedb match 'if <... $X.is_admin() ...>: ...' --lang python

# Matches all of:
#   if user.is_admin(): ...
#   if user.authenticated() and user.is_admin(): ...
#   if (check_role() or user.is_admin()) and active: ...
```

## Matching Constructs

### Function Calls

```bash
# Direct call
codedb match 'foo($X)' --lang go

# Method call
codedb match '$OBJ.method($X)' --lang python

# Chained calls
codedb match '$X.foo().bar()' --lang typescript

# Nested calls
codedb match 'outer(inner($X))' --lang python
```

### Assignments

```bash
# Any assignment to a specific variable
codedb match 'password = $X' --lang python

# Short variable declaration (Go)
codedb match '$X := $Y' --lang go

# Self-assignment (bug pattern)
codedb match '$X = $X' --lang typescript
```

### Conditionals

```bash
# Python
codedb match 'if $COND: ...' --lang python

# Go
codedb match 'if $COND { ... }' --lang go

# TypeScript
codedb match 'if ($COND) { ... }' --lang typescript
```

### Function Definitions

```bash
# Python function
codedb match 'def $F(...): ...' --lang python

# Go function
codedb match 'func $F(...) { ... }' --lang go

# TypeScript function
codedb match 'function $F(...) { ... }' --lang typescript
```

### Binary Operations

```bash
# Null comparisons
codedb match '$X == null' --lang typescript
codedb match '$X == None' --lang python
codedb match '$X == nil' --lang go

# Redundant comparisons
codedb match '$X == $X' --lang go

# String concatenation with user input
codedb match '"SELECT * FROM " + $X' --lang typescript
```

## Filtering Results

### Boolean Combinators

Use flags to combine patterns with boolean logic.

**`--not`**: Exclude matches that also match another pattern.

```bash
# Find os.system calls, but not the safe ones
codedb match 'os.system($CMD)' --lang python \
  --not 'os.system("ls")'

# Find eval(), but not when called with a string literal
codedb match 'eval($X)' --lang typescript \
  --not 'eval("...")'
```

**`--inside`**: Only return matches that occur inside a larger pattern.

```bash
# Only find print() calls inside test functions
codedb match 'print($X)' --lang python \
  --inside 'def test_$F(...): ...'

# Only find assignments inside if blocks
codedb match '$X = $Y' --lang go \
  --inside 'if $COND { ... }'
```

**`--not-inside`**: Exclude matches inside a pattern.

```bash
# Find TODO comments, but not in test files
codedb match '$X' --lang python \
  --not-inside 'def test_$F(...): ...'
```

These flags are repeatable. Multiple `--not` flags all apply (AND logic).
Multiple `--inside` flags all apply (match must be inside all of them).

### Metavariable Constraints

Use `--where` to add constraints on metavariable values.

**Regex match** (`~`):
```bash
# Find functions whose names start with "unsafe_"
codedb match 'def $F(...): ...' --lang python \
  --where '$F ~ /^unsafe_/'

# Find calls to methods ending in "_sync"
codedb match '$X.$M()' --lang typescript \
  --where '$M ~ /_sync$/'
```

**Regex negation** (`!~`):
```bash
# Find os.system calls where the argument isn't a string literal
codedb match 'os.system($CMD)' --lang python \
  --where '$CMD !~ /^"/'
```

**Equality** (`==`, `!=`):
```bash
# Find cases where left and right of comparison are different variables
codedb match '$X == $Y' --lang go \
  --where '$X != $Y'
```

**Numeric comparison** (`<`, `>`, `<=`, `>=`):
```bash
# Find sleep calls with large values
codedb match 'time.Sleep($X)' --lang go \
  --where '$X > 1000'
```

Multiple `--where` flags apply as AND.

## Scoping Flags

### `--lang` (required)

Specify the target language. Supported in v1: `go`, `python`, `typescript`.

```bash
codedb match '$X == nil' --lang go
```

### `--repo`

Restrict matching to a specific repository.

```bash
codedb match 'eval($X)' --lang typescript --repo my-app
```

### `--file`

Restrict matching to files matching a glob pattern.

```bash
codedb match 'os.system($X)' --lang python --file '*.py'
codedb match '$X.query($SQL)' --lang python --file 'src/**/*.py'
```

### `--count`

Limit the number of results.

```bash
codedb match 'fmt.Println(...)' --lang go --count 10
```

## Output

### Default (human-readable)

```
github.com/user/repo src/handler.py:42:5: os.system(user_input)
  $CMD = "user_input"

github.com/user/repo src/utils.py:118:9: os.system(cmd)
  $CMD = "cmd"

2 matches found
```

Each match shows:
- Repository, file path, line, column
- The matched source text
- Metavariable bindings

### JSON (`--json`)

```bash
codedb match 'os.system($CMD)' --lang python --json
```

```json
{
  "matches": [
    {
      "file": "src/handler.py",
      "repo": "github.com/user/repo",
      "line": 42,
      "col": 5,
      "end_line": 42,
      "end_col": 30,
      "text": "os.system(user_input)",
      "bindings": {
        "CMD": "user_input"
      }
    }
  ],
  "total": 2
}
```

## How It Works

`codedb match` uses a hybrid execution strategy:

1. **Pre-filter**: Extract literal names from the pattern (e.g. `foo` from
   `foo($X)`) and use CodeDB's existing symbol/reference indexes to narrow
   candidates. This avoids parsing every file.

2. **AST match**: Parse each candidate file with tree-sitter and walk the AST,
   trying to match the pattern against every node. Metavariables bind on first
   occurrence and must unify on subsequent occurrences.

3. **Post-filter**: Apply `--not`, `--inside`, `--not-inside`, and `--where`
   constraints to filter the raw matches.

This means matching is fast even on large codebases -- if you search for
`foo($X)`, only files that are known to reference `foo` are parsed.

## Examples: Common Bug Patterns

### Security

```bash
# SQL injection via string concatenation
codedb match '"SELECT " + $X' --lang python
codedb match 'cursor.execute("SELECT " + $X)' --lang python

# Hardcoded secrets
codedb match 'password = "$X"' --lang python
codedb match '$X = "AKIA..."' --lang python \
  --where '$X ~ /key|secret|token/i'

# Command injection
codedb match 'os.system($X)' --lang python \
  --not 'os.system("...")'

# Eval with non-literal argument
codedb match 'eval($X)' --lang typescript \
  --where '$X !~ /^"/'
```

### Correctness

```bash
# Self-assignment (dead code)
codedb match '$X = $X' --lang go

# Always-true/false comparisons
codedb match '$X == $X' --lang python
codedb match '$X != $X' --lang typescript

# Unused return value from important functions
codedb match '$F(...)' --lang go \
  --where '$F ~ /^(Close|Flush|Write)$/' \
  --not '$_ = $F(...)'
```

### Code Quality

```bash
# Empty except blocks (Python)
codedb match 'def $F(...): ... pass' --lang python

# Functions with too many parameters
codedb match 'def $F($A, $B, $C, $D, $E, $F, ...)' --lang python

# Console.log left in production code
codedb match 'console.log(...)' --lang typescript \
  --not-inside 'function test_$F(...) { ... }'
```

## Comparison with codedb search

| Feature | `codedb search` | `codedb match` |
|---|---|---|
| **Approach** | Text/keyword search | Structure-aware AST matching |
| **Engine** | Bleve full-text + SQL | SQL pre-filter + tree-sitter AST |
| **Patterns** | Literals, regex, phrases | Metavariables, ellipsis, deep expressions |
| **Understands** | Text content, file paths, symbols | Code structure, nesting, arguments |
| **Use case** | "Find files containing X" | "Find code shaped like X" |
| **Speed** | Fast (pre-indexed) | Moderate (parses candidates on-the-fly) |

Use `search` when you know what text to look for. Use `match` when you know
what code structure to look for.
