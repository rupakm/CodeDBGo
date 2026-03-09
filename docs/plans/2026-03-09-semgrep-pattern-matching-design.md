# Semgrep-Style Structural Pattern Matching for CodeDB

**Date**: 2026-03-09
**Status**: Approved
**Approach**: C — Custom AST Walker with Pattern IR (selected over A: Pattern-to-AST and B: Tree-sitter Query Language)

## Goal

Add Semgrep-style structural pattern matching to CodeDB via a `codedb match`
subcommand. Users write code-like patterns with metavariables and ellipsis
operators to find structural code patterns across indexed repositories.

## v1 Scope

- Pattern language with metavariables (`$X`), ellipsis (`...`), deep expressions (`<... ...>`)
- Boolean combinators: `--not`, `--inside`, `--not-inside`
- Metavariable constraints: `--where '$X ~ /regex/'`
- Languages: Go, Python, TypeScript
- Hybrid execution: SQL pre-filter then live AST matching
- Separate `codedb match` subcommand

## Deferred

- Semantic features (constant propagation, import aliasing)
- Taint analysis (source/sink/sanitizer dataflow)
- Autofix (templated code replacement)
- YAML rule files (`codedb scan`)
- Additional languages (Rust, C, C++, JavaScript, TSX)

## Alternative Approaches (for re-evaluation)

### Approach A: Pattern-to-AST Matching

Parse the pattern string itself as code using tree-sitter, with special handling
for `$X` and `...`. Match the pattern AST against target ASTs node-by-node.

**Pros**: Patterns naturally handle all syntax. How Semgrep works.
**Cons**: Requires grammar hacks per language to parse `$X`/`...` as valid code.

### Approach B: Tree-sitter Query Language

Translate user patterns into tree-sitter S-expression queries, using the
built-in query engine.

**Pros**: Leverages existing tree-sitter query machinery.
**Cons**: Translation is extremely complex. Grammar-specific node names. No
native metavariable unification. gotreesitter DFA issues affect queries.

---

## 1. Pattern Language Syntax

Users write patterns that look like the target language, with special constructs:

```
$X              metavariable -- matches any single expression/identifier, binds to name X
$...X           ellipsis metavariable -- matches zero or more items, binds to name X
...             anonymous ellipsis -- matches zero or more items (no binding)
$_              anonymous metavariable -- matches any single item (no binding)
<... P ...>     deep expression -- matches P nested arbitrarily deep inside an expression
```

### Examples

```python
# Pattern: foo($X, ..., $X)
# Matches: foo(a, b, c, a)  -- $X binds to "a", used twice
# Skips:   foo(a, b, c, d)  -- $X doesn't unify

# Pattern: $X = $X
# Matches: x = x  (self-assignment, likely a bug)

# Pattern: $F(..., None, ...)
# Matches: any function call with None as an argument

# Pattern: if <... $X.is_admin() ...>: ...
# Matches: if user.authenticated() and user.is_admin() and user.has_role("x"):
```

### CLI Usage

```bash
# Ad-hoc pattern matching
codedb match 'foo($X, ..., $X)' --lang python
codedb match 'func($F) { ... }' --lang go
codedb match '$X == null' --lang typescript --repo TraceForge

# Boolean combinators via flags
codedb match 'os.system($CMD)' --lang python \
  --not 'os.system("ls")'

codedb match 'eval($X)' --lang python \
  --inside 'def $F(...): ...'

# Metavariable constraints via flags
codedb match '$F($X)' --lang python \
  --where '$F ~ /^unsafe_/' \
  --where '$X != "safe_value"'
```

## 2. Pattern IR (Intermediate Representation)

The pattern parser converts user patterns into a tree of IR nodes. This IR is
language-independent -- the same IR matches against Go, Python, or TypeScript
ASTs.

### IR Node Types

