# Structural Pattern Matching Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `codedb match` subcommand for Semgrep-style structural code pattern matching.

**Architecture:** Custom Pattern IR parsed from user patterns, matched against tree-sitter ASTs via language-specific mappers. Hybrid execution: SQL pre-filters narrow candidate blobs, then live AST matching on survivors. See `docs/plans/2026-03-09-semgrep-pattern-matching-design.md` for full design.

**Tech Stack:** Go, tree-sitter via `github.com/odvcencio/gotreesitter`, SQLite, Cobra CLI.

---

## Task 1: Pattern IR Types

Define all IR node types and match formula types. Pure data structures, no logic.

**Files:**
- Create: `internal/codedb/match/pattern/ir.go`
- Test: `internal/codedb/match/pattern/ir_test.go`

**Step 1: Write the test**

```go
// internal/codedb/match/pattern/ir_test.go
package pattern

import "testing"

func TestPatternNodeInterface(t *testing.T) {
	// Verify all types satisfy PatternNode interface
	var nodes []PatternNode
	nodes = append(nodes,
		&Literal{Value: "foo"},
		&Metavar{Name: "X"},
		&Ellipsis{},
		&EllipsisMetavar{Name: "ARGS"},
		&Wildcard{},
		&Call{Func: &Literal{Value: "foo"}, Args: []PatternNode{&Ellipsis{}}},
		&MethodCall{Object: &Metavar{Name: "X"}, Method: &Literal{Value: "save"}, Args: nil},
		&BinaryOp{Left: &Metavar{Name: "X"}, Op: "==", Right: &Metavar{Name: "Y"}},
		&Assignment{Left: &Metavar{Name: "X"}, Right: &Metavar{Name: "Y"}},
		&IfStmt{Cond: &Metavar{Name: "C"}, Body: []PatternNode{&Ellipsis{}}},
		&FuncDef{Name: &Metavar{Name: "F"}, Params: nil, Body: []PatternNode{&Ellipsis{}}},
		&DeepExpr{Inner: &Literal{Value: "x"}},
		&Block{Stmts: []PatternNode{&Ellipsis{}}},
	)
	if len(nodes) != 13 {
		t.Errorf("expected 13 node types, got %d", len(nodes))
	}
}

func TestMatchFormulaInterface(t *testing.T) {
	var formulas []MatchFormula
	formulas = append(formulas,
		&And{Formulas: []MatchFormula{}},
		&Or{Formulas: []MatchFormula{}},
		&Not{Formula: &BasePattern{}},
		&Inside{Formula: &BasePattern{}},
		&NotInside{Formula: &BasePattern{}},
		&MetavarConstraint{Metavar: "X", Op: "~", Value: "/foo/"},
		&BasePattern{Lang: "go", Pattern: &Literal{Value: "x"}},
	)
	if len(formulas) != 7 {
		t.Errorf("expected 7 formula types, got %d", len(formulas))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/codedb/match/pattern/ -v -run TestPatternNode`
Expected: FAIL — package does not exist

**Step 3: Write the IR types**

```go
// internal/codedb/match/pattern/ir.go
package pattern

// PatternNode is the interface for all pattern IR nodes.
type PatternNode interface {
	patternNode()
}

// --- Atom nodes ---

// Literal matches exact text: "foo", "None", "42".
type Literal struct{ Value string }

// Metavar matches any expression and binds to Name. Same name must unify.
type Metavar struct{ Name string }

// Ellipsis matches zero or more items in a sequence.
type Ellipsis struct{}

// EllipsisMetavar matches zero or more items and binds them.
type EllipsisMetavar struct{ Name string }

// Wildcard matches any single node without binding.
type Wildcard struct{}

// --- Compound nodes ---

// Call matches function calls: foo(...) or $F(...).
type Call struct {
	Func PatternNode
	Args []PatternNode
}

// MethodCall matches method calls: $X.foo(...) or $X.$M(...).
type MethodCall struct {
	Object PatternNode
	Method PatternNode
	Args   []PatternNode
}

// BinaryOp matches binary operations: $X == $Y, $X + $Y.
type BinaryOp struct {
	Left  PatternNode
	Op    string
	Right PatternNode
}

// Assignment matches assignments: $X = $Y, $X := $Y.
type Assignment struct {
	Left  PatternNode
	Right PatternNode
}

// IfStmt matches conditionals: if $COND: ... or if $COND { ... }.
type IfStmt struct {
	Cond PatternNode
	Body []PatternNode
}

// FuncDef matches function definitions.
type FuncDef struct {
	Name   PatternNode
	Params []PatternNode
	Body   []PatternNode
}

// DeepExpr matches a pattern nested arbitrarily deep: <... P ...>.
type DeepExpr struct {
	Inner PatternNode
}

// Block matches a sequence of statements.
type Block struct {
	Stmts []PatternNode
}

func (*Literal) patternNode()       {}
func (*Metavar) patternNode()       {}
func (*Ellipsis) patternNode()      {}
func (*EllipsisMetavar) patternNode() {}
func (*Wildcard) patternNode()      {}
func (*Call) patternNode()          {}
func (*MethodCall) patternNode()    {}
func (*BinaryOp) patternNode()     {}
func (*Assignment) patternNode()   {}
func (*IfStmt) patternNode()       {}
func (*FuncDef) patternNode()      {}
func (*DeepExpr) patternNode()     {}
func (*Block) patternNode()        {}

// MatchFormula is the interface for boolean combinators over patterns.
type MatchFormula interface {
	matchFormula()
}

// And requires all sub-formulas to match.
type And struct{ Formulas []MatchFormula }

// Or requires at least one sub-formula to match.
type Or struct{ Formulas []MatchFormula }

// Not excludes matches of the sub-formula.
type Not struct{ Formula MatchFormula }

// Inside requires matches to occur within the scope of the sub-formula.
type Inside struct{ Formula MatchFormula }

// NotInside excludes matches within the scope of the sub-formula.
type NotInside struct{ Formula MatchFormula }

// MetavarConstraint applies a constraint to a bound metavariable.
// Op is one of: "~" (regex), "!~", "==", "!=", "<", ">", "<=", ">=".
type MetavarConstraint struct {
	Metavar string
	Op      string
	Value   string
}

// BasePattern is a leaf formula: a pattern in a specific language.
type BasePattern struct {
	Lang    string
	Pattern PatternNode
}

func (*And) matchFormula()              {}
func (*Or) matchFormula()               {}
func (*Not) matchFormula()              {}
func (*Inside) matchFormula()           {}
func (*NotInside) matchFormula()        {}
func (*MetavarConstraint) matchFormula() {}
func (*BasePattern) matchFormula()      {}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/codedb/match/pattern/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/codedb/match/pattern/ir.go internal/codedb/match/pattern/ir_test.go
git commit -m "feat(match): add pattern IR types and match formula types"
```

