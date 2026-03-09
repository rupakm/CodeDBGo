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