```go
type PatternNode interface{ patternNode() }

// Atoms
type Literal struct {          // exact text: "foo", "None", "null", "42"
    Value string
}
type Metavar struct {           // $X -- bind/unify by name
    Name string
}
type Ellipsis struct{}          // ... -- skip zero or more
type EllipsisMetavar struct {   // $...X -- skip zero or more, bind
    Name string
}
type Wildcard struct{}          // $_ -- match any single node

// Compound
type Call struct {              // foo(...) or $F(...)
    Func PatternNode            // Literal("foo") or Metavar("F")
    Args []PatternNode          // [Metavar("X"), Ellipsis{}, Metavar("X")]
}
type MethodCall struct {        // $X.foo(...) or $X.$M(...)
    Object PatternNode
    Method PatternNode
    Args   []PatternNode
}
type BinaryOp struct {          // $X == $Y, $X + $Y
    Left  PatternNode
    Op    string
    Right PatternNode
}
type Assignment struct {        // $X = $Y
    Left  PatternNode
    Right PatternNode
}
type IfStmt struct {            // if $COND: ...
    Cond PatternNode
    Body []PatternNode
}
type FuncDef struct {           // def $F(...): ... / func $F(...) { ... }
    Name   PatternNode
    Params []PatternNode
    Body   []PatternNode
}
type DeepExpr struct {          // <... P ...> -- match P anywhere nested
    Inner PatternNode
}
type Block struct {             // sequence of statements
    Stmts []PatternNode
}
```

### Parse Examples

```
"foo($X, ..., $X)"  ->  Call{Func: Literal("foo"), Args: [Metavar("X"), Ellipsis{}, Metavar("X")]}

"$X = $X"           ->  Assignment{Left: Metavar("X"), Right: Metavar("X")}

"$X.save()"         ->  MethodCall{Object: Metavar("X"), Method: Literal("save"), Args: []}

"if <... $X.is_admin() ...>: ..."  ->  IfStmt{
                          Cond: DeepExpr{Inner: MethodCall{...}},
                          Body: [Ellipsis{}]
                        }
```

### Boolean Combinators (from CLI flags)

```go
type MatchFormula interface{ matchFormula() }

type And struct {               // --lang + base pattern + --inside
    Patterns []MatchFormula
}
type Or struct {                // (future: pattern-either in YAML)
    Patterns []MatchFormula
}
type Not struct {               // --not
    Pattern MatchFormula
}
type Inside struct {            // --inside
    Pattern MatchFormula
}
type NotInside struct {         // --not-inside
    Pattern MatchFormula
}
type MetavarConstraint struct { // --where '$X ~ /regex/'
    Metavar string
    Op      string              // "~" (regex), "!~", "==", "!=", "<", ">"
    Value   string
}
type BasePattern struct {
    Lang    string
    Pattern PatternNode
}
```

## 3. Matching Engine

### Core Algorithm

```
match(patternNode, astNode, bindings) -> (matched bool, newBindings)
```

Rules:
1. **Literal** -- match if `astNode.Text() == literal.Value`
2. **Metavar** -- if `$X` already bound, match if `astNode.Text() == bindings["X"]`.
   Otherwise bind `$X -> astNode.Text()` and match.
3. **Wildcard** (`$_`) -- always matches a single node, no binding
4. **Ellipsis** (`...`) -- handled by parent compound node's sequence matching
5. **DeepExpr** -- recursively try `match(inner, descendant)` for every descendant

### Ellipsis Matching in Sequences

For matching `[Metavar("X"), Ellipsis{}, Metavar("X")]` against `[a, b, c, a]`:

```
matchSequence(patterns, astNodes, bindings):
  if patterns is empty: succeed if astNodes is empty
  if patterns[0] is Ellipsis:
    for skip in 0..len(astNodes):
      if matchSequence(patterns[1:], astNodes[skip:], bindings): return true
    return false
  else:
    if astNodes is empty: return false
    if match(patterns[0], astNodes[0], bindings):
      return matchSequence(patterns[1:], astNodes[1:], bindings)
    return false
```

### AST Node Type Mapping

The matcher uses a `LangMapper` interface to abstract grammar differences:

