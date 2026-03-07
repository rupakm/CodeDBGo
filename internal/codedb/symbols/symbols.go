// Package symbols extracts symbol definitions and references from source code
// using tree-sitter for accurate AST-based parsing.
package symbols

import (
	"context"
	"sort"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	ts_typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
)

// Symbol represents a symbol definition extracted from source code.
type Symbol struct {
	Name       string
	Kind       string
	Line       int
	Col        int
	EndLine    int
	EndCol     int
	ParentIdx  int // -1 if no parent
	Signature  string
	ReturnType string
	Params     string
	startByte  uint32
	endByte    uint32
}

// Ref represents a reference (call site) extracted from source code.
type Ref struct {
	RefName          string
	Kind             string
	Line             int
	Col              int
	ContainingSymIdx int // -1 if not inside a symbol
}

// languageConfig holds tree-sitter query patterns for a language.
type languageConfig struct {
	name     string
	lang     *sitter.Language
	defQuery string
	refQuery string
}

// Extract extracts symbol definitions and references from source code
// for the given language using tree-sitter.
func Extract(source, language string) ([]Symbol, []Ref) {
	config := getConfig(language)
	if config == nil {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(config.lang)

	tree, err := parser.ParseCtx(context.Background(), nil, []byte(source))
	if err != nil || tree == nil {
		return nil, nil
	}
	defer tree.Close()

	root := tree.RootNode()
	src := []byte(source)

	// Phase 1: extract definition symbols
	symbols := extractDefs(root, src, config)

	// Sort by (startByte, reverse endByte) for nesting detection
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].startByte != symbols[j].startByte {
			return symbols[i].startByte < symbols[j].startByte
		}
		return symbols[i].endByte > symbols[j].endByte
	})

	// Phase 2: determine parent_index via a stack
	type stackEntry struct {
		idx     int
		endByte uint32
	}
	var stack []stackEntry
	for i := range symbols {
		for len(stack) > 0 && stack[len(stack)-1].endByte <= symbols[i].startByte {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			symbols[i].ParentIdx = stack[len(stack)-1].idx
		}
		stack = append(stack, stackEntry{i, symbols[i].endByte})
	}

	// Phase 3: extract references
	refs := extractRefs(root, src, config, symbols)

	return symbols, refs
}

func extractDefs(root *sitter.Node, src []byte, config *languageConfig) []Symbol {
	q, err := sitter.NewQuery([]byte(config.defQuery), config.lang)
	if err != nil {
		return nil
	}
	defer q.Close()

	nameIdx := captureIndex(q, "name")
	defIdx := captureIndex(q, "def")

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	var symbols []Symbol
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		m = qc.FilterPredicates(m, src)

		var nameText string
		var defNode *sitter.Node
		for _, cap := range m.Captures {
			if nameIdx >= 0 && cap.Index == uint32(nameIdx) {
				nameText = cap.Node.Content(src)
			}
			if defIdx >= 0 && cap.Index == uint32(defIdx) {
				defNode = cap.Node
			}
		}

		if nameText == "" || defNode == nil {
			continue
		}

		start := defNode.StartPoint()
		end := defNode.EndPoint()

		sig, retType, params := extractTypeInfo(defNode, src, config.name)

		symbols = append(symbols, Symbol{
			Name:       nameText,
			Kind:       normalizeKind(defNode.Type()),
			Line:       int(start.Row) + 1,
			Col:        int(start.Column) + 1,
			EndLine:    int(end.Row) + 1,
			EndCol:     int(end.Column) + 1,
			ParentIdx:  -1,
			Signature:  sig,
			ReturnType: retType,
			Params:     params,
			startByte:  defNode.StartByte(),
			endByte:    defNode.EndByte(),
		})
	}

	return symbols
}

func extractRefs(root *sitter.Node, src []byte, config *languageConfig, symbols []Symbol) []Ref {
	q, err := sitter.NewQuery([]byte(config.refQuery), config.lang)
	if err != nil {
		return nil
	}
	defer q.Close()

	refNameIdx := captureIndex(q, "ref_name")
	refIdx := captureIndex(q, "ref")

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	var refs []Ref
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		m = qc.FilterPredicates(m, src)

		var rname string
		var refNode *sitter.Node
		for _, cap := range m.Captures {
			if refNameIdx >= 0 && cap.Index == uint32(refNameIdx) {
				rname = cap.Node.Content(src)
			}
			if refIdx >= 0 && cap.Index == uint32(refIdx) {
				refNode = cap.Node
			}
		}

		if rname == "" || refNode == nil {
			continue
		}

		containing := findContainingSymbol(symbols, refNode.StartByte())

		refs = append(refs, Ref{
			RefName:          rname,
			Kind:             "call",
			Line:             int(refNode.StartPoint().Row) + 1,
			Col:              int(refNode.StartPoint().Column) + 1,
			ContainingSymIdx: containing,
		})
	}

	return refs
}

