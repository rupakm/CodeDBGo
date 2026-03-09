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
	src := `package main
func main() { foo(1, 2) }`
	tree, lang := parseGo(t, src)
	m := NewGoMapper(lang, []byte(src))
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
	if fn.Text([]byte(src)) != "foo" {
		t.Errorf("func name = %q, want foo", fn.Text([]byte(src)))
	}
	args := m.CallArgs(call)
	if len(args) != 2 {
		t.Errorf("args = %d, want 2", len(args))
	}
}

func TestGoMapperIsAssignment(t *testing.T) {
	src := `package main
func main() { x := 1 }`
	tree, lang := parseGo(t, src)
	m := NewGoMapper(lang, []byte(src))
	root := tree.RootNode()
	decl := findFirst(root, lang, "short_var_declaration")
	if decl == nil {
		t.Fatal("no short_var_declaration found")
	}
	if !m.IsAssignment(decl) {
		t.Error("IsAssignment returned false for short_var_declaration")
	}
	left := m.AssignLeft(decl)
	if left == nil {
		t.Fatal("AssignLeft returned nil")
	}
	if left.Text([]byte(src)) != "x" {
		t.Errorf("left = %q, want x", left.Text([]byte(src)))
	}
	right := m.AssignRight(decl)
	if right == nil {
		t.Fatal("AssignRight returned nil")
	}
	if right.Text([]byte(src)) != "1" {
		t.Errorf("right = %q, want 1", right.Text([]byte(src)))
	}
}

func TestGoMapperIsFuncDef(t *testing.T) {
	src := `package main
func hello(x int) string { return "" }`
	tree, lang := parseGo(t, src)
	m := NewGoMapper(lang, []byte(src))
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
	if name.Text([]byte(src)) != "hello" {
		t.Errorf("func name = %q, want hello", name.Text([]byte(src)))
	}
}

func TestGoMapperIsBinaryOp(t *testing.T) {
	src := `package main
func main() { _ = x == y }`
	tree, lang := parseGo(t, src)
	m := NewGoMapper(lang, []byte(src))
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

func TestGoMapperIsMethodCall(t *testing.T) {
	src := `package main
func main() { x.Save() }`
	tree, lang := parseGo(t, src)
	m := NewGoMapper(lang, []byte(src))
	root := tree.RootNode()
	call := findFirst(root, lang, "call_expression")
	if call == nil {
		t.Fatal("no call_expression found")
	}
	if !m.IsMethodCall(call) {
		t.Error("IsMethodCall returned false")
	}
	obj := m.MethodObject(call)
	if obj == nil {
		t.Fatal("MethodObject returned nil")
	}
	if obj.Text([]byte(src)) != "x" {
		t.Errorf("object = %q, want x", obj.Text([]byte(src)))
	}
	name := m.MethodName(call)
	if name == nil {
		t.Fatal("MethodName returned nil")
	}
	if name.Text([]byte(src)) != "Save" {
		t.Errorf("method = %q, want Save", name.Text([]byte(src)))
	}
}

// --- Python tests ---

func parsePython(t *testing.T, src string) (*gotreesitter.Tree, *gotreesitter.Language) {
	t.Helper()
	lang := grammars.PythonLanguage()
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree")
	}
	t.Cleanup(func() { tree.Release() })
	return tree, lang
}

func TestPythonMapperIsCall(t *testing.T) {
	src := `foo(1, 2)`
	tree, lang := parsePython(t, src)
	m := NewPythonMapper(lang, []byte(src))
	root := tree.RootNode()
	call := findFirst(root, lang, "call")
	if call == nil {
		t.Fatal("no call found")
	}
	if !m.IsCall(call) {
		t.Error("IsCall returned false")
	}
	fn := m.CallFunc(call)
	if fn == nil {
		t.Fatal("CallFunc returned nil")
	}
	if fn.Text([]byte(src)) != "foo" {
		t.Errorf("func name = %q, want foo", fn.Text([]byte(src)))
	}
	args := m.CallArgs(call)
	if len(args) != 2 {
		t.Errorf("args = %d, want 2", len(args))
	}
}