---

## Task 2: Pattern Tokenizer

Scan pattern strings into tokens. This is the foundation for the parser.

**Files:**
- Create: `internal/codedb/match/pattern/token.go`
- Create: `internal/codedb/match/pattern/lexer.go`
- Test: `internal/codedb/match/pattern/lexer_test.go`

**Step 1: Write the test**

```go
// internal/codedb/match/pattern/lexer_test.go
package pattern

import "testing"

func TestLexSimpleCall(t *testing.T) {
	tokens := Lex("foo($X, ..., $X)")
	want := []TokenKind{IDENT, LPAREN, METAVAR, COMMA, ELLIPSIS, COMMA, METAVAR, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	for i, tok := range tokens {
		if tok.Kind != want[i] {
			t.Errorf("token[%d] = %v, want %v", i, tok.Kind, want[i])
		}
	}
	if tokens[2].Value != "X" {
		t.Errorf("metavar name = %q, want %q", tokens[2].Value, "X")
	}
}

func TestLexMethodCall(t *testing.T) {
	tokens := Lex("$X.save()")
	want := []TokenKind{METAVAR, DOT, IDENT, LPAREN, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexAssignment(t *testing.T) {
	tokens := Lex("$X = $Y")
	want := []TokenKind{METAVAR, ASSIGN, METAVAR, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexBinaryOp(t *testing.T) {
	tokens := Lex("$X == None")
	want := []TokenKind{METAVAR, OP, IDENT, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[1].Value != "==" {
		t.Errorf("op = %q, want %q", tokens[1].Value, "==")
	}
}

func TestLexDeepExpr(t *testing.T) {
	tokens := Lex("<... $X ...>")
	want := []TokenKind{DEEP_OPEN, METAVAR, DEEP_CLOSE, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexKeyword(t *testing.T) {
	tokens := Lex("if $X: ...")
	want := []TokenKind{KEYWORD, METAVAR, COLON, ELLIPSIS, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[0].Value != "if" {
		t.Errorf("keyword = %q, want %q", tokens[0].Value, "if")
	}
}

func TestLexFuncDef(t *testing.T) {
	tokens := Lex("def $F(...): ...")
	want := []TokenKind{KEYWORD, METAVAR, LPAREN, ELLIPSIS, RPAREN, COLON, ELLIPSIS, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexString(t *testing.T) {
	tokens := Lex(`foo("hello")`)
	want := []TokenKind{IDENT, LPAREN, STRING, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[2].Value != "hello" {
		t.Errorf("string = %q, want %q", tokens[2].Value, "hello")
	}
}

func TestLexWildcard(t *testing.T) {
	tokens := Lex("foo($_, $_)")
	want := []TokenKind{IDENT, LPAREN, METAVAR, COMMA, METAVAR, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[2].Value != "_" {
		t.Errorf("wildcard metavar = %q, want %q", tokens[2].Value, "_")
	}
}

func TestLexEllipsisMetavar(t *testing.T) {
	tokens := Lex("foo($...ARGS)")
	want := []TokenKind{IDENT, LPAREN, METAVAR, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	// $...ARGS is a METAVAR with Value "...ARGS"
	if tokens[2].Value != "...ARGS" {
		t.Errorf("ellipsis metavar = %q, want %q", tokens[2].Value, "...ARGS")
	}
}

func TestLexGoShortDecl(t *testing.T) {
	tokens := Lex("$X := $Y")
	want := []TokenKind{METAVAR, ASSIGN, METAVAR, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[1].Value != ":=" {
		t.Errorf("assign = %q, want %q", tokens[1].Value, ":=")
	}
}

func TestLexNumber(t *testing.T) {
	tokens := Lex("foo(42)")
	want := []TokenKind{IDENT, LPAREN, NUMBER, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[2].Value != "42" {
		t.Errorf("number = %q, want %q", tokens[2].Value, "42")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/codedb/match/pattern/ -v -run TestLex`
Expected: FAIL — Lex, TokenKind undefined

**Step 3: Write token types**

```go
// internal/codedb/match/pattern/token.go
package pattern

import "fmt"

// TokenKind identifies the type of a lexer token.
type TokenKind int

const (
	EOF TokenKind = iota
	IDENT        // foo, bar, None, null
	METAVAR      // $X, $_, $...ARGS
	NUMBER       // 42, 3.14
	STRING       // "hello", 'world'
	ELLIPSIS     // ...
	DEEP_OPEN    // <...
	DEEP_CLOSE   // ...>
	LPAREN       // (
	RPAREN       // )
	LBRACE       // {
	RBRACE       // }
	LBRACKET     // [
	RBRACKET     // ]
	DOT          // .
	COMMA        // ,
	COLON        // :
	SEMICOLON    // ;
	OP           // ==, !=, <, >, <=, >=, +, -, *, /, &&, ||
	ASSIGN       // =, :=, +=
	KEYWORD      // if, def, func, function, class, for, while, return, import
	NEWLINE      // \n
)

// Token is a single lexer token.
type Token struct {
	Kind  TokenKind
	Value string
	Pos   int // byte offset in input
}

func (t Token) String() string {
	return fmt.Sprintf("%v(%q)", t.Kind, t.Value)
}

var keywords = map[string]bool{
	"if": true, "def": true, "func": true, "function": true,
	"class": true, "for": true, "while": true, "return": true, "import": true,
}
```

**Step 4: Write the lexer**

