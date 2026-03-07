package index

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/sageox/codedbgo/internal/codedb/language"
	"github.com/sageox/codedbgo/internal/codedb/store"
	"github.com/sageox/codedbgo/internal/codedb/symbols"
)

// ProgressFunc is called with status messages during indexing.
type ProgressFunc func(msg string)

// IndexOptions configures the indexing process.
type IndexOptions struct {
	MaxHistoryDepth int // 0 = unlimited
	Progress        ProgressFunc
}

// BleveCodeDoc is the document indexed into the code Bleve index.
type BleveCodeDoc struct {
	Content string `json:"content"`
}

// BleveDiffDoc is the document indexed into the diff Bleve index.
type BleveDiffDoc struct {
	Content string `json:"content"`
}

// IndexRepo indexes a git repository into the store.
func IndexRepo(ctx context.Context, s *store.Store, url string, opts IndexOptions) error {
	report := func(msg string) {
		if opts.Progress != nil {
			opts.Progress(msg)
		}
	}

	// 1. Clone or fetch
	report("Cloning/fetching repository...")
	dirName, err := RepoDirFromURL(url)
	if err != nil {
		return err
	}
	repoPath := filepath.Join(s.ReposDir(), dirName)
	repo, err := CloneOrFetch(url, repoPath)
	if err != nil {
		return fmt.Errorf("clone/fetch %s: %w", url, err)
	}

	// 2. Upsert repo
	repoName, err := RepoNameFromURL(url)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`INSERT INTO repos (name, path) VALUES (?, ?)
		 ON CONFLICT(name) DO UPDATE SET path = excluded.path`,
		repoName, repoPath,
	)
	if err != nil {
		return fmt.Errorf("upsert repo: %w", err)
	}
	var repoID int64
	err = s.DB.QueryRow("SELECT id FROM repos WHERE name = ?", repoName).Scan(&repoID)
	if err != nil {
		return fmt.Errorf("get repo id: %w", err)
	}

	// 3. Load known commits
	knownCommits := make(map[string]bool)
	rows, err := s.DB.Query("SELECT hash FROM commits WHERE repo_id = ?", repoID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return err
		}
		knownCommits[h] = true
	}
	rows.Close()

	// 4. List refs
	report("Listing refs...")
	type refInfo struct {
		name   string
		tipOID plumbing.Hash
	}
	var refList []refInfo
	refs, err := repo.References()
	if err != nil {
		return fmt.Errorf("list refs: %w", err)
	}
	refs.ForEach(func(ref *plumbing.Reference) error {
		// Resolve symbolic refs
		resolved := ref
		if ref.Type() == plumbing.SymbolicReference {
			r, err := repo.Reference(ref.Name(), true)
			if err != nil {
				return nil // skip broken refs
			}
			resolved = r
		}
		refList = append(refList, refInfo{
			name:   ref.Name().String(),
			tipOID: resolved.Hash(),
		})
		return nil
	})
	report(fmt.Sprintf("Found %d refs.", len(refList)))

	// 5. Begin transaction
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	codeBatch := s.CodeIndex.NewBatch()
	diffBatch := s.DiffIndex.NewBatch()

	var totalNewCommits, totalNewBlobs int
	treeCache := make(map[plumbing.Hash]map[string]plumbing.Hash)

	// 6. For each ref
	for refIdx, ri := range refList {
		// Walk ancestors from tip
		type commitData struct {
			oid       plumbing.Hash
			author    string
			message   string
			timestamp int64
			treeHash  plumbing.Hash
			parentIDs []plumbing.Hash
		}
		var newCommits []commitData
		visited := make(map[plumbing.Hash]bool)
		walkStack := []plumbing.Hash{ri.tipOID}
		depthTruncated := false

		for len(walkStack) > 0 {
			if opts.MaxHistoryDepth > 0 && len(newCommits) >= opts.MaxHistoryDepth {
				depthTruncated = true
				break
			}
			oid := walkStack[len(walkStack)-1]
			walkStack = walkStack[:len(walkStack)-1]

			oidHex := oid.String()
			if knownCommits[oidHex] || visited[oid] {
				continue
			}
			visited[oid] = true

			commitObj, err := repo.CommitObject(oid)
			if err != nil {
				continue // skip non-commit objects
			}

			var parentIDs []plumbing.Hash
			for _, p := range commitObj.ParentHashes {
				parentIDs = append(parentIDs, p)
				walkStack = append(walkStack, p)
			}

			newCommits = append(newCommits, commitData{
				oid:       oid,
				author:    commitObj.Author.Name,
				message:   commitObj.Message,
				timestamp: commitObj.Author.When.Unix(),
				treeHash:  commitObj.TreeHash,
				parentIDs: parentIDs,
			})
		}

		// Reverse for oldest-first processing
		for i, j := 0, len(newCommits)-1; i < j; i, j = i+1, j-1 {
			newCommits[i], newCommits[j] = newCommits[j], newCommits[i]
		}

		if len(newCommits) > 0 {
			report(fmt.Sprintf("Ref %d/%d: %s — %d new commits",
				refIdx+1, len(refList), ri.name, len(newCommits)))
		}
		if depthTruncated {
			report(fmt.Sprintf("Warning: history depth limit (%d) reached for ref %s.",
				opts.MaxHistoryDepth, ri.name))
		}

		for _, cd := range newCommits {
			oidHex := cd.oid.String()

			// Insert commit
			_, err := tx.Exec(
				`INSERT OR IGNORE INTO commits (repo_id, hash, author, message, timestamp)
				 VALUES (?, ?, ?, ?, ?)`,
				repoID, oidHex, cd.author, cd.message, cd.timestamp,
			)
			if err != nil {
				return fmt.Errorf("insert commit: %w", err)
			}

			var commitDBID int64
			err = tx.QueryRow("SELECT id FROM commits WHERE hash = ?", oidHex).Scan(&commitDBID)
			if err != nil {
				return fmt.Errorf("get commit id: %w", err)
			}

			totalNewCommits++
			if totalNewCommits%500 == 0 {
				report(fmt.Sprintf("Processed %d commits, %d new blobs...", totalNewCommits, totalNewBlobs))
			}

			// Insert parents
			for _, parentOID := range cd.parentIDs {
				var parentDBID int64
				err := tx.QueryRow("SELECT id FROM commits WHERE hash = ?", parentOID.String()).Scan(&parentDBID)
				if err == nil {
					if _, err := tx.Exec("INSERT OR IGNORE INTO commit_parents (commit_id, parent_id) VALUES (?, ?)",
						commitDBID, parentDBID); err != nil {
						return fmt.Errorf("insert commit parent: %w", err)
					}
				}
			}

			// Get tree entries (with cache)
			if len(treeCache) >= 32 {
				treeCache = make(map[plumbing.Hash]map[string]plumbing.Hash)
			}
			childEntries, err := getTreeEntries(repo, cd.treeHash, treeCache)
			if err != nil {
				return fmt.Errorf("get tree entries: %w", err)
			}

			parentEntries := make(map[string]plumbing.Hash)
			if len(cd.parentIDs) > 0 {
				parentCommit, pErr := repo.CommitObject(cd.parentIDs[0])
				if pErr == nil {
					if pe, peErr := getTreeEntries(repo, parentCommit.TreeHash, treeCache); peErr == nil {
						parentEntries = pe
					}
				}
			}

			// Find changed files
			for path, childBlobOID := range childEntries {
				parentBlobOID, existsInParent := parentEntries[path]
				if existsInParent && parentBlobOID == childBlobOID {
					continue // unchanged
				}

				// Ensure new blob
				newBlobDBID, isNew, err := ensureBlob(tx, repo, childBlobOID, path, codeBatch)
				if err != nil {
					return err
				}
				if isNew {
					totalNewBlobs++
				}

				// Ensure old blob if exists
				var oldBlobDBID sql.NullInt64
				if existsInParent {
					id, isNew, err := ensureBlob(tx, repo, parentBlobOID, path, codeBatch)
					if err != nil {
						return err
					}
					if isNew {
						totalNewBlobs++
					}
					oldBlobDBID = sql.NullInt64{Int64: id, Valid: true}
				}

				// Insert diff
				_, err = tx.Exec(
					`INSERT OR IGNORE INTO diffs (commit_id, path, old_blob_id, new_blob_id)
					 VALUES (?, ?, ?, ?)`,
					commitDBID, path, oldBlobDBID, newBlobDBID,
				)
				if err != nil {
					return fmt.Errorf("insert diff: %w", err)
				}

				var diffDBID int64
				err = tx.QueryRow("SELECT id FROM diffs WHERE commit_id = ? AND path = ?",
					commitDBID, path).Scan(&diffDBID)
				if err != nil {
					continue
				}

				// Index diff in Bleve
				diffText := generateDiffText(repo, path, parentBlobOID, childBlobOID, existsInParent, true)
				if diffText != "" {
					diffBatch.Index(fmt.Sprintf("diff_%d", diffDBID), BleveDiffDoc{Content: diffText})
				}
			}

			// Handle deleted files
			for path, parentBlobOID := range parentEntries {
				if _, exists := childEntries[path]; exists {
					continue
				}
				oldBlobDBID, isNew, err := ensureBlob(tx, repo, parentBlobOID, path, codeBatch)
				if err != nil {
					return err
				}
				if isNew {
					totalNewBlobs++
				}
				_, err = tx.Exec(
					`INSERT OR IGNORE INTO diffs (commit_id, path, old_blob_id, new_blob_id)
					 VALUES (?, ?, ?, NULL)`,
					commitDBID, path, oldBlobDBID,
				)
				if err != nil {
					continue
				}

				var diffDBID int64
				err = tx.QueryRow("SELECT id FROM diffs WHERE commit_id = ? AND path = ?",
					commitDBID, path).Scan(&diffDBID)
				if err != nil {
					continue
				}
				diffText := generateDiffText(repo, path, parentBlobOID, plumbing.ZeroHash, true, false)
				if diffText != "" {
					diffBatch.Index(fmt.Sprintf("diff_%d", diffDBID), BleveDiffDoc{Content: diffText})
				}
			}

			knownCommits[oidHex] = true
		}

		// Build file_revs for tip
		tipHex := ri.tipOID.String()
		var tipCommitDBID int64
		err = tx.QueryRow("SELECT id FROM commits WHERE hash = ?", tipHex).Scan(&tipCommitDBID)
		if err != nil {
			continue // skip if tip not indexed
		}

		if _, err := tx.Exec("DELETE FROM file_revs WHERE commit_id = ?", tipCommitDBID); err != nil {
			return fmt.Errorf("delete file_revs: %w", err)
		}

		var tipEntries map[string]plumbing.Hash
		tipCommit, tErr := repo.CommitObject(ri.tipOID)
		if tErr == nil {
			tipEntries, _ = getTreeEntries(repo, tipCommit.TreeHash, treeCache)
		}
		if tipEntries == nil {
			tipEntries = make(map[string]plumbing.Hash)
		}

		for path, blobOID := range tipEntries {
			blobDBID, isNew, err := ensureBlob(tx, repo, blobOID, path, codeBatch)
			if err != nil {
				continue
			}
			if isNew {
				totalNewBlobs++
			}
			if _, err := tx.Exec("INSERT OR IGNORE INTO file_revs (commit_id, path, blob_id) VALUES (?, ?, ?)",
				tipCommitDBID, path, blobDBID); err != nil {
				return fmt.Errorf("insert file_rev: %w", err)
			}
		}

		// Upsert ref
		if _, err := tx.Exec(
			`INSERT INTO refs (repo_id, name, commit_id) VALUES (?, ?, ?)
			 ON CONFLICT(repo_id, name) DO UPDATE SET commit_id = excluded.commit_id`,
			repoID, ri.name, tipCommitDBID,
		); err != nil {
			return fmt.Errorf("upsert ref: %w", err)
		}
	}

	report(fmt.Sprintf("Indexing complete: %d new commits, %d new blobs.", totalNewCommits, totalNewBlobs))

	// Commit Bleve batches
	report("Committing indexes...")
	if err := s.CodeIndex.Batch(codeBatch); err != nil {
		return fmt.Errorf("commit code index: %w", err)
	}
	if err := s.DiffIndex.Batch(diffBatch); err != nil {
		return fmt.Errorf("commit diff index: %w", err)
	}

	// Commit SQLite transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// getTreeEntries returns a map of filepath -> blob hash for all blobs in a tree.
