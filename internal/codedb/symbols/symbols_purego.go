//go:build ignore
// +build ignore

// symbols_purego.go is a PROOF OF CONCEPT showing what the symbols package
// would look like after migrating from smacker/go-tree-sitter (CGO) to
// odvcencio/gotreesitter (pure Go).
//
// This file is not compiled (build tag: ignore). It demonstrates the API
// mapping so the migration effort can be evaluated concretely.
//
// To complete the migration:
//   1. Replace go.mod dependency
//   2. Delete symbols_cgo.go and symbols_nocgo.go
//   3. Remove the "ignore" build tag from this file
//   4. Run tests: go test ./internal/codedb/symbols/...

package symbols

import (
	"sort"
	"strings"
	"unicode"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// languageConfig holds tree-sitter query patterns for a language.
type languageConfig struct {
	name     string
	lang     *gotreesitter.Language
	defQuery string
	refQuery string
}

// Extract extracts symbol definitions and references from source code
// for the given language using tree-sitter (pure Go implementation).
func Extract(source, language string) ([]Symbol, []Ref) {
	config := getConfig(language)
	if config == nil {
		return nil, nil
	}

	parser := gotreesitter.NewParser(config.lang)
	tree, err := parser.Parse([]byte(source))
	if err != nil || tree == nil {
		return nil, nil
	}
	defer tree.Release()

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

func extractDefs(root *gotreesitter.Node, src []byte, config *languageConfig) []Symbol {
	q, err := gotreesitter.NewQuery(config.defQuery, config.lang)
	if err != nil {
		return nil
	}

	nameIdx := captureIndex(q, "name")
	defIdx := captureIndex(q, "def")

	// Query execution: gotreesitter combines cursor creation and exec
	cursor := q.Exec(root, config.lang, src)

	var symbols []Symbol
	for {
		m, ok := cursor.NextMatch()
		if !ok {
			break
		}
		// Note: gotreesitter handles predicates internally during matching,
		// so there is no separate FilterPredicates call needed.

		var nameText string
		var defNode *gotreesitter.Node
		for _, cap := range m.Captures {
			if nameIdx >= 0 && cap.Index == uint32(nameIdx) {
				nameText = cap.Node.Text(src) // .Content(src) → .Text(src)
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

		sig, retType, params := extractTypeInfo(defNode, src, config)

		symbols = append(symbols, Symbol{
			Name:       nameText,
			Kind:       normalizeKind(defNode.Type(config.lang)), // .Type() → .Type(lang)
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

func extractRefs(root *gotreesitter.Node, src []byte, config *languageConfig, symbols []Symbol) []Ref {
	q, err := gotreesitter.NewQuery(config.refQuery, config.lang)
	if err != nil {
		return nil
	}

	refNameIdx := captureIndex(q, "ref_name")
	refIdx := captureIndex(q, "ref")

	cursor := q.Exec(root, config.lang, src)

	var refs []Ref
	for {
		m, ok := cursor.NextMatch()
		if !ok {
			break
		}

		var rname string
		var refNode *gotreesitter.Node
		for _, cap := range m.Captures {
			if refNameIdx >= 0 && cap.Index == uint32(refNameIdx) {
				rname = cap.Node.Text(src)
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

func captureIndex(q *gotreesitter.Query, name string) int {
	for i, n := range q.CaptureNames() {
		if n == name {
			return i
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
	case "function_definition":
		return "function"
	case "class_definition":
		return "class"
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
	case "method_declaration":
		return "method"
	case "type_declaration", "type_spec":
		return "type"
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
// Note: node.Type() requires lang parameter in gotreesitter.
func extractTypeInfo(node *gotreesitter.Node, src []byte, config *languageConfig) (string, string, string) {
	sig := extractSignature(node, src, config.lang)

	if !isFunctionLike(node.Type(config.lang)) {
		return sig, "", ""
	}

	var retType, params string
	switch config.name {
	case "rust", "python":
		retType, params = extractArrowFnTypes(node, src, config.lang)
	case "go":
		retType, params = extractGoFnTypes(node, src, config.lang)
	case "typescript", "tsx":
		retType, params = extractTSFnTypes(node, src, config.lang)
	case "javascript":
		_, params = extractJSFnTypes(node, src, config.lang)
	case "c", "cpp":
		retType, params = extractCFnTypes(node, src, config.lang)
	}

	return sig, retType, params
}

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

var bodyKinds = map[string]bool{
	"block": true, "statement_block": true, "compound_statement": true,
	"field_declaration_list": true, "declaration_list": true,
	"class_body": true, "interface_body": true, "enum_variant_list": true,
}

func findBodyStart(node *gotreesitter.Node, lang *gotreesitter.Language) (uint32, bool) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if bodyKinds[child.Type(lang)] {
			return child.StartByte(), true
		}
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		for j := 0; j < child.ChildCount(); j++ {
			grandchild := child.Child(j)
			if bodyKinds[grandchild.Type(lang)] {
				return grandchild.StartByte(), true
			}
		}
	}
	return 0, false
}

func extractSignature(node *gotreesitter.Node, src []byte, lang *gotreesitter.Language) string {
	bodyStart, found := findBodyStart(node, lang)
	sigEnd := node.EndByte()
	if found {
		sigEnd = bodyStart
	} else {
		text := string(src[node.StartByte():node.EndByte()])
		if idx := strings.Index(text, "{"); idx >= 0 {
			sigEnd = node.StartByte() + uint32(idx)
		}
	}
	sig := string(src[node.StartByte():sigEnd])
	return collapseWhitespace(sig)
}

func childByType(node *gotreesitter.Node, kind string, lang *gotreesitter.Language) *gotreesitter.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == kind {
			return child
		}
	}
	return nil
}

func childrenSlice(node *gotreesitter.Node) []*gotreesitter.Node {
	n := node.ChildCount()
	children := make([]*gotreesitter.Node, n)
	for i := 0; i < n; i++ {
		children[i] = node.Child(i)
	}
	return children
}

func extractParamListText(node *gotreesitter.Node, src []byte, paramListKind string, lang *gotreesitter.Language) string {
	paramNode := childByType(node, paramListKind, lang)
	if paramNode == nil {
		return ""
	}
	text := strings.TrimSpace(paramNode.Text(src))
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	return collapseWhitespace(strings.TrimSpace(text))
}

func findReturnTypeAfterArrow(node *gotreesitter.Node, src []byte, lang *gotreesitter.Language) string {
	sawArrow := false
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "->" {
			sawArrow = true
			continue
		}
		if sawArrow && child.IsNamed() {
			return child.Text(src)
		}
	}
	return ""
}

func extractArrowFnTypes(node *gotreesitter.Node, src []byte, lang *gotreesitter.Language) (string, string) {
	retType := findReturnTypeAfterArrow(node, src, lang)
	params := extractParamListText(node, src, "parameters", lang)
	return retType, params
}

func extractGoFnTypes(node *gotreesitter.Node, src []byte, lang *gotreesitter.Language) (string, string) {
	children := childrenSlice(node)

	namePos := -1
	for i, child := range children {
		if child.Type(lang) == "identifier" || child.Type(lang) == "field_identifier" {
			namePos = i
			break
		}
	}
	if namePos < 0 {
		return "", ""
	}

	var paramsNode *gotreesitter.Node
	for _, child := range children[namePos+1:] {
		if child.Type(lang) == "parameter_list" {
			paramsNode = child
			break
		}
	}
	if paramsNode == nil {
		return "", ""
	}

	text := strings.TrimSpace(paramsNode.Text(src))
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	params := collapseWhitespace(strings.TrimSpace(text))

	paramsEnd := paramsNode.EndByte()
	blockStart := node.EndByte()
	for _, child := range children {
		if child.Type(lang) == "block" {
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

func extractTSFnTypes(node *gotreesitter.Node, src []byte, lang *gotreesitter.Language) (string, string) {
	params := extractParamListText(node, src, "formal_parameters", lang)

	var retType string
	ta := childByType(node, "type_annotation", lang)
	if ta != nil {
		text := strings.TrimSpace(ta.Text(src))
		text = strings.TrimPrefix(text, ":")
		retType = strings.TrimSpace(text)
	}

	return retType, params
}

func extractJSFnTypes(node *gotreesitter.Node, src []byte, lang *gotreesitter.Language) (string, string) {
	params := extractParamListText(node, src, "formal_parameters", lang)
	return "", params
}

func extractCFnTypes(node *gotreesitter.Node, src []byte, lang *gotreesitter.Language) (string, string) {
	children := childrenSlice(node)

	declPos := -1
	for i, child := range children {
		if child.Type(lang) == "function_declarator" {
			declPos = i
			break
		}
	}

	var retType string
	if declPos > 0 {
		retEnd := children[declPos].StartByte()
		ret := strings.TrimSpace(string(src[node.StartByte():retEnd]))
		if ret != "" {
			retType = ret
		}
	}

	var params string
	if declPos >= 0 {
		decl := children[declPos]
		paramNode := childByType(decl, "parameter_list", lang)
		if paramNode != nil {
			text := strings.TrimSpace(paramNode.Text(src))
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
		lang: grammars.GoLanguage(),
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
		lang: grammars.RustLanguage(),
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
		lang: grammars.PythonLanguage(),
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
		lang: grammars.JavascriptLanguage(),
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
		lang: grammars.TypescriptLanguage(),
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
		lang:     grammars.TsxLanguage(),
		defQuery: ts.defQuery,
		refQuery: ts.refQuery,
	}
}

func cConfig() *languageConfig {
	return &languageConfig{
		name: "c",
		lang: grammars.CLanguage(),
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
		lang: grammars.CppLanguage(),
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
