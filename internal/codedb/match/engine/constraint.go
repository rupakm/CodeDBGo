package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
)

// ApplyConstraints filters matches by metavariable constraints.
func ApplyConstraints(matches []MatchResult, constraints []pattern.MetavarConstraint) []MatchResult {
	var result []MatchResult
	for _, m := range matches {
		if satisfiesAll(m, constraints) {
			result = append(result, m)
		}
	}
	return result
}

func satisfiesAll(m MatchResult, constraints []pattern.MetavarConstraint) bool {
	for _, c := range constraints {
		if !satisfies(m, c) {
			return false
		}
	}
	return true
}

func satisfies(m MatchResult, c pattern.MetavarConstraint) bool {
	val, ok := m.Bindings[c.Metavar]
	if !ok {
		return false
	}

	switch c.Op {
	case "~":
		return matchRegex(c.Value, val)
	case "!~":
		return !matchRegex(c.Value, val)
	case "==":
		return val == c.Value
	case "!=":
		return val != c.Value
	case "<":
		return compareNum(val, c.Value) < 0
	case ">":
		return compareNum(val, c.Value) > 0
	case "<=":
		return compareNum(val, c.Value) <= 0
	case ">=":
		return compareNum(val, c.Value) >= 0
	default:
		return false
	}
}

// matchRegex matches val against a regex pattern like "/pattern/" or "/pattern/flags".
func matchRegex(regexVal, val string) bool {
	// Strip surrounding slashes: /pattern/ → pattern
	pat := regexVal
	if strings.HasPrefix(pat, "/") {
		pat = pat[1:]
		if idx := strings.LastIndex(pat, "/"); idx >= 0 {
			pat = pat[:idx]
		}
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return false
	}
	return re.MatchString(val)
}

func compareNum(a, b string) int {
	na, errA := strconv.ParseFloat(a, 64)
	nb, errB := strconv.ParseFloat(b, 64)
	if errA != nil || errB != nil {
		return strings.Compare(a, b)
	}
	if na < nb {
		return -1
	}
	if na > nb {
		return 1
	}
	return 0
}
