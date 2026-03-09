package engine

import (
	"github.com/odvcencio/gotreesitter"
	"github.com/sageox/codedbgo/internal/codedb/match/mapper"
	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
)

// MatchResult represents a single match of a pattern against source code.
type MatchResult struct {
	File    string
	Line    int
	Col     int
	EndLine int
	EndCol  int
	Text    string
	Bindings map[string]string
}

// Match walks the AST rooted at root and returns all nodes matching pat.
func Match(pat pattern.PatternNode, root *gotreesitter.Node, m mapper.LangMapper) []MatchResult {
	var results []MatchResult
	walkTree(root, m, func(node *gotreesitter.Node) {
		bindings := make(map[string]string)
		if matchNode(pat, node, m, bindings) {
			start := node.StartPoint()
			end := node.EndPoint()
			results = append(results, MatchResult{
				Line:     int(start.Row) + 1,
				Col:      int(start.Column) + 1,
				EndLine:  int(end.Row) + 1,
				EndCol:   int(end.Column) + 1,
				Text:     m.NodeText(node),
				Bindings: bindings,
			})
		}
	})
	return results
}

// walkTree visits every node in the tree in pre-order.
func walkTree(node *gotreesitter.Node, m mapper.LangMapper, fn func(*gotreesitter.Node)) {
	fn(node)
	for i := 0; i < node.ChildCount(); i++ {
		walkTree(node.Child(i), m, fn)
	}
}

// matchNode tries to match a pattern against a single AST node.
func matchNode(pat pattern.PatternNode, node *gotreesitter.Node, m mapper.LangMapper, bindings map[string]string) bool {
	switch p := pat.(type) {
	case *pattern.Literal:
		return m.NodeText(node) == p.Value

	case *pattern.Metavar:
		return bindMetavar(p.Name, m.NodeText(node), bindings)

	case *pattern.Wildcard:
		return true

	case *pattern.Ellipsis:
		return true

	case *pattern.EllipsisMetavar:
		// Matches a single node and captures its text
		return bindMetavar(p.Name, m.NodeText(node), bindings)

	case *pattern.Call:
		if !m.IsCall(node) {
			return false
		}
		fn := m.CallFunc(node)
		if fn == nil {
			return false
		}
		// For method calls, the function child is a selector_expression.
		// Match pattern func against the function name directly.
		if !matchNode(p.Func, fn, m, bindings) {
			return false
		}
		args := m.CallArgs(node)
		return matchSequence(p.Args, args, m, bindings)

	case *pattern.MethodCall:
		if !m.IsMethodCall(node) {
			return false
		}
		obj := m.MethodObject(node)
		if obj == nil || !matchNode(p.Object, obj, m, bindings) {
			return false
		}
		name := m.MethodName(node)
		if name == nil || !matchNode(p.Method, name, m, bindings) {
			return false
		}
		if p.Args == nil {
			return true
		}
		args := m.MethodArgs(node)
		return matchSequence(p.Args, args, m, bindings)

	case *pattern.BinaryOp:
		isBin, op := m.IsBinaryOp(node)
		if !isBin || op != p.Op {
			return false
		}
		left := m.BinaryLeft(node)
		right := m.BinaryRight(node)
		if left == nil || right == nil {
			return false
		}
		return matchNode(p.Left, left, m, bindings) && matchNode(p.Right, right, m, bindings)

	case *pattern.Assignment:
		if !m.IsAssignment(node) {
			return false
		}
		left := m.AssignLeft(node)
		right := m.AssignRight(node)
		if left == nil || right == nil {
			return false
		}
		return matchNode(p.Left, left, m, bindings) && matchNode(p.Right, right, m, bindings)

	case *pattern.IfStmt:
		if !m.IsIfStmt(node) {
			return false
		}
		cond := m.IfCond(node)
		if cond == nil || !matchNode(p.Cond, cond, m, bindings) {
			return false
		}
		if p.Body == nil {
			return true
		}
		body := m.IfBody(node)
		return matchSequence(p.Body, body, m, bindings)

	case *pattern.FuncDef:
		if !m.IsFuncDef(node) {
			return false
		}
		name := m.FuncName(node)
		if name == nil || !matchNode(p.Name, name, m, bindings) {
			return false
		}
		if p.Params != nil {
			params := m.FuncParams(node)
			if !matchSequence(p.Params, params, m, bindings) {
				return false
			}
		}
		if p.Body != nil {
			body := m.FuncBody(node)
			return matchSequence(p.Body, body, m, bindings)
		}
		return true

	case *pattern.DeepExpr:
		// Match the inner pattern against any descendant
		found := false
		walkTree(node, m, func(desc *gotreesitter.Node) {
			if !found {
				descBindings := copyBindings(bindings)
				if matchNode(p.Inner, desc, m, descBindings) {
					// Apply bindings from successful match
					for k, v := range descBindings {
						bindings[k] = v
					}
					found = true
				}
			}
		})
		return found

	case *pattern.Block:
		// Match block statements against children
		var children []*gotreesitter.Node
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.IsNamed() {
				children = append(children, child)
			}
		}
		return matchSequence(p.Stmts, children, m, bindings)
	}

	return false
}

// bindMetavar binds a name to a value, or unifies if already bound.
func bindMetavar(name, value string, bindings map[string]string) bool {
	if existing, ok := bindings[name]; ok {
		return existing == value
	}
	bindings[name] = value
	return true
}

func copyBindings(b map[string]string) map[string]string {
	c := make(map[string]string, len(b))
	for k, v := range b {
		c[k] = v
	}
	return c
}