```go
type LangMapper interface {
    IsCall(node) bool
    CallFunc(node) *Node
    CallArgs(node) []*Node
    IsMethodCall(node) bool
    MethodObject(node) *Node
    MethodName(node) *Node
    MethodArgs(node) []*Node
    IsAssignment(node) bool
    AssignLeft(node) *Node
    AssignRight(node) *Node
    IsIfStmt(node) bool
    IfCond(node) *Node
    IfBody(node) []*Node
    IsFuncDef(node) bool
    FuncName(node) *Node
    FuncParams(node) []*Node
    FuncBody(node) []*Node
    IsBinaryOp(node) (bool, string)
}
```

Per-language node type mappings:

| IR concept | Go | Python | TypeScript |
|---|---|---|---|
| Call | `call_expression` | `call` | `call_expression` |
| MethodCall | `call_expression` + `selector_expression` | `call` + `attribute` | `call_expression` + `member_expression` |
| Assignment | `short_var_declaration`, `assignment_statement` | `assignment` | `assignment_expression` |
| IfStmt | `if_statement` | `if_statement` | `if_statement` |
| FuncDef | `function_declaration`, `method_declaration` | `function_definition` | `function_declaration` |
| BinaryOp | `binary_expression` | `comparison_operator`, `boolean_operator` | `binary_expression` |

### Execution Flow

```
User: codedb match 'foo($X, ..., $X)' --lang python --repo TraceForge

1. Parse CLI -> MatchFormula

2. Pre-filter (SQL):
   SELECT b.id, b.content_hash FROM blobs b
   JOIN file_revs fr ON fr.blob_id = b.id
   JOIN ... WHERE b.language = 'python'
   AND EXISTS (SELECT 1 FROM symbol_refs sr
               WHERE sr.blob_id = b.id AND sr.ref_name = 'foo')
   -> candidate blob IDs (only blobs that reference "foo")

3. For each candidate blob:
   a. Load source from git object store
   b. Parse with tree-sitter -> AST
   c. Walk every node in AST:
      if mapper.IsCall(node) && match(pattern, node, {}):
        record Match{file, line, col, bindings, matchedText}
   d. Apply --not, --inside, --where constraints

4. Format and display results
```

### Match Result

```go
type Match struct {
    File     string
    Line     int
    Col      int
    EndLine  int
    EndCol   int
    Text     string              // matched source text
    Bindings map[string]string   // metavar name -> matched text
}
```

### Output Format

```
traceforge/src/lib.rs:464:5: foo(config, validate(), transform(), config)
  $X = "config"
```

## 4. Package Structure

```
internal/codedb/match/
  pattern/
    ir.go              # PatternNode and MatchFormula types
    parser.go          # Parse pattern string -> PatternNode IR
    parser_test.go
  mapper/
    mapper.go          # LangMapper interface
    go.go              # Go AST node mapping
    python.go          # Python AST node mapping
    typescript.go      # TypeScript AST node mapping
    mapper_test.go
  engine/
    match.go           # Core matcher: match(pattern, ast, bindings)
    sequence.go        # Ellipsis-aware sequence matching
    formula.go         # Boolean combinator evaluation (And/Or/Not/Inside)
    constraint.go      # Metavariable constraint checking (regex, comparison)
    engine_test.go
  executor.go          # Orchestrator: pre-filter -> parse -> match -> format
  executor_test.go

cmd/codedb/main.go
  matchCmd              # codedb match '<pattern>' --lang <lang> [flags]
```

### CLI Flags

- `--lang` (required) -- target language
- `--repo` -- filter to specific repo
- `--file` -- file path glob filter
- `--not` -- exclude matches (repeatable)
- `--inside` -- require matches within scope (repeatable)
- `--not-inside` -- exclude matches within scope (repeatable)
- `--where` -- metavariable constraint (repeatable)
- `--json` -- JSON output
- `--count` -- max results

### Pre-filter SQL Hints