func getTreeEntries(repo *git.Repository, treeHash plumbing.Hash, cache map[plumbing.Hash]map[string]plumbing.Hash) (map[string]plumbing.Hash, error) {
	if treeHash == (plumbing.Hash{}) {
		return nil, fmt.Errorf("zero hash")
	}
	if cached, ok := cache[treeHash]; ok {
		return cached, nil
	}

	tree, err := repo.TreeObject(treeHash)
	if err != nil {
		return nil, err
	}

	entries := make(map[string]plumbing.Hash)
	tree.Files().ForEach(func(f *object.File) error {
		entries[f.Name] = f.Hash
		return nil
	})

	cache[treeHash] = entries
	return entries, nil
}

// ensureBlob inserts a blob if not already present and indexes it in Bleve.
// Returns (blobDBID, isNew, error).
func ensureBlob(tx *sql.Tx, repo *git.Repository, blobOID plumbing.Hash, path string, codeBatch *bleve.Batch) (int64, bool, error) {
	contentHash := blobOID.String()
	lang := language.Detect(path)

	var langPtr *string
	if lang != "" {
		langPtr = &lang
	}

	_, err := tx.Exec(
		"INSERT OR IGNORE INTO blobs (content_hash, language) VALUES (?, ?)",
		contentHash, langPtr,
	)
	if err != nil {
		return 0, false, fmt.Errorf("insert blob: %w", err)
	}

	var blobDBID int64
	err = tx.QueryRow("SELECT id FROM blobs WHERE content_hash = ?", contentHash).Scan(&blobDBID)
	if err != nil {
		return 0, false, fmt.Errorf("get blob id: %w", err)
	}

	// Check if this was newly inserted by trying to read the content
	// We use a simple heuristic: try to index and if the blob already has an ID, it might be old.
	// Actually, we check changes via the result: if the blob was INSERT OR IGNORE'd and already existed,
	// we don't need to re-index. We detect this by checking if the ID was already in our batch.
	// Simplification: always check if we've already indexed this content_hash before.

	// For simplicity in Go, we track this with a query
	var parsed int
	tx.QueryRow("SELECT parsed FROM blobs WHERE id = ?", blobDBID).Scan(&parsed)

	isNew := false
	// We check if we need to index this blob by seeing if it's the first time we encounter it
	// Use a simpler approach: try to get blob content only for new inserts
	blobObj, bErr := repo.BlobObject(blobOID)
	if bErr == nil && parsed == 0 {
		reader, rErr := blobObj.Reader()
		if rErr == nil {
			content, readErr := io.ReadAll(reader)
			reader.Close()
			if readErr == nil && utf8.Valid(content) && len(content) > 0 {
				codeBatch.Index(fmt.Sprintf("blob_%d", blobDBID), BleveCodeDoc{Content: string(content)})
				isNew = true
			}
		}
	}

	return blobDBID, isNew, nil
}

