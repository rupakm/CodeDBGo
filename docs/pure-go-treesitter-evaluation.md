# Pure Go Tree-Sitter Evaluation

## Problem Statement

CodeDBGo currently uses `github.com/smacker/go-tree-sitter` for AST-based symbol
extraction. This package wraps the tree-sitter C library via CGO, which means:

- A C compiler is required at build time
- Cross-compilation requires a C cross-toolchain
- The default build (`CGO_ENABLED=0`) ships with **no symbol extraction at all**
- Two separate build targets must be maintained (`build` vs `build-cgo`)

The goal is to evaluate whether a **pure Go tree-sitter implementation** can
replace the CGO dependency, giving us full symbol extraction in every build
without requiring a C toolchain.

## Current Architecture

```
symbols_cgo.go    (//go:build cgo)    — Full tree-sitter implementation
symbols_nocgo.go  (//go:build !cgo)   — No-op stub returning nil
```

Languages supported: Go, Rust, Python, JavaScript, TypeScript, TSX, JSX, C, C++.

The code uses tree-sitter's **query API** extensively — S-expression patterns with
`@name` / `@def` / `@ref` captures to extract symbol definitions and references.

## Candidate Packages

### 1. `github.com/odvcencio/gotreesitter` — **Recommended**

| Attribute | Details |
|---|---|
| CGO required | **No** — pure Go, zero C code |
| Grammar coverage | **206 languages** including all 9 we need |
| Query API | Full S-expression queries with captures |
| Maturity | v0.6.0, active development |
| Performance (full parse) | ~3.9x slower than native C |
| Performance (incremental) | ~42x *faster* than native C |
| License | MIT |

**Key advantages:**
- Ground-up Go reimplementation of the tree-sitter runtime
- Table-driven LR(1) with GLR fallback — same algorithm as C tree-sitter
- Grammars extracted from upstream `parser.c` via `ts2go` tool
- 203 of 206 grammars produce error-free parse trees
- Built-in `Tagger` API for symbol extraction (similar to our use case)
- Arena allocation reduces GC pressure
- No Wasm, no shared libraries, no build-time code generation

**Key differences from smacker/go-tree-sitter:**

| smacker API | gotreesitter API |
|---|---|
| `sitter.NewParser()` then `parser.SetLanguage(lang)` | `gotreesitter.NewParser(lang)` |
| `parser.ParseCtx(ctx, nil, src)` returns `*Tree` | `parser.Parse(src)` returns `(*Tree, error)` |
| `tree.RootNode()` returns `*Node` | `tree.RootNode()` returns `*Node` |
| `node.Content(src)` | `node.Text(src)` |
| `node.Type()` | `node.Type(lang)` — requires lang param |
| `node.Child(i)` | `node.Child(i)` |
| `node.ChildCount()` returns `uint32` | `node.ChildCount()` returns `int` |
| `node.IsNamed()` | `node.IsNamed()` |
| `sitter.NewQuery(pattern, lang)` | `gotreesitter.NewQuery(pattern, lang)` |
| `sitter.NewQueryCursor()` + `qc.Exec(q, node)` | `q.Exec(node, lang, src)` returns cursor |
| `qc.NextMatch()` returns `(*QueryMatch, bool)` | `cursor.NextMatch()` returns `(*QueryMatch, bool)` |
| `qc.FilterPredicates(m, src)` | Predicates handled internally |
| `q.CaptureCount()` + `q.CaptureNameForId(i)` | `q.CaptureNames()` returns `[]string` |
| `cap.Node` + `cap.Index` | `cap.Node` + `cap.Index` |
| Grammar: `golang.GetLanguage()` | Grammar: `grammars.GoLanguage()` |

### 2. `github.com/tree-sitter/go-tree-sitter` (Official)

**Still requires CGO.** The official Go bindings wrap the C library. Not a solution
for our problem.

### 3. `github.com/malivvan/tree-sitter` (Wasm + wazero)

Runs tree-sitter C code compiled to WebAssembly via wazero (pure Go Wasm runtime).
**Pre-release quality**, limited grammar availability, performance overhead from
Wasm interpretation. Not recommended.

### 4. `github.com/yourbase/treesitter`

Only supports JSON and Python. Effectively abandoned (2 stars, no releases).
Not viable.

### 5. Standard library `go/ast`

Pure Go, but only parses Go source code. Cannot replace tree-sitter for
multi-language support.

## Migration Plan

### Effort Estimate: **Low-Medium**

The migration is mechanical — the APIs are different but conceptually 1:1. All
tree-sitter query patterns (S-expressions) remain unchanged. The main changes:

1. **Replace imports** — swap `smacker/go-tree-sitter` → `gotreesitter` + `gotreesitter/grammars`
2. **Adapt parser creation** — `NewParser(lang)` instead of `NewParser()` + `SetLanguage()`
3. **Adapt node access** — `node.Type(lang)` instead of `node.Type()`, `node.Text(src)` instead of `node.Content(src)`
4. **Adapt query execution** — `q.Exec(node, lang, src)` instead of separate cursor creation
5. **Remove build tags** — eliminate `symbols_cgo.go` / `symbols_nocgo.go` split; single file works everywhere
6. **Update go.mod** — replace dependency
7. **Remove Makefile CGO targets** — `build-cgo` and `test-cgo` become unnecessary

### What stays the same:
- All S-expression query patterns (`defQuery`, `refQuery`)
- Symbol/Ref data structures
- Parent-child nesting detection logic
- Type info extraction logic (signature, params, return type)
- Node traversal patterns (Child, ChildCount, StartPoint, EndPoint, StartByte, EndByte)

### Proof of Concept

A proof-of-concept file `symbols_purego.go` is included in this branch showing
the adapted `Extract()` function and language configs. See
`internal/codedb/symbols/symbols_purego.go`.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Parsing differences from C tree-sitter | Low | Same grammar tables extracted from upstream; 203/206 error-free |
| Performance regression (full parse ~3.9x slower) | Low | Symbol extraction is not hot path; runs once per file at index time |
| Library stability (v0.6.0) | Medium | Pin version; active maintainer; fallback to CGO build if needed |
| Query pattern compatibility | Low | Same S-expression syntax; can validate with existing test suite |
| Missing `FilterPredicates` | Low | gotreesitter handles predicates internally during matching |

## Recommendation

**Proceed with migration to `github.com/odvcencio/gotreesitter`.**

Benefits:
1. **Single build target** — eliminate CGO split, `symbols_nocgo.go` no-op stub, and dual Makefile targets
2. **Cross-compilation** — `GOOS=windows GOARCH=arm64 go build` just works
3. **Simpler CI** — no C toolchain needed in CI/CD pipeline
4. **Every user gets symbol extraction** — no more degraded builds
5. **Incremental parsing potential** — 42x faster than C for re-parses, useful for watch mode

Trade-offs:
1. ~3.9x slower full parse (negligible for index-time batch processing)
2. Newer library with smaller community (but actively maintained, MIT licensed)
3. One-time migration effort (estimated 1-2 hours for the symbols package)