```go
// internal/codedb/match/pattern/lexer.go
package pattern

import (
	"unicode"
)

// Lex tokenizes a pattern string into a slice of Tokens.
func Lex(input string) []Token {
	l := &lexer{input: []rune(input)}
	var tokens []Token
	for {
		tok := l.next()
		tokens = append(tokens, tok)
		if tok.Kind == EOF {
			break
		}
	}
	return tokens
}

type lexer struct {
	input []rune
	pos   int
}

func (l *lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *lexer) advance() rune {
	r := l.input[l.pos]
	l.pos++
	return r
}

func (l *lexer) match(s string) bool {
	rs := []rune(s)
	if l.pos+len(rs) > len(l.input) {
		return false
	}
	for i, r := range rs {
		if l.input[l.pos+i] != r {
			return false
		}
	}
	return true
}

func (l *lexer) next() Token {
	// Skip whitespace (except newlines)
	for l.pos < len(l.input) && l.input[l.pos] != '\n' && unicode.IsSpace(l.input[l.pos]) {
		l.pos++
	}

	if l.pos >= len(l.input) {
		return Token{Kind: EOF, Pos: l.pos}
	}

	start := l.pos
	ch := l.peek()

	// Newline
	if ch == '\n' {
		l.advance()
		return Token{Kind: NEWLINE, Value: "\n", Pos: start}
	}

	// Deep expression open: <...
	if ch == '<' && l.match("<...") {
		l.pos += 4
		return Token{Kind: DEEP_OPEN, Value: "<...", Pos: start}
	}

	// Deep expression close: ...>
	if ch == '.' && l.match("...>") {
		l.pos += 4
		return Token{Kind: DEEP_CLOSE, Value: "...>", Pos: start}
	}

	// Ellipsis: ...
	if ch == '.' && l.match("...") {
		l.pos += 3
		return Token{Kind: ELLIPSIS, Value: "...", Pos: start}
	}

	// Metavar: $X, $_, $...ARGS
	if ch == '$' {
		l.advance()
		if l.match("...") {
			l.pos += 3
			name := "..."
			for l.pos < len(l.input) && (unicode.IsUpper(l.input[l.pos]) || l.input[l.pos] == '_' || unicode.IsDigit(l.input[l.pos])) {
				name += string(l.advance())
			}
			return Token{Kind: METAVAR, Value: name, Pos: start}
		}
		if l.pos < len(l.input) && l.input[l.pos] == '_' && (l.pos+1 >= len(l.input) || !isIdentChar(l.input[l.pos+1])) {
			l.advance()
			return Token{Kind: METAVAR, Value: "_", Pos: start}
		}
		name := ""
		for l.pos < len(l.input) && (unicode.IsUpper(l.input[l.pos]) || l.input[l.pos] == '_' || unicode.IsDigit(l.input[l.pos])) {
			name += string(l.advance())
		}
		if name == "" {
			return Token{Kind: IDENT, Value: "$", Pos: start}
		}
		return Token{Kind: METAVAR, Value: name, Pos: start}
	}

	// String literals
	if ch == '"' || ch == '\'' {
		return l.lexString(ch, start)
	}

	// Numbers
	if unicode.IsDigit(ch) || (ch == '-' && l.pos+1 < len(l.input) && unicode.IsDigit(l.input[l.pos+1])) {
		return l.lexNumber(start)
	}

	// Multi-character operators
	if ch == ':' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: ASSIGN, Value: ":=", Pos: start}
	}
	if ch == '+' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: ASSIGN, Value: "+=", Pos: start}
	}
	if ch == '=' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: "==", Pos: start}
	}
	if ch == '!' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: "!=", Pos: start}
	}
	if ch == '<' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: "<=", Pos: start}
	}
	if ch == '>' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: ">=", Pos: start}
	}
	if ch == '&' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '&' {
		l.pos += 2
		return Token{Kind: OP, Value: "&&", Pos: start}
	}
	if ch == '|' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '|' {
		l.pos += 2
		return Token{Kind: OP, Value: "||", Pos: start}
	}

	// Single-character tokens
	l.advance()
	switch ch {
	case '(':
		return Token{Kind: LPAREN, Value: "(", Pos: start}
	case ')':
		return Token{Kind: RPAREN, Value: ")", Pos: start}
	case '{':
		return Token{Kind: LBRACE, Value: "{", Pos: start}
	case '}':
		return Token{Kind: RBRACE, Value: "}", Pos: start}
	case '[':
		return Token{Kind: LBRACKET, Value: "[", Pos: start}
	case ']':
		return Token{Kind: RBRACKET, Value: "]", Pos: start}
	case '.':
		return Token{Kind: DOT, Value: ".", Pos: start}
	case ',':
		return Token{Kind: COMMA, Value: ",", Pos: start}
	case ':':
		return Token{Kind: COLON, Value: ":", Pos: start}
	case ';':
		return Token{Kind: SEMICOLON, Value: ";", Pos: start}
	case '=':
		return Token{Kind: ASSIGN, Value: "=", Pos: start}
	case '<':
		return Token{Kind: OP, Value: "<", Pos: start}
	case '>':
		return Token{Kind: OP, Value: ">", Pos: start}
	case '+', '-', '*', '/':
		return Token{Kind: OP, Value: string(ch), Pos: start}
	}

	// Identifiers and keywords
	if isIdentStart(ch) {
		l.pos = start // back up to include first char
		return l.lexIdent(start)
	}

	// Unknown character — treat as ident
	return Token{Kind: IDENT, Value: string(ch), Pos: start}
}

func (l *lexer) lexString(quote rune, start int) Token {
	l.advance() // consume opening quote
	var val []rune
	for l.pos < len(l.input) && l.input[l.pos] != quote {
		if l.input[l.pos] == '\\' && l.pos+1 < len(l.input) {
			l.advance() // skip backslash
		}
		val = append(val, l.advance())
	}
	if l.pos < len(l.input) {
		l.advance() // consume closing quote
	}
	return Token{Kind: STRING, Value: string(val), Pos: start}
}

func (l *lexer) lexNumber(start int) Token {
	var val []rune
	if l.input[l.pos] == '-' {
		val = append(val, l.advance())
	}
	for l.pos < len(l.input) && (unicode.IsDigit(l.input[l.pos]) || l.input[l.pos] == '.') {
		val = append(val, l.advance())
	}
	return Token{Kind: NUMBER, Value: string(val), Pos: start}
}

func (l *lexer) lexIdent(start int) Token {
	var val []rune
	for l.pos < len(l.input) && isIdentChar(l.input[l.pos]) {
		val = append(val, l.advance())
	}
	name := string(val)
	if keywords[name] {
		return Token{Kind: KEYWORD, Value: name, Pos: start}
	}
	return Token{Kind: IDENT, Value: name, Pos: start}
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/codedb/match/pattern/ -v -run TestLex`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/codedb/match/pattern/token.go internal/codedb/match/pattern/lexer.go internal/codedb/match/pattern/lexer_test.go
git commit -m "feat(match): add pattern tokenizer/lexer"
```

---

## Task 3: Pattern Parser

Parse token streams into PatternNode IR trees.

**Files:**
- Create: `internal/codedb/match/pattern/parser.go`
- Create: `internal/codedb/match/pattern/parser_test.go`

**Step 1: Write the test**

```go
// internal/codedb/match/pattern/parser_test.go
package pattern

