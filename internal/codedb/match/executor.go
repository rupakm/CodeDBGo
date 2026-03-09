package match

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/sageox/codedbgo/internal/codedb/match/engine"
	"github.com/sageox/codedbgo/internal/codedb/match/mapper"
	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
	"github.com/sageox/codedbgo/internal/codedb/store"
)

// MatchOptions holds options for a match query.
type MatchOptions struct {
	Pattern       string
	Lang          string
	Repo          string
	File          string
	NotPats       []string
	InsidePats    []string
	NotInsidePats []string
	WhereClauses  []string
	MaxResults    int
	JSONOutput    bool
}

// MatchResult re-exports engine.MatchResult for convenience.
type MatchResult = engine.MatchResult

// Execute runs a structural pattern match against the store.
func Execute(ctx context.Context, s *store.Store, opts MatchOptions) ([]MatchResult, error) {
	// 1. Parse the main pattern
	pat, err := pattern.Parse(opts.Pattern)
	if err != nil {
		return nil, fmt.Errorf("parse pattern: %w", err)
	}

	// 2. Build the formula tree
	formula, constraints, err := buildFormula(pat, opts)
	if err != nil {
		return nil, err
	}

	// 3. Extract literal hints for SQL pre-filter
	hints := extractHints(pat)

	// 4. Query candidate blobs
	candidates, err := queryCandidates(ctx, s, opts, hints)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}

	// 5. For each candidate, load source, parse AST, run matcher
	var allResults []MatchResult
	repoCache := make(map[string]*git.Repository)

	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}

		src, err := readBlobContent(s, c, repoCache)
		if err != nil || src == "" {
			continue
		}

		lang, tsLang, tokenSourceFactory := getLangConfig(opts.Lang)
		if tsLang == nil {
			continue
		}

		parser := gotreesitter.NewParser(tsLang)
		srcBytes := []byte(src)
		var tree *gotreesitter.Tree
		if tokenSourceFactory != nil {
			tree, err = parser.ParseWithTokenSource(srcBytes, tokenSourceFactory(srcBytes, tsLang))
		} else {
			tree, err = parser.Parse(srcBytes)
		}
		if err != nil || tree == nil {
			continue
		}

		root := tree.RootNode()
		// Check for degenerate parse tree
		if root.ChildCount() < 3 && len(srcBytes) > 200 {
			tree.Release()
			continue
		}

		m := newMapper(lang, tsLang, srcBytes)
		if m == nil {
			tree.Release()
			continue
		}

		var results []MatchResult
		if formula != nil {
			results = engine.ApplyFormula(formula, root, m)
		} else {
			results = engine.Match(pat, root, m)
		}
		tree.Release()

		// Apply constraints
		if len(constraints) > 0 {
			results = engine.ApplyConstraints(results, constraints)
		}

		// Set file path on results
		for i := range results {
			results[i].File = c.path
		}

		allResults = append(allResults, results...)

		if opts.MaxResults > 0 && len(allResults) >= opts.MaxResults {
			allResults = allResults[:opts.MaxResults]
			break
		}
	}

	return allResults, nil
}

// candidate represents a blob to match against.
type candidate struct {
	blobID      int64
	contentHash string
	path        string
	repoPath    string
}

// queryCandidates builds and executes SQL to find candidate blobs.
func queryCandidates(ctx context.Context, s *store.Store, opts MatchOptions, hints []string) ([]candidate, error) {
	query := `SELECT DISTINCT b.id, b.content_hash, fr.path, r.path
		FROM blobs b
		JOIN file_revs fr ON fr.blob_id = b.id
		JOIN commits c ON c.id = fr.commit_id
		JOIN refs ref ON ref.commit_id = c.id
		JOIN repos r ON r.id = c.repo_id
		WHERE b.language = ?`
	args := []interface{}{opts.Lang}

	if opts.Repo != "" {
		query += " AND r.name LIKE ?"
		args = append(args, "%"+opts.Repo+"%")
	}
	if opts.File != "" {
		query += " AND fr.path LIKE ?"
		args = append(args, "%"+opts.File+"%")
	}

	// Use literal hints to pre-filter via symbol_refs or Bleve
	// For now, just use language filter. Can add Bleve pre-filter later.
	_ = hints

	if opts.MaxResults > 0 {
		// Fetch more candidates than needed to allow for filtering
		query += fmt.Sprintf(" LIMIT %d", opts.MaxResults*10)
	} else {
		query += " LIMIT 10000"
	}

	rows, err := s.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.blobID, &c.contentHash, &c.path, &c.repoPath); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// readBlobContent reads the source text of a blob from the git repo.
func readBlobContent(s *store.Store, c candidate, repoCache map[string]*git.Repository) (string, error) {
	// repos.path is stored as an absolute path by the indexer
	fullPath := c.repoPath
	repo, ok := repoCache[fullPath]
	if !ok {
		var err error
		repo, err = git.PlainOpen(fullPath)
		if err != nil {
			return "", err
		}
		repoCache[fullPath] = repo
	}

	oid := plumbing.NewHash(c.contentHash)
	blob, err := repo.BlobObject(oid)
	if err != nil {
		return "", err
	}
	reader, err := blob.Reader()
	if err != nil {
		return "", err
	}
	content, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !utf8.Valid(content) {
		return "", fmt.Errorf("invalid content")
	}
	return string(content), nil
}