func captureIndex(q *sitter.Query, name string) int {
	for i := uint32(0); i < q.CaptureCount(); i++ {
		if q.CaptureNameForId(i) == name {
			return int(i)
		}
	}
	return -1
}

// findContainingSymbol finds the innermost symbol whose byte range contains byte_offset.
func findContainingSymbol(symbols []Symbol, byteOffset uint32) int {
	best := -1
	for i, sym := range symbols {
		if sym.startByte <= byteOffset && byteOffset < sym.endByte {
			best = i
		}
	}
	return best
}

// normalizeKind maps tree-sitter node kind strings to consistent kind names.
func normalizeKind(tsKind string) string {
	switch tsKind {
	// Rust
	case "function_item":
		return "function"
	case "struct_item":
		return "struct"
	case "enum_item":
		return "enum"
	case "trait_item":
		return "trait"
	case "impl_item":
		return "impl"
	case "const_item":
		return "const"
	case "static_item":
		return "static"
	case "mod_item":
		return "module"
	// Python / C / C++
	case "function_definition":
		return "function"
	case "class_definition":
		return "class"
	// JS / TS / Go
	case "function_declaration":
		return "function"
	case "class_declaration":
		return "class"
	case "method_definition":
		return "method"
	case "interface_declaration":
		return "interface"
	case "enum_declaration":
		return "enum"
	case "type_alias_declaration":
		return "type_alias"
	// Go
	case "method_declaration":
		return "method"
	case "type_declaration", "type_spec":
		return "type"
	// C / C++
	case "struct_specifier":
		return "struct"
	case "enum_specifier":
		return "enum"
	case "class_specifier":
		return "class"
	case "namespace_definition":
		return "namespace"
	default:
		return tsKind
	}
}

// isFunctionLike checks if a node kind represents a function-like definition.
func isFunctionLike(kind string) bool {
	switch kind {
	case "function_item", "function_definition", "function_declaration",
		"method_definition", "method_declaration":
		return true
	}
	return false
}

// extractTypeInfo extracts signature, return_type, and params from a def node.
func extractTypeInfo(node *sitter.Node, src []byte, language string) (string, string, string) {
	sig := extractSignature(node, src)

	if !isFunctionLike(node.Type()) {
		return sig, "", ""
	}

	var retType, params string
	switch language {
	case "rust", "python":
		retType, params = extractArrowFnTypes(node, src)
	case "go":
		retType, params = extractGoFnTypes(node, src)
	case "typescript", "tsx":
		retType, params = extractTSFnTypes(node, src)
	case "javascript":
		_, params = extractJSFnTypes(node, src)
	case "c", "cpp":
		retType, params = extractCFnTypes(node, src)
	}

	return sig, retType, params
}

// collapseWhitespace collapses consecutive whitespace into single spaces.
func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevWS := false
	for _, c := range s {
		if unicode.IsSpace(c) {
			if !prevWS {
				b.WriteByte(' ')
				prevWS = true
			}
		} else {
			b.WriteRune(c)
			prevWS = false
		}
	}
	return strings.TrimSpace(b.String())
}

// bodyKinds are node types that represent body/block nodes.
var bodyKinds = map[string]bool{
	"block": true, "statement_block": true, "compound_statement": true,
	"field_declaration_list": true, "declaration_list": true,
	"class_body": true, "interface_body": true, "enum_variant_list": true,
}

// findBodyStart finds the start byte of the first "body" node child.
func findBodyStart(node *sitter.Node) (uint32, bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if bodyKinds[child.Type()] {
			return child.StartByte(), true
		}
	}
	// Search one level deeper
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		for j := 0; j < int(child.ChildCount()); j++ {
			grandchild := child.Child(j)
			if bodyKinds[grandchild.Type()] {
				return grandchild.StartByte(), true
			}
		}
	}
	return 0, false
}

// extractSignature extracts everything before the body of a def node.
func extractSignature(node *sitter.Node, src []byte) string {
	bodyStart, found := findBodyStart(node)
	sigEnd := node.EndByte()
	if found {
		sigEnd = bodyStart
	} else {
		// Fallback: find first '{' in node text
		text := string(src[node.StartByte():node.EndByte()])
		if idx := strings.Index(text, "{"); idx >= 0 {
			sigEnd = node.StartByte() + uint32(idx)
		}
	}
	sig := string(src[node.StartByte():sigEnd])
	return collapseWhitespace(sig)
}

