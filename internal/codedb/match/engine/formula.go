package engine

import (
	"github.com/odvcencio/gotreesitter"
	"github.com/sageox/codedbgo/internal/codedb/match/mapper"
	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
)

// ApplyFormula evaluates a MatchFormula against an AST and returns matching results.
func ApplyFormula(formula pattern.MatchFormula, root *gotreesitter.Node, m mapper.LangMapper) []MatchResult {
	switch f := formula.(type) {
	case *pattern.BasePattern:
		return Match(f.Pattern, root, m)

	case *pattern.And:
		if len(f.Formulas) == 0 {
			return nil
		}
		results := ApplyFormula(f.Formulas[0], root, m)
		for _, sub := range f.Formulas[1:] {
			results = applySubFormula(sub, results, root, m)
			if len(results) == 0 {
				return nil
			}
		}
		return results

	case *pattern.Or:
		var results []MatchResult
		seen := make(map[string]bool)
		for _, sub := range f.Formulas {
			for _, r := range ApplyFormula(sub, root, m) {
				key := resultKey(r)
				if !seen[key] {
					seen[key] = true
					results = append(results, r)
				}
			}
		}
		return results

	case *pattern.Not:
		// Not at top level makes no sense; it's applied as a filter in And
		return nil

	case *pattern.Inside:
		// Inside at top level makes no sense; it's applied as a filter in And
		return nil

	case *pattern.NotInside:
		return nil

	default:
		return nil
	}
}

// applySubFormula filters results based on a sub-formula within an And.
func applySubFormula(sub pattern.MatchFormula, results []MatchResult, root *gotreesitter.Node, m mapper.LangMapper) []MatchResult {
	switch f := sub.(type) {
	case *pattern.Not:
		excludes := ApplyFormula(f.Formula, root, m)
		return subtractResults(results, excludes)

	case *pattern.Inside:
		containers := ApplyFormula(f.Formula, root, m)
		return filterInside(results, containers)

	case *pattern.NotInside:
		containers := ApplyFormula(f.Formula, root, m)
		return filterNotInside(results, containers)

	case *pattern.MetavarConstraint:
		return ApplyConstraints(results, []pattern.MetavarConstraint{*f})

	default:
		// For BasePattern, And, Or — intersect
		subResults := ApplyFormula(sub, root, m)
		return intersectResults(results, subResults)
	}
}

// subtractResults removes results that overlap with excludes.
func subtractResults(results, excludes []MatchResult) []MatchResult {
	excludeKeys := make(map[string]bool)
	for _, e := range excludes {
		excludeKeys[resultKey(e)] = true
	}
	var filtered []MatchResult
	for _, r := range results {
		if !excludeKeys[resultKey(r)] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// filterInside keeps only results whose position is inside one of the containers.
func filterInside(results, containers []MatchResult) []MatchResult {
	var filtered []MatchResult
	for _, r := range results {
		for _, c := range containers {
			if isInsideResult(r, c) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
}

// filterNotInside keeps only results whose position is NOT inside any container.
func filterNotInside(results, containers []MatchResult) []MatchResult {
	var filtered []MatchResult
	for _, r := range results {
		inside := false
		for _, c := range containers {
			if isInsideResult(r, c) {
				inside = true
				break
			}
		}
		if !inside {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// isInsideResult checks if result r is positionally within container c.
func isInsideResult(r, c MatchResult) bool {
	if r.Line < c.Line || r.EndLine > c.EndLine {
		return false
	}
	if r.Line == c.Line && r.Col < c.Col {
		return false
	}
	if r.EndLine == c.EndLine && r.EndCol > c.EndCol {
		return false
	}
	return true
}

// intersectResults keeps results that have a matching position in subResults.
func intersectResults(results, subResults []MatchResult) []MatchResult {
	subKeys := make(map[string]bool)
	for _, s := range subResults {
		subKeys[resultKey(s)] = true
	}
	var filtered []MatchResult
	for _, r := range results {
		if subKeys[resultKey(r)] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// resultKey creates a unique key for a match result based on position.
func resultKey(r MatchResult) string {
	return r.File + ":" + itoa(r.Line) + ":" + itoa(r.Col) + "-" + itoa(r.EndLine) + ":" + itoa(r.EndCol)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