// generateDiffText creates simple diff text for full-text search indexing.
func generateDiffText(repo *git.Repository, path string, oldOID, newOID plumbing.Hash, hasOld, hasNew bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)

	if hasOld && oldOID != (plumbing.Hash{}) {
		blob, err := repo.BlobObject(oldOID)
		if err == nil {
			reader, err := blob.Reader()
			if err == nil {
				content, err := io.ReadAll(reader)
				reader.Close()
				if err == nil && utf8.Valid(content) {
					lines := strings.SplitN(string(content), "\n", 101)
					for i, line := range lines {
						if i >= 100 {
							break
						}
						fmt.Fprintf(&b, "-%s\n", line)
					}
				}
			}
		}
	}

	if hasNew && newOID != (plumbing.Hash{}) {
		blob, err := repo.BlobObject(newOID)
		if err == nil {
			reader, err := blob.Reader()
			if err == nil {
				content, err := io.ReadAll(reader)
				reader.Close()
				if err == nil && utf8.Valid(content) {
					lines := strings.SplitN(string(content), "\n", 101)
					for i, line := range lines {
						if i >= 100 {
							break
						}
						fmt.Fprintf(&b, "+%s\n", line)
					}
				}
			}
		}
	}

	return b.String()
}

// ParseStats holds statistics from the symbol parsing phase.
type ParseStats struct {
	BlobsParsed      uint64
	SymbolsExtracted uint64
}