// childByType finds the first named child with the given type.
func childByType(node *sitter.Node, kind string) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == kind {
			return child
		}
	}
	return nil
}

// childrenSlice returns all children as a slice.
func childrenSlice(node *sitter.Node) []*sitter.Node {
	n := int(node.ChildCount())
	children := make([]*sitter.Node, n)
	for i := 0; i < n; i++ {
		children[i] = node.Child(i)
	}
	return children
}

// extractParamListText strips outer parens from a param list node.
func extractParamListText(node *sitter.Node, src []byte, paramListKind string) string {
	paramNode := childByType(node, paramListKind)
	if paramNode == nil {
		return ""
	}
	text := strings.TrimSpace(paramNode.Content(src))
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	return collapseWhitespace(strings.TrimSpace(text))
}

// findReturnTypeAfterArrow finds the return type after a -> token.
func findReturnTypeAfterArrow(node *sitter.Node, src []byte) string {
	sawArrow := false
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "->" {
			sawArrow = true
			continue
		}
		if sawArrow && child.IsNamed() {
			return child.Content(src)
		}
	}
	return ""
}

// extractArrowFnTypes handles languages using -> for return types and "parameters" for param lists (Rust, Python).
func extractArrowFnTypes(node *sitter.Node, src []byte) (string, string) {
	retType := findReturnTypeAfterArrow(node, src)
	params := extractParamListText(node, src, "parameters")
	return retType, params
}

func extractGoFnTypes(node *sitter.Node, src []byte) (string, string) {
	children := childrenSlice(node)

	// Find the name node (identifier for functions, field_identifier for methods)
	namePos := -1
	for i, child := range children {
		if child.Type() == "identifier" || child.Type() == "field_identifier" {
			namePos = i
			break
		}
	}
	if namePos < 0 {
		return "", ""
	}

	// Find parameter_list after the name
	var paramsNode *sitter.Node
	for _, child := range children[namePos+1:] {
		if child.Type() == "parameter_list" {
			paramsNode = child
			break
		}
	}
	if paramsNode == nil {
		return "", ""
	}

	// Params: strip outer parens
	text := strings.TrimSpace(paramsNode.Content(src))
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	params := collapseWhitespace(strings.TrimSpace(text))

	// Return type: text between params end and block start
	paramsEnd := paramsNode.EndByte()
	blockStart := node.EndByte()
	for _, child := range children {
		if child.Type() == "block" {
			blockStart = child.StartByte()
			break
		}
	}
	var retType string
	if ret := strings.TrimSpace(string(src[paramsEnd:blockStart])); ret != "" {
		retType = ret
	}

	return retType, params
}

func extractTSFnTypes(node *sitter.Node, src []byte) (string, string) {
	params := extractParamListText(node, src, "formal_parameters")

	// Return type: type_annotation directly on the function node
	var retType string
	ta := childByType(node, "type_annotation")
	if ta != nil {
		text := strings.TrimSpace(ta.Content(src))
		text = strings.TrimPrefix(text, ":")
		retType = strings.TrimSpace(text)
	}

	return retType, params
}

func extractJSFnTypes(node *sitter.Node, src []byte) (string, string) {
	params := extractParamListText(node, src, "formal_parameters")
	return "", params
}

func extractCFnTypes(node *sitter.Node, src []byte) (string, string) {
	children := childrenSlice(node)

	declPos := -1
	for i, child := range children {
		if child.Type() == "function_declarator" {
			declPos = i
			break
		}
	}

	// Return type: everything before function_declarator
	var retType string
	if declPos > 0 {
		retEnd := children[declPos].StartByte()
		ret := strings.TrimSpace(string(src[node.StartByte():retEnd]))
		if ret != "" {
			retType = ret
		}
	}

	// Params: parameter_list inside function_declarator
	var params string
	if declPos >= 0 {
		decl := children[declPos]
		paramNode := childByType(decl, "parameter_list")
		if paramNode != nil {
			text := strings.TrimSpace(paramNode.Content(src))
			text = strings.TrimPrefix(text, "(")
			text = strings.TrimSuffix(text, ")")
			params = collapseWhitespace(strings.TrimSpace(text))
		}
	}

	return retType, params
}

// --- Language configs ---

func getConfig(language string) *languageConfig {
	switch language {
	case "go":
		return goConfig()
	case "rust":
		return rustConfig()
	case "python":
		return pythonConfig()
	case "javascript":
		return javascriptConfig()
	case "typescript":
		return typescriptConfig()
	case "tsx":
		return tsxConfig()
	case "jsx":
		return javascriptConfig()
	case "c":
		return cConfig()
	case "cpp":
		return cppConfig()
	default:
		return nil
	}
}

