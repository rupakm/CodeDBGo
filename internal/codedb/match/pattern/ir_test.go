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
