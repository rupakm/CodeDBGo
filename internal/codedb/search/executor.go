package search

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"

	"github.com/sageox/codedbgo/internal/codedb/store"
)

// Result represents a single search result.
type Result struct {
	Repo       string  `json:"repo,omitempty"`
	FilePath   string  `json:"file_path,omitempty"`
	Content    string  `json:"content,omitempty"`
	Score      float64 `json:"score,omitempty"`
	Line       int     `json:"line,omitempty"`
	Language   string  `json:"language,omitempty"`
	CommitHash string  `json:"commit_hash,omitempty"`
	Author     string  `json:"author,omitempty"`
	Message    string  `json:"message,omitempty"`
	SymbolName string  `json:"symbol_name,omitempty"`
	SymbolKind string  `json:"symbol_kind,omitempty"`
}

// Execute runs a parsed query against the store using the planner to determine
// the execution strategy (SQL only, Bleve only, or intersect).
func Execute(s *store.Store, query *ParsedQuery) ([]Result, error) {
	plan, err := Plan(query)
	if err != nil {
		return nil, err
	}

	switch plan.Strategy {
	case JoinSQLOnly:
		return executePlanSQL(s, plan)
	case JoinBleveOnly:
		return executePlanBleve(s, plan)
	case JoinIntersect:
		return executePlanIntersect(s, plan, query)
	default:
		return executePlanSQL(s, plan)
	}
}

// executePlanSQL executes a plan that only needs SQL (commits, symbols, calls).
func executePlanSQL(s *store.Store, plan *ExecutionPlan) ([]Result, error) {
	args := make([]interface{}, len(plan.SQLParams))
	for i, p := range plan.SQLParams {
		args[i] = p
	}

	rows, err := s.DB.Query(plan.SQL, args...)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []Result
	for rows.Next() {
		values := make([]sql.NullString, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}

		r := Result{}
		for i, col := range cols {
			val := values[i].String
			switch col {
			case "path":
				r.FilePath = val
			case "name":
				r.SymbolName = val
			case "kind":
				r.SymbolKind = val
			case "line":
				fmt.Sscanf(val, "%d", &r.Line)
			case "hash":
				r.CommitHash = val
			case "author":
				r.Author = val
			case "message":
				r.Message = val
			case "score":
				fmt.Sscanf(val, "%f", &r.Score)
			case "snippet":
				r.Content = val
			}
		}
		results = append(results, r)
	}

	return results, nil
}

// executePlanBleve executes a plan that only needs Bleve full-text search.
func executePlanBleve(s *store.Store, plan *ExecutionPlan) ([]Result, error) {
	idx := s.CodeIndex
	if plan.BleveIndex == "diff" {
		idx = s.DiffIndex
	}

	bleveQuery := bleve.NewQueryStringQuery(plan.BleveQuery)
	searchReq := bleve.NewSearchRequestOptions(bleveQuery, plan.Limit*5, 0, false)
	searchReq.Fields = []string{"content"}
	searchReq.Highlight = bleve.NewHighlightWithStyle("ansi")
	searchResult, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("bleve search: %w", err)
	}

	if searchResult.Total == 0 {
		return nil, nil
	}

	var results []Result
	for _, hit := range searchResult.Hits {
		fragment := ""
		if frags, ok := hit.Fragments["content"]; ok && len(frags) > 0 {
			fragment = frags[0]
		}

		if plan.BleveIndex == "diff" {
			diffID := strings.TrimPrefix(hit.ID, "diff_")
			rows, err := s.DB.Query(`
				SELECT substr(c.hash, 1, 10), c.author, substr(c.message, 1, 80), d.path
				FROM diffs d JOIN commits c ON c.id = d.commit_id
				WHERE d.id = ?`, diffID)
			if err != nil {
				continue
			}
			for rows.Next() {
				var hash, author, message, path string
				if err := rows.Scan(&hash, &author, &message, &path); err != nil {
					continue
				}
				results = append(results, Result{
					CommitHash: hash, Author: author, Message: message,
					FilePath: path, Score: hit.Score, Content: fragment,
				})
			}
			rows.Close()
		} else {
			blobID := strings.TrimPrefix(hit.ID, "blob_")
			rows, err := s.DB.Query(`
				SELECT fr.path, b.language, rp.name
				FROM blobs b
				JOIN file_revs fr ON fr.blob_id = b.id
				JOIN refs r ON r.commit_id = fr.commit_id
				JOIN repos rp ON rp.id = r.repo_id
				WHERE b.id = ? AND r.name = ?`, blobID, "refs/heads/main")
			if err != nil {
				continue
			}
			for rows.Next() {
				var path string
				var lang, repo sql.NullString
				if err := rows.Scan(&path, &lang, &repo); err != nil {
					continue
				}
				results = append(results, Result{
					FilePath: path, Score: hit.Score, Content: fragment,
					Language: lang.String, Repo: repo.String,
				})
			}
			rows.Close()
		}

		if len(results) >= plan.Limit {
			results = results[:plan.Limit]
			break
		}
	}

	return results, nil
}

