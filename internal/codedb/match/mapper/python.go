package mapper

import "github.com/odvcencio/gotreesitter"

// PythonMapper implements LangMapper for Python source code.
type PythonMapper struct {
	lang *gotreesitter.Language
	src  []byte
}

// NewPythonMapper creates a new PythonMapper.
func NewPythonMapper(lang *gotreesitter.Language, src []byte) *PythonMapper {
	return &PythonMapper{lang: lang, src: src}
}

func (m *PythonMapper) Lang() *gotreesitter.Language { return m.lang }

func (m *PythonMapper) NodeText(node *gotreesitter.Node) string {
	return node.Text(m.src)
}

// --- Call ---

func (m *PythonMapper) IsCall(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "call"
}

func (m *PythonMapper) CallFunc(node *gotreesitter.Node) *gotreesitter.Node {
	// call: function arguments
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(m.lang) == "argument_list" {
			break
		}
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *PythonMapper) CallArgs(node *gotreesitter.Node) []*gotreesitter.Node {
	argList := m.findChild(node, "argument_list")
	if argList == nil {
		return nil
	}
	return m.namedChildren(argList)
}

// --- Method Call ---

func (m *PythonMapper) IsMethodCall(node *gotreesitter.Node) bool {
	if node.Type(m.lang) != "call" {
		return false
	}
	fn := m.CallFunc(node)
	return fn != nil && fn.Type(m.lang) == "attribute"
}

func (m *PythonMapper) MethodObject(node *gotreesitter.Node) *gotreesitter.Node {
	attr := m.CallFunc(node)
	if attr == nil {
		return nil
	}
	// attribute: object "." name
	for i := 0; i < attr.ChildCount(); i++ {
		child := attr.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *PythonMapper) MethodName(node *gotreesitter.Node) *gotreesitter.Node {
	attr := m.CallFunc(node)
	if attr == nil {
		return nil
	}
	// Last named child is the method name (identifier)
	var last *gotreesitter.Node
	for i := 0; i < attr.ChildCount(); i++ {
		child := attr.Child(i)
		if child.IsNamed() {
			last = child
		}
	}
	return last
}

func (m *PythonMapper) MethodArgs(node *gotreesitter.Node) []*gotreesitter.Node {
	return m.CallArgs(node)
}

// --- Assignment ---

func (m *PythonMapper) IsAssignment(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "assignment"
}

func (m *PythonMapper) AssignLeft(node *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *PythonMapper) AssignRight(node *gotreesitter.Node) *gotreesitter.Node {
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

func (m *PythonMapper) IsIfStmt(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "if_statement"
}

func (m *PythonMapper) IfCond(node *gotreesitter.Node) *gotreesitter.Node {
	// if_statement: "if" condition ":" body
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() && child.Type(m.lang) != "block" {
			return child
		}
	}
	return nil
}

func (m *PythonMapper) IfBody(node *gotreesitter.Node) []*gotreesitter.Node {
	block := m.findChild(node, "block")
	if block == nil {
		return nil
	}
	return m.namedChildren(block)
}

// --- FuncDef ---

func (m *PythonMapper) IsFuncDef(node *gotreesitter.Node) bool {
	return node.Type(m.lang) == "function_definition"
}

func (m *PythonMapper) FuncName(node *gotreesitter.Node) *gotreesitter.Node {
	return m.findChild(node, "identifier")
}

func (m *PythonMapper) FuncParams(node *gotreesitter.Node) []*gotreesitter.Node {
	params := m.findChild(node, "parameters")
	if params == nil {
		return nil
	}
	return m.namedChildren(params)
}

func (m *PythonMapper) FuncBody(node *gotreesitter.Node) []*gotreesitter.Node {
	block := m.findChild(node, "block")
	if block == nil {
		return nil
	}
	return m.namedChildren(block)
}

// --- BinaryOp ---

func (m *PythonMapper) IsBinaryOp(node *gotreesitter.Node) (bool, string) {
	typ := node.Type(m.lang)
	if typ != "comparison_operator" && typ != "boolean_operator" && typ != "binary_operator" {
		return false, ""
	}
	// Operator is an unnamed child
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

func (m *PythonMapper) BinaryLeft(node *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

func (m *PythonMapper) BinaryRight(node *gotreesitter.Node) *gotreesitter.Node {
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

func (m *PythonMapper) findChild(node *gotreesitter.Node, typ string) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(m.lang) == typ {
			return child
		}
	}
	return nil
}

func (m *PythonMapper) namedChildren(node *gotreesitter.Node) []*gotreesitter.Node {
	var children []*gotreesitter.Node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			children = append(children, child)
		}
	}
	return children
}