func TestPythonMapperIsMethodCall(t *testing.T) {
	src := `x.save()`
	tree, lang := parsePython(t, src)
	m := NewPythonMapper(lang, []byte(src))
	root := tree.RootNode()
	call := findFirst(root, lang, "call")
	if call == nil {
		t.Fatal("no call found")
	}
	if !m.IsMethodCall(call) {
		t.Error("IsMethodCall returned false")
	}
	obj := m.MethodObject(call)
	if obj == nil {
		t.Fatal("MethodObject returned nil")
	}
	if obj.Text([]byte(src)) != "x" {
		t.Errorf("object = %q, want x", obj.Text([]byte(src)))
	}
}

func TestPythonMapperIsAssignment(t *testing.T) {
	src := `x = 1`
	tree, lang := parsePython(t, src)
	m := NewPythonMapper(lang, []byte(src))
	root := tree.RootNode()
	assign := findFirst(root, lang, "assignment")
	if assign == nil {
		t.Fatal("no assignment found")
	}
	if !m.IsAssignment(assign) {
		t.Error("IsAssignment returned false")
	}
}

func TestPythonMapperIsFuncDef(t *testing.T) {
	src := "def hello(x):\n    pass"
	tree, lang := parsePython(t, src)
	m := NewPythonMapper(lang, []byte(src))
	root := tree.RootNode()
	fn := findFirst(root, lang, "function_definition")
	if fn == nil {
		t.Fatal("no function_definition found")
	}
	if !m.IsFuncDef(fn) {
		t.Error("IsFuncDef returned false")
	}
	name := m.FuncName(fn)
	if name == nil {
		t.Fatal("FuncName returned nil")
	}
	if name.Text([]byte(src)) != "hello" {
		t.Errorf("func name = %q, want hello", name.Text([]byte(src)))
	}
}

// --- TypeScript tests ---

func parseTypeScript(t *testing.T, src string) (*gotreesitter.Tree, *gotreesitter.Language) {
	t.Helper()
	// Use JavaScript grammar without token source for tests — the pure Go
	// TypeScript DFA lexer produces broken trees, and GenericTokenSourceOrEOF
	// also corrupts small inputs. JS grammar shares the same node types.
	// Source must have multiple statements for proper program root.
	lang := grammars.JavascriptLanguage()
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree")
	}
	t.Cleanup(func() { tree.Release() })
	return tree, lang
}

func TestTSMapperIsCall(t *testing.T) {
	src := "var a = 1;\nfoo(1, 2);\nvar b = 2;\n"
	tree, lang := parseTypeScript(t, src)
	m := NewTypeScriptMapper(lang, []byte(src))
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
	if fn.Text([]byte(src)) != "foo" {
		t.Errorf("func name = %q, want foo", fn.Text([]byte(src)))
	}
	args := m.CallArgs(call)
	if len(args) != 2 {
		t.Errorf("args = %d, want 2", len(args))
	}
}

func TestTSMapperIsMethodCall(t *testing.T) {
	src := "var a = 1;\nx.save();\nvar b = 2;\n"
	tree, lang := parseTypeScript(t, src)
	m := NewTypeScriptMapper(lang, []byte(src))
	root := tree.RootNode()
	call := findFirst(root, lang, "call_expression")
	if call == nil {
		t.Fatal("no call_expression found")
	}
	if !m.IsMethodCall(call) {
		t.Error("IsMethodCall returned false")
	}
}

func TestTSMapperIsFuncDef(t *testing.T) {
	src := "var a = 1;\nfunction hello(x) { return x; }\nvar b = 2;\n"
	tree, lang := parseTypeScript(t, src)
	m := NewTypeScriptMapper(lang, []byte(src))
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
	if name.Text([]byte(src)) != "hello" {
		t.Errorf("func name = %q, want hello", name.Text([]byte(src)))
	}
}