import (
	"testing"
)

func TestParseSimpleCall(t *testing.T) {
	node, err := Parse("foo($X)")
	if err != nil {
		t.Fatal(err)
	}
	call, ok := node.(*Call)
	if !ok {
		t.Fatalf("expected Call, got %T", node)
	}
	lit, ok := call.Func.(*Literal)
	if !ok || lit.Value != "foo" {
		t.Errorf("func = %v, want Literal(foo)", call.Func)
	}
	if len(call.Args) != 1 {
		t.Fatalf("args = %d, want 1", len(call.Args))
	}
	mv, ok := call.Args[0].(*Metavar)
	if !ok || mv.Name != "X" {
		t.Errorf("arg[0] = %v, want Metavar(X)", call.Args[0])
	}
}

func TestParseCallWithEllipsis(t *testing.T) {
	node, err := Parse("foo($X, ..., $X)")
	if err != nil {
		t.Fatal(err)
	}
	call := node.(*Call)
	if len(call.Args) != 3 {
		t.Fatalf("args = %d, want 3", len(call.Args))
	}
	if _, ok := call.Args[0].(*Metavar); !ok {
		t.Errorf("arg[0] = %T, want Metavar", call.Args[0])
	}
	if _, ok := call.Args[1].(*Ellipsis); !ok {
		t.Errorf("arg[1] = %T, want Ellipsis", call.Args[1])
	}
	if _, ok := call.Args[2].(*Metavar); !ok {
		t.Errorf("arg[2] = %T, want Metavar", call.Args[2])
	}
}

func TestParseMethodCall(t *testing.T) {
	node, err := Parse("$X.save()")
	if err != nil {
		t.Fatal(err)
	}
	mc, ok := node.(*MethodCall)
	if !ok {
		t.Fatalf("expected MethodCall, got %T", node)
	}
	if mv, ok := mc.Object.(*Metavar); !ok || mv.Name != "X" {
		t.Errorf("object = %v, want Metavar(X)", mc.Object)
	}
	if lit, ok := mc.Method.(*Literal); !ok || lit.Value != "save" {
		t.Errorf("method = %v, want Literal(save)", mc.Method)
	}
}

func TestParseAssignment(t *testing.T) {
	node, err := Parse("$X = $Y")
	if err != nil {
		t.Fatal(err)
	}
	assign, ok := node.(*Assignment)
	if !ok {
		t.Fatalf("expected Assignment, got %T", node)
	}
	if mv, ok := assign.Left.(*Metavar); !ok || mv.Name != "X" {
		t.Errorf("left = %v, want Metavar(X)", assign.Left)
	}
}

func TestParseBinaryOp(t *testing.T) {
	node, err := Parse("$X == None")
	if err != nil {
		t.Fatal(err)
	}
	binop, ok := node.(*BinaryOp)
	if !ok {
		t.Fatalf("expected BinaryOp, got %T", node)
	}
	if binop.Op != "==" {
		t.Errorf("op = %q, want %q", binop.Op, "==")
	}
}

func TestParseDeepExpr(t *testing.T) {
	node, err := Parse("<... $X.is_admin() ...>")
	if err != nil {
		t.Fatal(err)
	}
	deep, ok := node.(*DeepExpr)
	if !ok {
		t.Fatalf("expected DeepExpr, got %T", node)
	}
	if _, ok := deep.Inner.(*MethodCall); !ok {
		t.Errorf("inner = %T, want MethodCall", deep.Inner)
	}
}

func TestParseIfStmt(t *testing.T) {
	node, err := Parse("if $COND: ...")
	if err != nil {
		t.Fatal(err)
	}
	ifst, ok := node.(*IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", node)
	}
	if _, ok := ifst.Cond.(*Metavar); !ok {
		t.Errorf("cond = %T, want Metavar", ifst.Cond)
	}
	if len(ifst.Body) != 1 {
		t.Fatalf("body = %d stmts, want 1", len(ifst.Body))
	}
	if _, ok := ifst.Body[0].(*Ellipsis); !ok {
		t.Errorf("body[0] = %T, want Ellipsis", ifst.Body[0])
	}
}

func TestParseFuncDef(t *testing.T) {
	node, err := Parse("def $F(...): ...")
	if err != nil {
		t.Fatal(err)
	}
	fndef, ok := node.(*FuncDef)
	if !ok {
		t.Fatalf("expected FuncDef, got %T", node)
	}
	if mv, ok := fndef.Name.(*Metavar); !ok || mv.Name != "F" {
		t.Errorf("name = %v, want Metavar(F)", fndef.Name)
	}
}

func TestParseGoFuncDef(t *testing.T) {
	node, err := Parse("func $F(...) { ... }")
	if err != nil {
		t.Fatal(err)
	}
	fndef, ok := node.(*FuncDef)
	if !ok {
		t.Fatalf("expected FuncDef, got %T", node)
	}
	if mv, ok := fndef.Name.(*Metavar); !ok || mv.Name != "F" {
		t.Errorf("name = %v, want Metavar(F)", fndef.Name)
	}
}

func TestParseWildcard(t *testing.T) {
	node, err := Parse("foo($_, $_)")
	if err != nil {
		t.Fatal(err)
	}
	call := node.(*Call)
	for i, arg := range call.Args {
		if _, ok := arg.(*Wildcard); !ok {
			t.Errorf("arg[%d] = %T, want Wildcard", i, arg)
		}
	}
}

func TestParseStringArg(t *testing.T) {
	node, err := Parse(`foo("hello")`)
	if err != nil {
		t.Fatal(err)
	}
	call := node.(*Call)
	if len(call.Args) != 1 {
		t.Fatalf("args = %d, want 1", len(call.Args))
	}
	lit, ok := call.Args[0].(*Literal)
	if !ok || lit.Value != `"hello"` {
		t.Errorf("arg[0] = %v, want Literal(\"hello\")", call.Args[0])
	}
}

func TestParseEllipsisMetavar(t *testing.T) {
	node, err := Parse("foo($...ARGS, 3, $...REST)")
	if err != nil {
		t.Fatal(err)
	}
	call := node.(*Call)
	if len(call.Args) != 3 {
		t.Fatalf("args = %d, want 3", len(call.Args))
	}
	em, ok := call.Args[0].(*EllipsisMetavar)
	if !ok || em.Name != "ARGS" {
		t.Errorf("arg[0] = %v, want EllipsisMetavar(ARGS)", call.Args[0])
	}
}

