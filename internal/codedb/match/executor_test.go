package match

import (
	"testing"

	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
)

func TestParseWhereClause(t *testing.T) {
	tests := []struct {
		input   string
		metavar string
		op      string
		value   string
	}{
		{"$F ~ /^unsafe_/", "F", "~", "/^unsafe_/"},
		{"$X == nil", "X", "==", "nil"},
		{"$X != 0", "X", "!=", "0"},
		{"$N > 10", "N", ">", "10"},
		{"$CMD !~ /^\"safe/", "CMD", "!~", `/^"safe/`},
	}
	for _, tt := range tests {
		c, err := parseWhereClause(tt.input)
		if err != nil {
			t.Errorf("parseWhereClause(%q) error: %v", tt.input, err)
			continue
		}
		if c.Metavar != tt.metavar {
			t.Errorf("metavar = %q, want %q", c.Metavar, tt.metavar)
		}
		if c.Op != tt.op {
			t.Errorf("op = %q, want %q", c.Op, tt.op)
		}
		if c.Value != tt.value {
			t.Errorf("value = %q, want %q", c.Value, tt.value)
		}
	}
}

func TestBuildFormulaNoOptions(t *testing.T) {
	pat, _ := pattern.Parse("foo($X)")
	formula, constraints, err := buildFormula(pat, MatchOptions{Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if formula != nil {
		t.Error("expected nil formula when no options")
	}
	if len(constraints) != 0 {
		t.Error("expected no constraints")
	}
}

func TestBuildFormulaWithNot(t *testing.T) {
	pat, _ := pattern.Parse("foo($X)")
	opts := MatchOptions{
		Lang:    "go",
		NotPats: []string{"foo(1)"},
	}
	formula, _, err := buildFormula(pat, opts)
	if err != nil {
		t.Fatal(err)
	}
	and, ok := formula.(*pattern.And)
	if !ok {
		t.Fatalf("expected And, got %T", formula)
	}
	if len(and.Formulas) != 2 {
		t.Fatalf("formulas = %d, want 2", len(and.Formulas))
	}
	if _, ok := and.Formulas[1].(*pattern.Not); !ok {
		t.Errorf("formula[1] = %T, want Not", and.Formulas[1])
	}
}

func TestBuildFormulaWithWhere(t *testing.T) {
	pat, _ := pattern.Parse("$F($X)")
	opts := MatchOptions{
		Lang:         "go",
		WhereClauses: []string{"$F ~ /^unsafe_/"},
	}
	_, constraints, err := buildFormula(pat, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(constraints) != 1 {
		t.Fatalf("constraints = %d, want 1", len(constraints))
	}
	if constraints[0].Metavar != "F" || constraints[0].Op != "~" {
		t.Errorf("constraint = %+v", constraints[0])
	}
}

func TestExtractHints(t *testing.T) {
	pat, _ := pattern.Parse("foo($X)")
	hints := extractHints(pat)
	if len(hints) != 1 || hints[0] != "foo" {
		t.Errorf("hints = %v, want [foo]", hints)
	}
}
