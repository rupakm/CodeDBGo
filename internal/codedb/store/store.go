package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/mattn/go-sqlite3"
)

// Store wraps a SQLite database and a Bleve full-text search index.
type Store struct {
	DB        *sql.DB
	CodeIndex bleve.Index
	DiffIndex bleve.Index
	Root      string
}

// Open opens (or creates) a Store at the given root directory.
// It creates the directory structure, initializes SQLite and Bleve indexes.
func Open(root string) (*Store, error) {
	reposDir := filepath.Join(root, "repos")
	bleveDir := filepath.Join(root, "bleve")
	bleveCodeDir := filepath.Join(bleveDir, "code")
	bleveDiffDir := filepath.Join(bleveDir, "diff")

	for _, dir := range []string{reposDir, bleveDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	dbPath := filepath.Join(root, "metadata.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := CreateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	codeIndex, err := openOrCreateBleveIndex(bleveCodeDir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open code index: %w", err)
	}

	diffIndex, err := openOrCreateBleveIndex(bleveDiffDir)
	if err != nil {
		db.Close()
		codeIndex.Close()
		return nil, fmt.Errorf("open diff index: %w", err)
	}

	return &Store{
		DB:        db,
		CodeIndex: codeIndex,
		DiffIndex: diffIndex,
		Root:      root,
	}, nil
}

// ReposDir returns the path to the bare git repos directory.
func (s *Store) ReposDir() string {
	return filepath.Join(s.Root, "repos")
}

// Close closes all resources.
func (s *Store) Close() error {
	var firstErr error
	if err := s.CodeIndex.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.DiffIndex.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.DB.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func openOrCreateBleveIndex(path string) (bleve.Index, error) {
	idx, err := bleve.Open(path)
	if err == bleve.ErrorIndexPathDoesNotExist {
		mapping := bleve.NewIndexMapping()
		return bleve.New(path, mapping)
	}
	return idx, err
}