func TestParseNestedCall(t *testing.T) {
	node, err := Parse("outer(inner($X))")
	if err != nil {
		t.Fatal(err)
	}
	outer := node.(*Call)
	if len(outer.Args) != 1 {
		t.Fatalf("outer args = %d, want 1", len(outer.Args))
	}
	inner, ok := outer.Args[0].(*Call)
	if !ok {
		t.Fatalf("arg[0] = %T, want Call", outer.Args[0])
	}
	if lit, ok := inner.Func.(*Literal); !ok || lit.Value != "inner" {
		t.Errorf("inner func = %v, want Literal(inner)", inner.Func)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/codedb/match/pattern/ -v -run TestParse`
Expected: FAIL — Parse undefined

**Step 3: Write the parser**

The parser is a recursive descent parser over the token stream. This is the
largest single piece of code in the project. Key entry point: `Parse(input string) (PatternNode, error)`.

The parser should handle:
- `expr` as the top-level rule
- Calls: `atom(args)` → Call
- Method calls: `atom.ident(args)` → MethodCall
- Assignments: `expr = expr` → Assignment (when `=` not followed by `=`)
- Binary ops: `expr OP expr` → BinaryOp
- If statements: `if expr : block` or `if expr { block }`
- Func defs: `def name(params): block` / `func name(params) { block }` / `function name(params) { block }`
- Deep expr: `<... expr ...>` → DeepExpr
- Atoms: METAVAR → Metavar/Wildcard/EllipsisMetavar, IDENT → Literal, NUMBER → Literal, STRING → Literal (quoted), ELLIPSIS → Ellipsis
- `$_` → Wildcard
- `$...NAME` → EllipsisMetavar

Implementation: create `internal/codedb/match/pattern/parser.go` with a `parser` struct
holding tokens and position, with methods for each grammar rule.

This file will be ~200-300 lines. Write it following the PEG grammar from the
design doc section 5.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/codedb/match/pattern/ -v -run TestParse`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/codedb/match/pattern/parser.go internal/codedb/match/pattern/parser_test.go
git commit -m "feat(match): add pattern parser (recursive descent)"
```

---

## Task 4: LangMapper Interface and Go Mapper

Define the LangMapper interface and implement it for Go.

**Files:**
- Create: `internal/codedb/match/mapper/mapper.go`
- Create: `internal/codedb/match/mapper/go.go`
- Test: `internal/codedb/match/mapper/mapper_test.go`

**Step 1: Write the test**

```go
// internal/codedb/match/mapper/mapper_test.go
package mapper

import (
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func parseGo(t *testing.T, src string) (*gotreesitter.Tree, *gotreesitter.Language) {
	t.Helper()
	lang := grammars.GoLanguage()
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.ParseWithTokenSource([]byte(src), grammars.NewGoTokenSourceOrEOF([]byte(src), lang))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree")
	}
	t.Cleanup(func() { tree.Release() })
	return tree, lang
}

func findFirst(node *gotreesitter.Node, lang *gotreesitter.Language, kind string) *gotreesitter.Node {
	if node.Type(lang) == kind {
		return node
	}
	for i := 0; i < node.ChildCount(); i++ {
		if found := findFirst(node.Child(i), lang, kind); found != nil {
			return found
		}
	}
	return nil
}

func TestGoMapperIsCall(t *testing.T) {
	tree, lang := parseGo(t, `package main
func main() { foo(1, 2) }`)
	m := NewGoMapper(lang)
	root := tree.RootNode()
	call := findFirst(root, lang, "call_expression")
	if call == nil {
		t.Fatal("no call_expression found")
	}
	if !m.IsCall(call) {
		t.Error("IsCall returned false")
	}
	fn := m.CallFunc(call)
	if fn == nil {
		t.Fatal("CallFunc returned nil")
	}
	if fn.Text([]byte(`package main
func main() { foo(1, 2) }`)) != "foo" {
		t.Errorf("func name = %q, want foo", fn.Text([]byte(`package main
func main() { foo(1, 2) }`)))
	}
	args := m.CallArgs(call)
	if len(args) != 2 {
		t.Errorf("args = %d, want 2", len(args))
	}
}

func TestGoMapperIsAssignment(t *testing.T) {
	tree, lang := parseGo(t, `package main
func main() { x := 1 }`)
	m := NewGoMapper(lang)
	root := tree.RootNode()
	decl := findFirst(root, lang, "short_var_declaration")
	if decl == nil {
		t.Fatal("no short_var_declaration found")
	}
	if !m.IsAssignment(decl) {
		t.Error("IsAssignment returned false for short_var_declaration")
	}
}

func TestGoMapperIsFuncDef(t *testing.T) {
	tree, lang := parseGo(t, `package main
func hello(x int) string { return "" }`)
	m := NewGoMapper(lang)
	root := tree.RootNode()
	fn := findFirst(root, lang, "function_declaration")
	if fn == nil {
		t.Fatal("no function_declaration found")
	}
	if !m.IsFuncDef(fn) {
		t.Error("IsFuncDef returned false")
	}
	name := m.FuncName(fn)
	if name == nil {
		t.Fatal("FuncName returned nil")
	}
}

func TestGoMapperIsBinaryOp(t *testing.T) {
	tree, lang := parseGo(t, `package main
func main() { _ = x == y }`)
	m := NewGoMapper(lang)
	root := tree.RootNode()
	binop := findFirst(root, lang, "binary_expression")
	if binop == nil {
		t.Fatal("no binary_expression found")
	}
	isBin, op := m.IsBinaryOp(binop)
	if !isBin {
		t.Error("IsBinaryOp returned false")
	}
	if op != "==" {
		t.Errorf("op = %q, want ==", op)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/codedb/match/mapper/ -v -run TestGoMapper`
Expected: FAIL — package/types don't exist

**Step 3: Write the mapper interface**

```go
// internal/codedb/match/mapper/mapper.go
package mapper

import "github.com/odvcencio/gotreesitter"

// LangMapper maps tree-sitter AST nodes to pattern IR concepts.
type LangMapper interface {
	IsCall(node *gotreesitter.Node) bool
	CallFunc(node *gotreesitter.Node) *gotreesitter.Node
	CallArgs(node *gotreesitter.Node) []*gotreesitter.Node

	IsMethodCall(node *gotreesitter.Node) bool
	MethodObject(node *gotreesitter.Node) *gotreesitter.Node
	MethodName(node *gotreesitter.Node) *gotreesitter.Node
	MethodArgs(node *gotreesitter.Node) []*gotreesitter.Node

	IsAssignment(node *gotreesitter.Node) bool
	AssignLeft(node *gotreesitter.Node) *gotreesitter.Node
	AssignRight(node *gotreesitter.Node) *gotreesitter.Node

	IsIfStmt(node *gotreesitter.Node) bool
	IfCond(node *gotreesitter.Node) *gotreesitter.Node
	IfBody(node *gotreesitter.Node) []*gotreesitter.Node

	IsFuncDef(node *gotreesitter.Node) bool
	FuncName(node *gotreesitter.Node) *gotreesitter.Node
	FuncParams(node *gotreesitter.Node) []*gotreesitter.Node
	FuncBody(node *gotreesitter.Node) []*gotreesitter.Node

	IsBinaryOp(node *gotreesitter.Node) (bool, string)
	BinaryLeft(node *gotreesitter.Node) *gotreesitter.Node
	BinaryRight(node *gotreesitter.Node) *gotreesitter.Node

	// NodeText returns the source text for a node.
	NodeText(node *gotreesitter.Node) string

	// Lang returns the tree-sitter language.
	Lang() *gotreesitter.Language
}
```

**Step 4: Write the Go mapper**

`internal/codedb/match/mapper/go.go` — implement `GoMapper` struct with all
methods. Key Go tree-sitter node types:
- Call: `call_expression` with child `function` (identifier or selector_expression)
  and `arguments` (argument_list with named children)
- MethodCall: `call_expression` where function child is `selector_expression`
- Assignment: `short_var_declaration` or `assignment_statement`
- If: `if_statement` with `condition` and `consequence` (block)
- FuncDef: `function_declaration` with `name` (identifier), `parameters`, `body`
- BinaryOp: `binary_expression` with unnamed operator child between left and right

The mapper stores `lang *gotreesitter.Language` and `src []byte` to resolve
node types and text.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/codedb/match/mapper/ -v -run TestGoMapper`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/codedb/match/mapper/
git commit -m "feat(match): add LangMapper interface and Go mapper"
```

---

## Task 5: Python and TypeScript Mappers

**Files:**
- Create: `internal/codedb/match/mapper/python.go`
- Create: `internal/codedb/match/mapper/typescript.go`
- Modify: `internal/codedb/match/mapper/mapper_test.go` (add Python/TS tests)

**Step 1: Write Python mapper tests**

Same pattern as Go tests but with Python source and Python node types:
- Call: `call` node
- MethodCall: `call` with `attribute` function
- Assignment: `assignment` node
- If: `if_statement`
- FuncDef: `function_definition`
- BinaryOp: `comparison_operator` or `boolean_operator`

**Step 2: Write TypeScript mapper tests**

Same pattern with TypeScript source:
- Call: `call_expression`
- MethodCall: `call_expression` with `member_expression`
- Assignment: `assignment_expression`
- If: `if_statement`
- FuncDef: `function_declaration`
- BinaryOp: `binary_expression`

Note: TypeScript needs `grammars.NewGenericTokenSourceOrEOF` token source factory.

**Step 3: Implement Python mapper** (`python.go`)

**Step 4: Implement TypeScript mapper** (`typescript.go`)

**Step 5: Run all mapper tests**

Run: `go test ./internal/codedb/match/mapper/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/codedb/match/mapper/
git commit -m "feat(match): add Python and TypeScript mappers"
```

---

## Task 6: Core Matching Engine

The recursive matcher that matches PatternNode IR against tree-sitter AST nodes.

**Files:**
- Create: `internal/codedb/match/engine/match.go`
- Create: `internal/codedb/match/engine/sequence.go`
- Test: `internal/codedb/match/engine/engine_test.go`

**Step 1: Write the test**

```go
// internal/codedb/match/engine/engine_test.go
package engine

import (
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/sageox/codedbgo/internal/codedb/match/mapper"
	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
)

func parseGoCode(t *testing.T, src string) (*gotreesitter.Tree, *gotreesitter.Language) {
	t.Helper()
	lang := grammars.GoLanguage()
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.ParseWithTokenSource([]byte(src), grammars.NewGoTokenSourceOrEOF([]byte(src), lang))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree")
	}
	t.Cleanup(func() { tree.Release() })
	return tree, lang
}

func TestMatchLiteralCall(t *testing.T) {
	src := `package main
func main() { foo(1) }`
	tree, lang := parseGoCode(t, src)
	pat, _ := pattern.Parse("foo($X)")
	m := mapper.NewGoMapper(lang, []byte(src))
	matches := Match(pat, tree.RootNode(), m)
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Bindings["X"] != "1" {
		t.Errorf("$X = %q, want %q", matches[0].Bindings["X"], "1")
	}
}

func TestMatchNoMatch(t *testing.T) {
	src := `package main
func main() { bar(1) }`
	tree, lang := parseGoCode(t, src)
	pat, _ := pattern.Parse("foo($X)")
	m := mapper.NewGoMapper(lang, []byte(src))
	matches := Match(pat, tree.RootNode(), m)
	if len(matches) != 0 {
		t.Fatalf("matches = %d, want 0", len(matches))
	}
}

func TestMatchMetavarUnification(t *testing.T) {
	src := `package main
func main() {
	foo(a, b, a)
	foo(a, b, c)
}`
	tree, lang := parseGoCode(t, src)
	pat, _ := pattern.Parse("foo($X, ..., $X)")
	m := mapper.NewGoMapper(lang, []byte(src))
	matches := Match(pat, tree.RootNode(), m)
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1 (only foo(a,b,a))", len(matches))
	}
	if matches[0].Bindings["X"] != "a" {
		t.Errorf("$X = %q, want %q", matches[0].Bindings["X"], "a")
	}
}

func TestMatchSelfAssignment(t *testing.T) {
	src := `package main
func main() {
	x := x
	y := z
}`
	tree, lang := parseGoCode(t, src)
	pat, _ := pattern.Parse("$X := $X")
	m := mapper.NewGoMapper(lang, []byte(src))
	matches := Match(pat, tree.RootNode(), m)
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
}

func TestMatchBinaryOp(t *testing.T) {
	src := `package main
func main() {
	_ = x == nil
	_ = y != nil
}`
	tree, lang := parseGoCode(t, src)
	pat, _ := pattern.Parse("$X == nil")
	m := mapper.NewGoMapper(lang, []byte(src))
	matches := Match(pat, tree.RootNode(), m)
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
}

func TestMatchWildcard(t *testing.T) {
	src := `package main
func main() {
	foo(1, 2)
	foo(1)
}`
	tree, lang := parseGoCode(t, src)
	pat, _ := pattern.Parse("foo($_, $_)")
	m := mapper.NewGoMapper(lang, []byte(src))
	matches := Match(pat, tree.RootNode(), m)
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1 (only foo(1,2))", len(matches))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/codedb/match/engine/ -v -run TestMatch`
Expected: FAIL — Match, MatchResult undefined

**Step 3: Write the matching engine**

Create `internal/codedb/match/engine/match.go`:
- `MatchResult` struct: `{File, Line, Col, EndLine, EndCol, Text string, Bindings map[string]string}`
- `Match(pat PatternNode, root *Node, mapper LangMapper) []MatchResult` — walks
  every node in the tree, tries `matchNode` against each
- `matchNode(pat PatternNode, node *Node, mapper LangMapper, bindings map[string]string) (bool, map[string]string)`:
  - Literal: compare node text
  - Metavar: bind or unify
  - Wildcard: always match
  - Call: check IsCall, match func name, matchSequence on args
  - MethodCall: check IsMethodCall, match object + method + args
  - BinaryOp: check IsBinaryOp, match op string, match left/right
  - Assignment: check IsAssignment, match left/right
  - DeepExpr: walk all descendants, try matchNode on each
  - IfStmt/FuncDef: check type, match sub-parts

Create `internal/codedb/match/engine/sequence.go`:
- `matchSequence(patterns []PatternNode, nodes []*Node, mapper, bindings) (bool, map[string]string)`
- Handles Ellipsis by trying skip 0..N nodes
- Handles EllipsisMetavar similarly but captures

**Step 4: Run test to verify it passes**

Run: `go test ./internal/codedb/match/engine/ -v -run TestMatch`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/codedb/match/engine/
git commit -m "feat(match): add core matching engine with sequence matching"
```

---

## Task 7: Boolean Combinators and Constraints

**Files:**
- Create: `internal/codedb/match/engine/formula.go`
- Create: `internal/codedb/match/engine/constraint.go`
- Modify: `internal/codedb/match/engine/engine_test.go` (add combinator tests)

**Step 1: Write tests for Not, Inside, and MetavarConstraint**

Test `Not`: pattern matches, but --not pattern also matches → exclude.
Test `Inside`: pattern matches only when inside a func def.
Test `MetavarConstraint`: `$F ~ /^unsafe_/` filters by regex on bound value.

**Step 2: Implement formula.go**

`ApplyFormula(formula MatchFormula, root *Node, mapper LangMapper) []MatchResult`:
- BasePattern: call `Match()`
- And: intersect results from all sub-formulas
- Not: subtract matches of sub-formula
- Inside: for each match, check if any node in the tree matching the Inside
  pattern contains this match's position
- NotInside: inverse of Inside

**Step 3: Implement constraint.go**

`ApplyConstraints(matches []MatchResult, constraints []MetavarConstraint) []MatchResult`:
- `~`: compile regex, test against binding value
- `!~`: inverse
- `==`, `!=`: string comparison
- `<`, `>`, `<=`, `>=`: parse as numbers, compare

**Step 4: Run tests**

Run: `go test ./internal/codedb/match/engine/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/codedb/match/engine/
git commit -m "feat(match): add boolean combinators and metavariable constraints"
```

---

## Task 8: Executor (Pre-filter + Orchestration)

Ties everything together: SQL pre-filter → load source → parse AST → match → format.

**Files:**
- Create: `internal/codedb/match/executor.go`
- Test: `internal/codedb/match/executor_test.go`

**Step 1: Write the test**

Integration test using an in-memory SQLite store with test data. Insert a blob
with known Go source, then run a match query and verify results.

Reference `internal/codedb/search/executor_test.go` for how to set up a test
store with `store.Open(t.TempDir())`.

**Step 2: Implement executor.go**

```go
// internal/codedb/match/executor.go
package match

// MatchOptions holds options for a match query.
type MatchOptions struct {
	Pattern    string
	Lang       string
	Repo       string
	File       string
	NotPats    []string
	InsidePats []string
	NotInsidePats []string
	WhereClauses  []string
	MaxResults int
	JSONOutput bool
}

// Execute runs a structural pattern match against the store.
func Execute(ctx context.Context, s *store.Store, opts MatchOptions) ([]engine.MatchResult, error)
```

Implementation:
1. Parse the main pattern via `pattern.Parse(opts.Pattern)`
2. Extract literal hints from pattern (function names, identifiers)
3. Build pre-filter SQL using hints + opts.Lang/Repo/File
4. Query candidate blobs from SQLite
5. For each blob: load source from git repo, parse with tree-sitter,
   run matcher, collect results
6. Parse and apply --not, --inside, --not-inside as formula wrappers
7. Parse and apply --where as MetavarConstraint
8. Return results (up to MaxResults)

Key helper needed: load blob source from git. Reference
`internal/codedb/index/indexer.go:619` `readBlobText()` — but that function
uses `git.Repository`. The executor needs to open the repo from
`store.ReposDir()` + repo path from SQL.

**Step 3: Run test**

Run: `go test ./internal/codedb/match/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/codedb/match/executor.go internal/codedb/match/executor_test.go
git commit -m "feat(match): add executor with SQL pre-filter and AST matching"
```

---

## Task 9: CLI `match` Subcommand

Wire everything into the Cobra CLI.

**Files:**
- Modify: `cmd/codedb/main.go` (add matchCmd)
- Modify: `internal/codedb/codedb.go` (add Match method to DB facade)

**Step 1: Add Match to DB facade**

```go
// In internal/codedb/codedb.go, add:
import "github.com/sageox/codedbgo/internal/codedb/match"

func (db *DB) Match(ctx context.Context, opts match.MatchOptions) ([]match.MatchResult, error) {
	return match.Execute(ctx, db.store, opts)
}
```

Note: `MatchResult` may need to be re-exported from `match` package or use
`engine.MatchResult` — adjust the import path based on where the type lives.

**Step 2: Add matchCmd to main.go**

```go
// In cmd/codedb/main.go, add:

var matchCmd = &cobra.Command{
	Use:   "match <pattern>",
	Short: "Find structural code patterns using AST matching",
	Long: `Find structural code patterns across indexed repositories.

Patterns look like code with metavariables ($X) and ellipsis (...).

Examples:
  codedb match 'foo($X, ..., $X)' --lang python
  codedb match '$X = $X' --lang go
  codedb match 'eval($X)' --lang typescript --not 'eval("...")'
  codedb match 'os.system($CMD)' --lang python --where '$CMD ~ /^"/' `,
	Args: cobra.ExactArgs(1),
	RunE: runMatch,
}

var matchFlags struct {
	lang      string
	repo      string
	file      string
	notPats   []string
	inside    []string
	notInside []string
	where     []string
	jsonOut   bool
	count     int
}
```

Add in `init()`:
```go
matchCmd.Flags().StringVar(&matchFlags.lang, "lang", "", "target language (required)")
matchCmd.MarkFlagRequired("lang")
matchCmd.Flags().StringVar(&matchFlags.repo, "repo", "", "filter to repository")
matchCmd.Flags().StringVar(&matchFlags.file, "file", "", "filter to file glob")
matchCmd.Flags().StringSliceVar(&matchFlags.notPats, "not", nil, "exclude pattern (repeatable)")
matchCmd.Flags().StringSliceVar(&matchFlags.inside, "inside", nil, "require inside pattern (repeatable)")
matchCmd.Flags().StringSliceVar(&matchFlags.notInside, "not-inside", nil, "exclude inside pattern (repeatable)")
matchCmd.Flags().StringSliceVar(&matchFlags.where, "where", nil, "metavar constraint (repeatable)")
matchCmd.Flags().BoolVar(&matchFlags.jsonOut, "json", false, "JSON output")
matchCmd.Flags().IntVar(&matchFlags.count, "count", 0, "max results")
rootCmd.AddCommand(matchCmd)
```

Implement `runMatch`:
```go
func runMatch(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	opts := match.MatchOptions{
		Pattern:       args[0],
		Lang:          matchFlags.lang,
		Repo:          matchFlags.repo,
		File:          matchFlags.file,
		NotPats:       matchFlags.notPats,
		InsidePats:    matchFlags.inside,
		NotInsidePats: matchFlags.notInside,
		WhereClauses:  matchFlags.where,
		MaxResults:    matchFlags.count,
		JSONOutput:    matchFlags.jsonOut,
	}

	results, err := db.Match(cmd.Context(), opts)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No matches found.")
		return nil
	}

	if matchFlags.jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	for _, r := range results {
		fmt.Fprintf(cmd.OutOrStdout(), "%s:%d:%d: %s\n", r.File, r.Line, r.Col, r.Text)
		for name, val := range r.Bindings {
			fmt.Fprintf(cmd.OutOrStdout(), "  $%s = %q\n", name, val)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n%d matches found\n", len(results))
	return nil
}
```

**Step 3: Build and smoke test**

Run: `go build -o bin/codedb ./cmd/codedb/`
Expected: builds successfully

Run: `./bin/codedb match --help`
Expected: shows usage with all flags

**Step 4: Commit**

```bash
git add cmd/codedb/main.go internal/codedb/codedb.go
git commit -m "feat(match): add codedb match CLI subcommand"
```

---

## Task 10: End-to-End Integration Test

Test the full pipeline against a real indexed repository.

**Files:**
- Modify: `internal/codedb/match/executor_test.go` (add E2E tests)

**Step 1: Write E2E tests**

Three tests, one per language:

1. **Go**: Index a temp git repo with Go source containing `foo(x, y, x)` and
   `foo(x, y, z)`. Run `match 'foo($X, ..., $X)' --lang go`. Expect 1 match.

2. **Python**: Index repo with `os.system(cmd)` and `os.system("ls")`. Run
   `match 'os.system($X)' --lang python --not 'os.system("ls")'`. Expect 1 match.

3. **TypeScript**: Index repo with `eval(input)` inside a function and
   `eval("safe")` at top level. Run
   `match 'eval($X)' --lang typescript --inside 'function $F(...) { ... }'`.
   Expect 1 match.

These tests need to create a temp git repo, index it, then run match. Reference
`internal/codedb/index/` tests for how to create test repos.

**Step 2: Run tests**

Run: `go test ./internal/codedb/match/ -v -run TestE2E`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/codedb/match/executor_test.go
git commit -m "test(match): add end-to-end integration tests for Go, Python, TypeScript"
```

---

## Task 11: Manual Smoke Test Against TraceForge

Not automated — run against the already-indexed TraceForge repo.

**Step 1: Build**

Run: `go build -o bin/codedb ./cmd/codedb/`

**Step 2: Run pattern matches**

```bash
# Find all verify() calls
./bin/codedb match 'verify(...)' --lang go

# Find self-assignments (should find none in well-written code)
./bin/codedb match '$X = $X' --lang go

# Find function definitions
./bin/codedb match 'func $F(...) { ... }' --lang go
```

Verify output format matches the manual spec in `docs/match-manual.md`.

**Step 3: Fix any issues found**

**Step 4: Commit any fixes**

---

## Task 12: Run Full Test Suite

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: PASS for all packages including new match/ packages

**Step 2: Check build**

Run: `go build ./...`
Expected: clean build, no warnings

**Step 3: Final commit**

```bash
git add -A
git commit -m "feat(match): structural pattern matching v1 complete

Adds codedb match subcommand with Semgrep-style pattern matching.
Supports metavariables, ellipsis, deep expressions, boolean
combinators, and metavariable constraints for Go, Python, TypeScript."
```

---

## Dependency Graph

```
Task 1 (IR types)
  ↓
Task 2 (Tokenizer) → Task 3 (Parser)
  ↓                      ↓
Task 4 (Go mapper) → Task 6 (Match engine) → Task 7 (Combinators)
  ↓                                              ↓
Task 5 (Py/TS mappers)                     Task 8 (Executor)
                                                ↓
                                          Task 9 (CLI)
                                                ↓
                                          Task 10 (E2E tests)
                                                ↓
                                          Task 11 (Smoke test)
                                                ↓
                                          Task 12 (Full suite)
```

Tasks 1-3 (IR + parser) can be done in sequence.
Tasks 4-5 (mappers) can be done in parallel.
Tasks 6-7 (engine + combinators) depend on IR + parser + at least one mapper.
Tasks 8-12 are sequential.

> Guided by SageOx
