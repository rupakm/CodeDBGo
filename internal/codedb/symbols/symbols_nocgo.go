//go:build !cgo

package symbols

// Extract is a no-op stub when CGO is disabled (tree-sitter requires CGO).
func Extract(source, language string) ([]Symbol, []Ref) {
	return nil, nil
}

// SupportedLanguages returns nil when CGO is disabled.
func SupportedLanguages() []string {
	return nil
}
