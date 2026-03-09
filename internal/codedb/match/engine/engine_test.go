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

// --- Formula tests ---

func TestFormulaNotExcludes(t *testing.T) {
	src := `package main
func main() {
	foo(1)
	foo(2)
	bar(3)
}`
	tree, lang := parseGoCode(t, src)
	m := mapper.NewGoMapper(lang, []byte(src))
	basePat, _ := pattern.Parse("foo($X)")
	notPat, _ := pattern.Parse("foo(1)")

	formula := &pattern.And{
		Formulas: []pattern.MatchFormula{
			&pattern.BasePattern{Lang: "go", Pattern: basePat},
			&pattern.Not{Formula: &pattern.BasePattern{Lang: "go", Pattern: notPat}},
		},
	}
	results := ApplyFormula(formula, tree.RootNode(), m)
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (foo(2) only)", len(results))
	}
	if results[0].Bindings["X"] != "2" {
		t.Errorf("$X = %q, want %q", results[0].Bindings["X"], "2")
	}
}

func TestFormulaInside(t *testing.T) {
	src := `package main
func safe() { foo(1) }
func unsafe() { foo(2) }
`
	tree, lang := parseGoCode(t, src)
	m := mapper.NewGoMapper(lang, []byte(src))
	basePat, _ := pattern.Parse("foo($X)")
	insidePat, _ := pattern.Parse("func unsafe(...) { ... }")

	formula := &pattern.And{
		Formulas: []pattern.MatchFormula{
			&pattern.BasePattern{Lang: "go", Pattern: basePat},
			&pattern.Inside{Formula: &pattern.BasePattern{Lang: "go", Pattern: insidePat}},
		},
	}
	results := ApplyFormula(formula, tree.RootNode(), m)
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (foo inside unsafe only)", len(results))
	}
	if results[0].Bindings["X"] != "2" {
		t.Errorf("$X = %q, want %q", results[0].Bindings["X"], "2")
	}
}

// --- Constraint tests ---

func TestConstraintRegex(t *testing.T) {
	results := []MatchResult{
		{Bindings: map[string]string{"F": "unsafe_exec"}},
		{Bindings: map[string]string{"F": "safe_run"}},
		{Bindings: map[string]string{"F": "unsafe_read"}},
	}
	constraints := []pattern.MetavarConstraint{
		{Metavar: "F", Op: "~", Value: "/^unsafe_/"},
	}
	filtered := ApplyConstraints(results, constraints)
	if len(filtered) != 2 {
		t.Fatalf("filtered = %d, want 2", len(filtered))
	}
}

func TestConstraintNotRegex(t *testing.T) {
	results := []MatchResult{
		{Bindings: map[string]string{"F": "unsafe_exec"}},
		{Bindings: map[string]string{"F": "safe_run"}},
	}
	constraints := []pattern.MetavarConstraint{
		{Metavar: "F", Op: "!~", Value: "/^unsafe_/"},
	}
	filtered := ApplyConstraints(results, constraints)
	if len(filtered) != 1 {
		t.Fatalf("filtered = %d, want 1", len(filtered))
	}
	if filtered[0].Bindings["F"] != "safe_run" {
		t.Errorf("F = %q, want safe_run", filtered[0].Bindings["F"])
	}
}

func TestConstraintEqual(t *testing.T) {
	results := []MatchResult{
		{Bindings: map[string]string{"X": "nil"}},
		{Bindings: map[string]string{"X": "0"}},
	}
	constraints := []pattern.MetavarConstraint{
		{Metavar: "X", Op: "==", Value: "nil"},
	}
	filtered := ApplyConstraints(results, constraints)
	if len(filtered) != 1 {
		t.Fatalf("filtered = %d, want 1", len(filtered))
	}
}
