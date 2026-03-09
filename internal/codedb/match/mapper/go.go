package mapper

import "github.com/odvcencio/gotreesitter"

// GoMapper implements LangMapper for Go source code.
type GoMapper struct {
	lang *gotreesitter.Language
	src  []byte
}

// NewGoMapper creates a new GoMapper.
func NewGoMapper(lang *gotreesitter.Language, src []byte) *GoMapper {
	return &GoMapper{lang: lang, src: src}
}

func (m *GoMapper) Lang() *gotreesitter.Language { return m.lang }

func (m *GoMapper) NodeText(node *gotreesitter.Node) string {
	return node.Text(m.src)
}

// --- Call ---

func (m *GoMapper) IsCall(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "call_expression"
}

func (m *GoMapper) CallFunc(node *gotreesitter.Node) *gotreesitter.Node {
	// call_expression: first named child is the function
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		typ := child.Type(m.lang)
		if typ == "argument_list" {
			break
		}
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *GoMapper) CallArgs(node *gotreesitter.Node) []*gotreesitter.Node {
	argList := m.findChild(node, "argument_list")
	if argList == nil {
		return nil
	}
	return m.namedChildren(argList)
}

// --- Method Call ---

func (m *GoMapper) IsMethodCall(node *gotreesitter.Node) bool {
	if node.Type(m.lang) != "call_expression" {
		return false
	}
	fn := m.CallFunc(node)
	return fn != nil && fn.Type(m.lang) == "selector_expression"
}

func (m *GoMapper) MethodObject(node *gotreesitter.Node) *gotreesitter.Node {
	sel := m.CallFunc(node)
	if sel == nil {
		return nil
	}
	// selector_expression: operand . field
	for i := 0; i < sel.ChildCount(); i++ {
		child := sel.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *GoMapper) MethodName(node *gotreesitter.Node) *gotreesitter.Node {
	sel := m.CallFunc(node)
	if sel == nil {
		return nil
	}
	// selector_expression: last named child is the field
	return m.findChild(sel, "field_identifier")
}

func (m *GoMapper) MethodArgs(node *gotreesitter.Node) []*gotreesitter.Node {
	return m.CallArgs(node)
}

// --- Assignment ---

func (m *GoMapper) IsAssignment(node *gotreesitter.Node) bool {
	typ := node.Type(m.lang)
	return typ == "short_var_declaration" || typ == "assignment_statement"
}

func (m *GoMapper) AssignLeft(node *gotreesitter.Node) *gotreesitter.Node {
	// Left side is the first named child (expression_list)
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			// If it's an expression_list with one child, unwrap it
			if child.Type(m.lang) == "expression_list" && child.ChildCount() > 0 {
				for j := 0; j < child.ChildCount(); j++ {
					c := child.Child(j)
					if c.IsNamed() {
						return c
					}
				}
			}
			return child
		}
	}
	return nil
}

func (m *GoMapper) AssignRight(node *gotreesitter.Node) *gotreesitter.Node {
	// Right side is the last named child (expression_list or single expr)
	var last *gotreesitter.Node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			last = child
		}
	}
	if last == nil {
		return nil
	}
	// Unwrap expression_list with one child
	if last.Type(m.lang) == "expression_list" {
		for j := 0; j < last.ChildCount(); j++ {
			c := last.Child(j)
			if c.IsNamed() {
				return c
			}
		}
	}
	return last
}

// --- If ---

func (m *GoMapper) IsIfStmt(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "if_statement"
}

func (m *GoMapper) IfCond(node *gotreesitter.Node) *gotreesitter.Node {
	// if_statement: "if" condition consequence
	// Condition is the first named child that isn't the block
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() && child.Type(m.lang) != "block" {
			return child
		}
	}
	return nil
}

func (m *GoMapper) IfBody(node *gotreesitter.Node) []*gotreesitter.Node {
	block := m.findChild(node, "block")
	if block == nil {
		return nil
	}
	return m.namedChildren(block)
}

// --- FuncDef ---

func (m *GoMapper) IsFuncDef(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "function_declaration"
}

func (m *GoMapper) FuncName(node *gotreesitter.Node) *gotreesitter.Node {
	return m.findChild(node, "identifier")
}

func (m *GoMapper) FuncParams(node *gotreesitter.Node) []*gotreesitter.Node {
	params := m.findChild(node, "parameter_list")
	if params == nil {
		return nil
	}
	return m.namedChildren(params)
}

func (m *GoMapper) FuncBody(node *gotreesitter.Node) []*gotreesitter.Node {
	block := m.findChild(node, "block")
	if block == nil {
		return nil
	}
	return m.namedChildren(block)
}

// --- BinaryOp ---

func (m *GoMapper) IsBinaryOp(node *gotreesitter.Node) (bool, string) {
	if node.Type(m.lang) != "binary_expression" {
		return false, ""
	}
	// The operator is an unnamed child between left and right
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			text := child.Text(m.src)
			if text != "" && text != "(" && text != ")" {
				return true, text
			}
		}
	}
	return true, ""
}

func (m *GoMapper) BinaryLeft(node *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *GoMapper) BinaryRight(node *gotreesitter.Node) *gotreesitter.Node {
	var last *gotreesitter.Node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			last = child
		}
	}
	return last
}

// --- Helpers ---

func (m *GoMapper) findChild(node *gotreesitter.Node, typ string) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(m.lang) == typ {
			return child
		}
	}
	return nil
}

func (m *GoMapper) namedChildren(node *gotreesitter.Node) []*gotreesitter.Node {
	var children []*gotreesitter.Node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			children = append(children, child)
		}
	}
	return children
}