| Pattern contains | Pre-filter |
|---|---|
| `foo(...)` literal function name | `symbol_refs WHERE ref_name = 'foo'` |
| `def foo(...)` function definition | `symbols WHERE name = 'foo'` |
| `--repo X` | `repos WHERE name LIKE '%X%'` |
| `--file *.py` | `file_revs WHERE path GLOB '*.py'` |
| `--lang python` | `blobs WHERE language = 'python'` |
| No extractable hints | Full scan of all blobs for the language |

## 5. Pattern Parser

### Tokenizer

```
IDENT       foo, bar, None, null, true
METAVAR     $X, $FOO, $_, $...ARGS
NUMBER      42, 3.14, -1
STRING      "hello", 'world'
ELLIPSIS    ...
DEEP_OPEN   <...
DEEP_CLOSE  ...>
LPAREN (    RPAREN )
LBRACE {    RBRACE }
LBRACKET [  RBRACKET ]
DOT .       COMMA ,
COLON :     SEMICOLON ;
OP          ==, !=, <, >, <=, >=, +, -, *, /, &&, ||, and, or, not
ASSIGN      =, :=, +=
KEYWORD     if, def, func, class, for, while, return, import
NEWLINE     \n (significant in Python patterns)
```

### Grammar (PEG-style)

```
pattern     := stmt_list
stmt_list   := stmt (NEWLINE stmt)*
stmt        := if_stmt | func_def | assign_stmt | expr_stmt | ELLIPSIS
if_stmt     := 'if' expr ':' block
              | 'if' expr '{' block '}'
func_def    := 'def' name '(' params ')' ':' block
              | 'func' name '(' params ')' '{' block '}'
              | 'function' name '(' params ')' '{' block '}'
assign_stmt := expr ('=' | ':=') expr
expr        := unary (OP unary)*
unary       := atom trailer*
trailer     := '(' args ')'
              | '.' IDENT
              | '.' IDENT '(' args ')'
atom        := METAVAR | IDENT | NUMBER | STRING | ELLIPSIS
              | DEEP_OPEN expr DEEP_CLOSE
              | '(' expr ')'
args        := (arg (',' arg)*)?
arg         := expr | ELLIPSIS | METAVAR
params      := (param (',' param)*)?
param       := METAVAR | IDENT (':' type)? | ELLIPSIS
block       := stmt_list | ELLIPSIS
name        := IDENT | METAVAR
```

### Design Decisions

1. **Language-agnostic** -- accepts a superset of Python/Go/TS syntax. Doesn't
   validate the pattern is valid in the target language.
2. **Ambiguity resolution** -- `foo(x)` always produces `Call`. The matcher and
   `LangMapper` resolve what it maps to in the target AST.
3. **Minimal keywords** -- only `if`, `def`, `func`, `function`, `class`, `for`,
   `return` are recognized as keywords.
4. **Whitespace** -- newlines are tokens for Python block structure, other
   whitespace is ignored.

## 6. Testing Strategy

### Unit Tests

**Pattern parser** (`pattern/parser_test.go`):
- Every IR node type produced correctly
- Edge cases and malformed patterns
- Target: 95%+ coverage

**Matcher engine** (`engine/engine_test.go`):
- Literal, metavar binding, unification, ellipsis backtracking
- Deep expressions, wildcards
- Boolean combinators (And/Or/Not/Inside)
- Metavariable constraints (regex, comparison)
- Target: 95%+ coverage

**Lang mappers** (`mapper/mapper_test.go`):
- Every node type mapping for Go, Python, TypeScript
- Target: 90%+ coverage

### Integration Tests (`executor_test.go`)

End-to-end tests with real multi-line source:

```go
// Python: find os.system calls with non-literal args
pattern: "os.system($X)"  not: "os.system(\"ls\")"
// expect match at os.system(cmd), not at os.system("ls")

// Go: find self-assignment bugs
pattern: "$X = $X"
// expect match at "x = x"

// TypeScript: find eval inside functions
pattern: "eval($X)"  inside: "function $F(...) { ... }"
// expect match only when eval is inside a function
```

Target: 85%+ coverage for executor.

---

> Guided by SageOx
