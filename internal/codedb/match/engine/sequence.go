package engine

import (
	"github.com/odvcencio/gotreesitter"
	"github.com/sageox/codedbgo/internal/codedb/match/mapper"
	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
)

// matchSequence matches a slice of pattern nodes against a slice of AST nodes,
// handling Ellipsis (skip 0..N nodes) and EllipsisMetavar.
func matchSequence(pats []pattern.PatternNode, nodes []*gotreesitter.Node, m mapper.LangMapper, bindings map[string]string) bool {
	return matchSeqHelper(pats, 0, nodes, 0, m, bindings)
}

func matchSeqHelper(pats []pattern.PatternNode, pi int, nodes []*gotreesitter.Node, ni int, m mapper.LangMapper, bindings map[string]string) bool {
	// Base case: all patterns consumed
	if pi >= len(pats) {
		return ni >= len(nodes)
	}

	pat := pats[pi]

	// Handle Ellipsis: skip 0..N nodes
	if _, ok := pat.(*pattern.Ellipsis); ok {
		// Try consuming 0, 1, 2, ... nodes
		for skip := 0; skip <= len(nodes)-ni; skip++ {
			saved := copyBindings(bindings)
			if matchSeqHelper(pats, pi+1, nodes, ni+skip, m, bindings) {
				return true
			}
			// Restore bindings on failure
			restoreBindings(bindings, saved)
		}
		return false
	}

	// Handle EllipsisMetavar: skip 0..N nodes and capture
	if em, ok := pat.(*pattern.EllipsisMetavar); ok {
		for skip := 0; skip <= len(nodes)-ni; skip++ {
			saved := copyBindings(bindings)
			// Capture the text of skipped nodes
			capturedText := ""
			for i := 0; i < skip; i++ {
				if i > 0 {
					capturedText += ", "
				}
				capturedText += m.NodeText(nodes[ni+i])
			}
			bindings[em.Name] = capturedText
			if matchSeqHelper(pats, pi+1, nodes, ni+skip, m, bindings) {
				return true
			}
			restoreBindings(bindings, saved)
		}
		return false
	}

	// Regular pattern: must match current node
	if ni >= len(nodes) {
		return false
	}

	saved := copyBindings(bindings)
	if matchNode(pat, nodes[ni], m, bindings) {
		if matchSeqHelper(pats, pi+1, nodes, ni+1, m, bindings) {
			return true
		}
	}
	restoreBindings(bindings, saved)
	return false
}

func restoreBindings(dst, src map[string]string) {
	for k := range dst {
		if _, ok := src[k]; !ok {
			delete(dst, k)
		}
	}
	for k, v := range src {
		dst[k] = v
	}
}