// ParseSymbols extracts symbols and references from all unparsed blobs with
// supported languages and inserts them into the symbols and symbol_refs tables.
func ParseSymbols(s *store.Store, progress ProgressFunc) (ParseStats, error) {
	report := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	var stats ParseStats

	// Collect supported languages into a set for the query placeholder.
	supported := symbols.SupportedLanguages()
	if len(supported) == 0 {
		return stats, nil
	}

	// Build placeholders for IN clause.
	placeholders := make([]string, len(supported))
	args := make([]interface{}, len(supported))
	for i, lang := range supported {
		placeholders[i] = "?"
		args[i] = lang
	}
	inClause := strings.Join(placeholders, ", ")

	// Query unparsed blobs with a supported language.
	query := fmt.Sprintf(
		"SELECT id, content_hash, language FROM blobs WHERE parsed = 0 AND language IN (%s)",
		inClause,
	)
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return stats, fmt.Errorf("query unparsed blobs: %w", err)
	}

	type blobRow struct {
		id          int64
		contentHash string
		language    string
	}
	var blobs []blobRow
	for rows.Next() {
		var b blobRow
		if err := rows.Scan(&b.id, &b.contentHash, &b.language); err != nil {
			rows.Close()
			return stats, fmt.Errorf("scan blob row: %w", err)
		}
		blobs = append(blobs, b)
	}
	rows.Close()

	if len(blobs) == 0 {
		report("No unparsed blobs to process.")
		return stats, nil
	}
	report(fmt.Sprintf("Found %d unparsed blobs with supported languages.", len(blobs)))

	// Collect repo paths.
	repoRows, err := s.DB.Query("SELECT path FROM repos")
	if err != nil {
		return stats, fmt.Errorf("query repo paths: %w", err)
	}
	var repoPaths []string
	for repoRows.Next() {
		var p string
		if err := repoRows.Scan(&p); err != nil {
			repoRows.Close()
			return stats, fmt.Errorf("scan repo path: %w", err)
		}
		repoPaths = append(repoPaths, p)
	}
	repoRows.Close()

	if len(repoPaths) == 0 {
		return stats, fmt.Errorf("no repos found in database")
	}

	// Open git repos.
	var repos []*git.Repository
	for _, rp := range repoPaths {
		r, err := git.PlainOpen(rp)
		if err != nil {
			continue
		}
		repos = append(repos, r)
	}
	if len(repos) == 0 {
		return stats, fmt.Errorf("could not open any git repos")
	}

	// Begin transaction.
	tx, err := s.DB.Begin()
	if err != nil {
		return stats, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for i, blob := range blobs {
		if (i+1)%100 == 0 {
			report(fmt.Sprintf("Parsing symbols: %d/%d blobs...", i+1, len(blobs)))
		}

		// Read blob content from git repos by OID (content_hash).
		oid := plumbing.NewHash(blob.contentHash)
		var content []byte
		for _, r := range repos {
			blobObj, bErr := r.BlobObject(oid)
			if bErr != nil {
				continue
			}
			reader, rErr := blobObj.Reader()
			if rErr != nil {
				continue
			}
			var readErr error
			content, readErr = io.ReadAll(reader)
			reader.Close()
			if readErr != nil {
				content = nil
				continue
			}
			break
		}
		if content == nil || !utf8.Valid(content) {
			// Mark as parsed even if we can't read it to avoid re-processing.
			tx.Exec("UPDATE blobs SET parsed = 1 WHERE id = ?", blob.id)
			continue
		}

		syms, refs := symbols.Extract(string(content), blob.language)

		// Insert symbols, tracking their DB IDs for parent resolution and ref linking.
		symDBIDs := make([]int64, len(syms))
		for j, sym := range syms {
			res, err := tx.Exec(
				`INSERT INTO symbols (blob_id, parent_id, name, kind, line, col, end_line, end_col, signature, return_type, params) VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				blob.id, sym.Name, sym.Kind, sym.Line, sym.Col, sym.EndLine, sym.EndCol, sym.Signature, sym.ReturnType, sym.Params,
			)
			if err != nil {
				return stats, fmt.Errorf("insert symbol: %w", err)
			}
			symDBIDs[j], _ = res.LastInsertId()
			stats.SymbolsExtracted++
		}

		// Update parent_id for nested symbols.
		for j, sym := range syms {
			if sym.ParentIdx >= 0 && sym.ParentIdx < len(symDBIDs) {
				_, err := tx.Exec("UPDATE symbols SET parent_id = ? WHERE id = ?",
					symDBIDs[sym.ParentIdx], symDBIDs[j])
				if err != nil {
					return stats, fmt.Errorf("update symbol parent: %w", err)
				}
			}
		}

		// Insert refs.
		for _, ref := range refs {
			var symbolID int64
			if ref.ContainingSymIdx >= 0 && ref.ContainingSymIdx < len(symDBIDs) {
				symbolID = symDBIDs[ref.ContainingSymIdx]
			}
			_, err := tx.Exec(
				`INSERT INTO symbol_refs (blob_id, symbol_id, ref_name, kind, line, col) VALUES (?, ?, ?, ?, ?, ?)`,
				blob.id, symbolID, ref.RefName, ref.Kind, ref.Line, ref.Col,
			)
			if err != nil {
				return stats, fmt.Errorf("insert symbol ref: %w", err)
			}
		}

		// Mark blob as parsed.
		_, err = tx.Exec("UPDATE blobs SET parsed = 1 WHERE id = ?", blob.id)
		if err != nil {
			return stats, fmt.Errorf("mark blob parsed: %w", err)
		}

		stats.BlobsParsed++
	}

	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit transaction: %w", err)
	}

	report(fmt.Sprintf("Symbol parsing complete: %d blobs parsed, %d symbols extracted.",
		stats.BlobsParsed, stats.SymbolsExtracted))

	return stats, nil
}