// extractHints extracts literal strings from the pattern for SQL pre-filtering.
func extractHints(pat pattern.PatternNode) []string {
	var hints []string
	switch p := pat.(type) {
	case *pattern.Literal:
		if len(p.Value) > 2 { // skip short identifiers
			hints = append(hints, p.Value)
		}
	case *pattern.Call:
		hints = append(hints, extractHints(p.Func)...)
	case *pattern.MethodCall:
		hints = append(hints, extractHints(p.Method)...)
	}
	return hints
}

// buildFormula constructs the full formula from options.
func buildFormula(basePat pattern.PatternNode, opts MatchOptions) (pattern.MatchFormula, []pattern.MetavarConstraint, error) {
	hasFormulas := len(opts.NotPats) > 0 || len(opts.InsidePats) > 0 || len(opts.NotInsidePats) > 0
	hasConstraints := len(opts.WhereClauses) > 0

	if !hasFormulas && !hasConstraints {
		return nil, nil, nil
	}

	var formulas []pattern.MatchFormula
	formulas = append(formulas, &pattern.BasePattern{Lang: opts.Lang, Pattern: basePat})

	for _, notStr := range opts.NotPats {
		notPat, err := pattern.Parse(notStr)
		if err != nil {
			return nil, nil, fmt.Errorf("parse --not pattern %q: %w", notStr, err)
		}
		formulas = append(formulas, &pattern.Not{
			Formula: &pattern.BasePattern{Lang: opts.Lang, Pattern: notPat},
		})
	}

	for _, insStr := range opts.InsidePats {
		insPat, err := pattern.Parse(insStr)
		if err != nil {
			return nil, nil, fmt.Errorf("parse --inside pattern %q: %w", insStr, err)
		}
		formulas = append(formulas, &pattern.Inside{
			Formula: &pattern.BasePattern{Lang: opts.Lang, Pattern: insPat},
		})
	}

	for _, niStr := range opts.NotInsidePats {
		niPat, err := pattern.Parse(niStr)
		if err != nil {
			return nil, nil, fmt.Errorf("parse --not-inside pattern %q: %w", niStr, err)
		}
		formulas = append(formulas, &pattern.NotInside{
			Formula: &pattern.BasePattern{Lang: opts.Lang, Pattern: niPat},
		})
	}

	var constraints []pattern.MetavarConstraint
	for _, w := range opts.WhereClauses {
		c, err := parseWhereClause(w)
		if err != nil {
			return nil, nil, err
		}
		constraints = append(constraints, c)
	}

	formula := &pattern.And{Formulas: formulas}
	return formula, constraints, nil
}

// parseWhereClause parses a string like "$F ~ /^unsafe_/" into a MetavarConstraint.
func parseWhereClause(clause string) (pattern.MetavarConstraint, error) {
	ops := []string{"!~", "~", "!=", "==", "<=", ">=", "<", ">"}
	for _, op := range ops {
		idx := strings.Index(clause, " "+op+" ")
		if idx >= 0 {
			metavar := strings.TrimSpace(clause[:idx])
			value := strings.TrimSpace(clause[idx+len(op)+2:])
			// Strip leading $ from metavar name
			metavar = strings.TrimPrefix(metavar, "$")
			return pattern.MetavarConstraint{
				Metavar: metavar,
				Op:      op,
				Value:   value,
			}, nil
		}
	}
	return pattern.MetavarConstraint{}, fmt.Errorf("invalid where clause: %q", clause)
}

type tokenSourceFactory func([]byte, *gotreesitter.Language) gotreesitter.TokenSource

// getLangConfig returns language name, tree-sitter language, and optional token source factory.
func getLangConfig(lang string) (string, *gotreesitter.Language, tokenSourceFactory) {
	switch lang {
	case "go":
		l := grammars.GoLanguage()
		return "go", l, func(src []byte, lang *gotreesitter.Language) gotreesitter.TokenSource {
			return grammars.NewGoTokenSourceOrEOF(src, lang)
		}
	case "python":
		return "python", grammars.PythonLanguage(), nil
	case "javascript":
		l := grammars.JavascriptLanguage()
		return "javascript", l, func(src []byte, lang *gotreesitter.Language) gotreesitter.TokenSource {
			return grammars.NewGenericTokenSourceOrEOF(src, lang)
		}
	case "typescript":
		// Use JavaScript grammar as fallback due to TypeScript DFA issues
		l := grammars.JavascriptLanguage()
		return "typescript", l, func(src []byte, lang *gotreesitter.Language) gotreesitter.TokenSource {
			return grammars.NewGenericTokenSourceOrEOF(src, lang)
		}
	default:
		return "", nil, nil
	}
}

// newMapper creates the appropriate LangMapper for the given language.
func newMapper(lang string, tsLang *gotreesitter.Language, src []byte) mapper.LangMapper {
	switch lang {
	case "go":
		return mapper.NewGoMapper(tsLang, src)
	case "python":
		return mapper.NewPythonMapper(tsLang, src)
	case "javascript", "typescript":
		return mapper.NewTypeScriptMapper(tsLang, src)
	default:
		return nil
	}
}
