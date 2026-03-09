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