func goConfig() *languageConfig {
	return &languageConfig{
		name: "go",
		lang: golang.GetLanguage(),
		defQuery: `
			(function_declaration name: (identifier) @name) @def
			(method_declaration name: (field_identifier) @name) @def
			(type_declaration (type_spec name: (type_identifier) @name)) @def
		`,
		refQuery: `
			(call_expression function: (identifier) @ref_name) @ref
			(call_expression function: (selector_expression field: (field_identifier) @ref_name)) @ref
		`,
	}
}

func rustConfig() *languageConfig {
	return &languageConfig{
		name: "rust",
		lang: rust.GetLanguage(),
		defQuery: `
			(function_item name: (identifier) @name) @def
			(struct_item name: (type_identifier) @name) @def
			(enum_item name: (type_identifier) @name) @def
			(trait_item name: (type_identifier) @name) @def
			(impl_item type: (type_identifier) @name) @def
			(const_item name: (identifier) @name) @def
			(static_item name: (identifier) @name) @def
			(mod_item name: (identifier) @name) @def
		`,
		refQuery: `
			(call_expression function: (identifier) @ref_name) @ref
			(call_expression function: (field_expression field: (field_identifier) @ref_name)) @ref
			(call_expression function: (scoped_identifier name: (identifier) @ref_name)) @ref
			(macro_invocation macro: (identifier) @ref_name) @ref
		`,
	}
}

func pythonConfig() *languageConfig {
	return &languageConfig{
		name: "python",
		lang: python.GetLanguage(),
		defQuery: `
			(function_definition name: (identifier) @name) @def
			(class_definition name: (identifier) @name) @def
		`,
		refQuery: `
			(call function: (identifier) @ref_name) @ref
			(call function: (attribute attribute: (identifier) @ref_name)) @ref
		`,
	}
}

func javascriptConfig() *languageConfig {
	return &languageConfig{
		name: "javascript",
		lang: javascript.GetLanguage(),
		defQuery: `
			(function_declaration name: (identifier) @name) @def
			(class_declaration name: (identifier) @name) @def
			(method_definition name: (property_identifier) @name) @def
		`,
		refQuery: `
			(call_expression function: (identifier) @ref_name) @ref
			(call_expression function: (member_expression property: (property_identifier) @ref_name)) @ref
		`,
	}
}

func typescriptConfig() *languageConfig {
	return &languageConfig{
		name: "typescript",
		lang: ts_typescript.GetLanguage(),
		defQuery: `
			(function_declaration name: (identifier) @name) @def
			(class_declaration name: (type_identifier) @name) @def
			(method_definition name: (property_identifier) @name) @def
			(interface_declaration name: (type_identifier) @name) @def
			(enum_declaration name: (identifier) @name) @def
			(type_alias_declaration name: (type_identifier) @name) @def
		`,
		refQuery: `
			(call_expression function: (identifier) @ref_name) @ref
			(call_expression function: (member_expression property: (property_identifier) @ref_name)) @ref
		`,
	}
}

func tsxConfig() *languageConfig {
	ts := typescriptConfig()
	return &languageConfig{
		name:     "tsx",
		lang:     tsx.GetLanguage(),
		defQuery: ts.defQuery,
		refQuery: ts.refQuery,
	}
}

func cConfig() *languageConfig {
	return &languageConfig{
		name: "c",
		lang: c.GetLanguage(),
		defQuery: `
			(function_definition declarator: (function_declarator declarator: (identifier) @name)) @def
			(struct_specifier name: (type_identifier) @name) @def
			(enum_specifier name: (type_identifier) @name) @def
		`,
		refQuery: `
			(call_expression function: (identifier) @ref_name) @ref
		`,
	}
}

func cppConfig() *languageConfig {
	return &languageConfig{
		name: "cpp",
		lang: cpp.GetLanguage(),
		defQuery: `
			(function_definition declarator: (function_declarator declarator: (identifier) @name)) @def
			(function_definition declarator: (function_declarator declarator: (qualified_identifier name: (identifier) @name))) @def
			(struct_specifier name: (type_identifier) @name) @def
			(enum_specifier name: (type_identifier) @name) @def
			(class_specifier name: (type_identifier) @name) @def
			(namespace_definition name: (identifier) @name) @def
		`,
		refQuery: `
			(call_expression function: (identifier) @ref_name) @ref
			(call_expression function: (qualified_identifier name: (identifier) @ref_name)) @ref
			(call_expression function: (field_expression field: (field_identifier) @ref_name)) @ref
		`,
	}
}

// SupportedLanguages returns the list of languages for which symbol extraction is available.
func SupportedLanguages() []string {
	return []string{"go", "rust", "python", "javascript", "typescript", "tsx", "jsx", "c", "cpp"}
}
