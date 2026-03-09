package mapper

import "github.com/odvcencio/gotreesitter"

// TypeScriptMapper implements LangMapper for TypeScript source code.
type TypeScriptMapper struct {
	lang *gotreesitter.Language
	src  []byte
}

// NewTypeScriptMapper creates a new TypeScriptMapper.
func NewTypeScriptMapper(lang *gotreesitter.Language, src []byte) *TypeScriptMapper {
	return &TypeScriptMapper{lang: lang, src: src}
}

func (m *TypeScriptMapper) Lang() *gotreesitter.Language { return m.lang }

func (m *TypeScriptMapper) NodeText(node *gotreesitter.Node) string {
	return node.Text(m.src)
}

// --- Call ---

func (m *TypeScriptMapper) IsCall(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "call_expression"
}

func (m *TypeScriptMapper) CallFunc(node *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(m.lang) == "arguments" {
			break
		}
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *TypeScriptMapper) CallArgs(node *gotreesitter.Node) []*gotreesitter.Node {
	argList := m.findChild(node, "arguments")
	if argList == nil {
		return nil
	}
	return m.namedChildren(argList)
}

// --- Method Call ---

func (m *TypeScriptMapper) IsMethodCall(node *gotreesitter.Node) bool {
	if node.Type(m.lang) != "call_expression" {
		return false
	}
	fn := m.CallFunc(node)
	return fn != nil && fn.Type(m.lang) == "member_expression"
}

func (m *TypeScriptMapper) MethodObject(node *gotreesitter.Node) *gotreesitter.Node {
	mem := m.CallFunc(node)
	if mem == nil {
		return nil
	}
	// member_expression: object "." property
	for i := 0; i < mem.ChildCount(); i++ {
		child := mem.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *TypeScriptMapper) MethodName(node *gotreesitter.Node) *gotreesitter.Node {
	mem := m.CallFunc(node)
	if mem == nil {
		return nil
	}
	return m.findChild(mem, "property_identifier")
}

func (m *TypeScriptMapper) MethodArgs(node *gotreesitter.Node) []*gotreesitter.Node {
	return m.CallArgs(node)
}

// --- Assignment ---

func (m *TypeScriptMapper) IsAssignment(node *gotreesitter.Node) bool {
	typ := node.Type(m.lang)
	return typ == "assignment_expression" || typ == "variable_declarator"
}

func (m *TypeScriptMapper) AssignLeft(node *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *TypeScriptMapper) AssignRight(node *gotreesitter.Node) *gotreesitter.Node {
	var last *gotreesitter.Node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			last = child
		}
	}
	return last
}

// --- If ---

func (m *TypeScriptMapper) IsIfStmt(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "if_statement"
}

func (m *TypeScriptMapper) IfCond(node *gotreesitter.Node) *gotreesitter.Node {
	paren := m.findChild(node, "parenthesized_expression")
	if paren != nil {
		// Unwrap: the condition is inside parens
		for i := 0; i < paren.ChildCount(); i++ {
			child := paren.Child(i)
			if child.IsNamed() {
				return child
			}
		}
	}
	// Fallback: first named child that isn't a block
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() && child.Type(m.lang) != "statement_block" {
			return child
		}
	}
	return nil
}

func (m *TypeScriptMapper) IfBody(node *gotreesitter.Node) []*gotreesitter.Node {
	block := m.findChild(node, "statement_block")
	if block == nil {
		return nil
	}
	return m.namedChildren(block)
}

// --- FuncDef ---

func (m *TypeScriptMapper) IsFuncDef(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "function_declaration"
}

func (m *TypeScriptMapper) FuncName(node *gotreesitter.Node) *gotreesitter.Node {
	return m.findChild(node, "identifier")
}

func (m *TypeScriptMapper) FuncParams(node *gotreesitter.Node) []*gotreesitter.Node {
	params := m.findChild(node, "formal_parameters")
	if params == nil {
		return nil
	}
	return m.namedChildren(params)
}

func (m *TypeScriptMapper) FuncBody(node *gotreesitter.Node) []*gotreesitter.Node {
	block := m.findChild(node, "statement_block")
	if block == nil {
		return nil
	}
	return m.namedChildren(block)
}

// --- BinaryOp ---

func (m *TypeScriptMapper) IsBinaryOp(node *gotreesitter.Node) (bool, string) {
	if node.Type(m.lang) != "binary_expression" {
		return false, ""
	}
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

func (m *TypeScriptMapper) BinaryLeft(node *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *TypeScriptMapper) BinaryRight(node *gotreesitter.Node) *gotreesitter.Node {
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

func (m *TypeScriptMapper) findChild(node *gotreesitter.Node, typ string) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(m.lang) == typ {
			return child
		}
	}
	return nil
}

func (m *TypeScriptMapper) namedChildren(node *gotreesitter.Node) []*gotreesitter.Node {
	var children []*gotreesitter.Node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			children = append(children, child)
		}
	}
	return children
}