// executePlanIntersect runs both Bleve and SQL, intersecting results.
func executePlanIntersect(s *store.Store, plan *ExecutionPlan, query *ParsedQuery) ([]Result, error) {
	idx := s.CodeIndex
	if plan.BleveIndex == "diff" {
		idx = s.DiffIndex
	}

	// Phase 1: Bleve search
	bleveQuery := bleve.NewQueryStringQuery(plan.BleveQuery)
	searchReq := bleve.NewSearchRequestOptions(bleveQuery, plan.Limit*5, 0, false)
	searchReq.Fields = []string{"content"}
	searchReq.Highlight = bleve.NewHighlightWithStyle("ansi")
	searchResult, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("bleve search: %w", err)
	}

	if searchResult.Total == 0 {
		return nil, nil
	}

	// Phase 2: For each Bleve hit, check metadata filters in SQL
	rev := query.Filters.Rev
	if rev == "" {
		rev = "main"
	}
	revRef := rev
	if !strings.HasPrefix(rev, "refs/") {
		revRef = "refs/heads/" + rev
	}

	var results []Result
	for _, hit := range searchResult.Hits {
		fragment := ""
		if frags, ok := hit.Fragments["content"]; ok && len(frags) > 0 {
			fragment = frags[0]
		}

		if plan.BleveIndex == "diff" {
			diffID := strings.TrimPrefix(hit.ID, "diff_")
			sqlQ := `
				SELECT substr(c.hash, 1, 10), c.author, substr(c.message, 1, 80), d.path
				FROM diffs d JOIN commits c ON c.id = d.commit_id
				WHERE d.id = ?`
			args := []interface{}{diffID}

			if query.Filters.Repo != "" {
				sqlQ += " AND c.repo_id IN (SELECT id FROM repos WHERE name LIKE ?)"
				args = append(args, "%"+query.Filters.Repo+"%")
			}
			if query.Filters.NegRepo != "" {
				sqlQ += " AND c.repo_id NOT IN (SELECT id FROM repos WHERE name LIKE ?)"
				args = append(args, "%"+query.Filters.NegRepo+"%")
			}
			if query.Filters.File != "" {
				if strings.ContainsAny(query.Filters.File, "*?") {
					sqlQ += " AND d.path GLOB ?"
					args = append(args, query.Filters.File)
				} else {
					sqlQ += " AND d.path LIKE ?"
					args = append(args, "%"+query.Filters.File+"%")
				}
			}
			if query.Filters.NegFile != "" {
				sqlQ += " AND d.path NOT LIKE ?"
				args = append(args, "%"+query.Filters.NegFile+"%")
			}
			if query.Filters.Author != "" {
				sqlQ += " AND c.author LIKE ?"
				args = append(args, "%"+query.Filters.Author+"%")
			}
			if query.Filters.NegAuthor != "" {
				sqlQ += " AND c.author NOT LIKE ?"
				args = append(args, "%"+query.Filters.NegAuthor+"%")
			}
			if query.Filters.Before != "" {
				sqlQ += " AND c.timestamp < CAST(strftime('%s', ?) AS INTEGER)"
				args = append(args, query.Filters.Before)
			}
			if query.Filters.After != "" {
				sqlQ += " AND c.timestamp > CAST(strftime('%s', ?) AS INTEGER)"
				args = append(args, query.Filters.After)
			}

			rows, err := s.DB.Query(sqlQ, args...)
			if err != nil {
				continue
			}
			for rows.Next() {
				var hash, author, message, path string
				if err := rows.Scan(&hash, &author, &message, &path); err != nil {
					continue
				}
				results = append(results, Result{
					CommitHash: hash, Author: author, Message: message,
					FilePath: path, Score: hit.Score, Content: fragment,
				})
			}
			rows.Close()
		} else {
			blobID := strings.TrimPrefix(hit.ID, "blob_")
			sqlQ := `
				SELECT fr.path, b.language, rp.name
				FROM blobs b
				JOIN file_revs fr ON fr.blob_id = b.id
				JOIN refs r ON r.commit_id = fr.commit_id
				JOIN repos rp ON rp.id = r.repo_id
				WHERE b.id = ? AND r.name = ?`
			args := []interface{}{blobID, revRef}

			if query.Filters.Repo != "" {
				sqlQ += " AND rp.name LIKE ?"
				args = append(args, "%"+query.Filters.Repo+"%")
			}
			if query.Filters.NegRepo != "" {
				sqlQ += " AND rp.name NOT LIKE ?"
				args = append(args, "%"+query.Filters.NegRepo+"%")
			}
			if query.Filters.File != "" {
				if strings.ContainsAny(query.Filters.File, "*?") {
					sqlQ += " AND fr.path GLOB ?"
					args = append(args, query.Filters.File)
				} else {
					sqlQ += " AND fr.path LIKE ?"
					args = append(args, "%"+query.Filters.File+"%")
				}
			}
			if query.Filters.NegFile != "" {
				sqlQ += " AND fr.path NOT LIKE ?"
				args = append(args, "%"+query.Filters.NegFile+"%")
			}
			if query.Filters.Lang != "" {
				sqlQ += " AND b.language = ?"
				args = append(args, query.Filters.Lang)
			}
			if query.Filters.NegLang != "" {
				sqlQ += " AND b.language != ?"
				args = append(args, query.Filters.NegLang)
			}

			rows, err := s.DB.Query(sqlQ, args...)
			if err != nil {
				continue
			}
			for rows.Next() {
				var path string
				var lang, repo sql.NullString
				if err := rows.Scan(&path, &lang, &repo); err != nil {
					continue
				}
				results = append(results, Result{
					FilePath: path, Score: hit.Score, Content: fragment,
					Language: lang.String, Repo: repo.String,
				})
			}
			rows.Close()
		}

		if len(results) >= plan.Limit {
			results = results[:plan.Limit]
			break
		}
	}

	return results, nil
}
