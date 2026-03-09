package match

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sageox/codedbgo/internal/codedb/match/pattern"
	"github.com/sageox/codedbgo/internal/codedb/store"
)

func TestParseWhereClause(t *testing.T) {
	tests := []struct {
		input   string
		metavar string
		op      string
		value   string
	}{
		{"$F ~ /^unsafe_/", "F", "~", "/^unsafe_/"},
		{"$X == nil", "X", "==", "nil"},
		{"$X != 0", "X", "!=", "0"},
		{"$N > 10", "N", ">", "10"},
		{"$CMD !~ /^\"safe/", "CMD", "!~", `/^"safe/`},
	}
	for _, tt := range tests {
		c, err := parseWhereClause(tt.input)
		if err != nil {
			t.Errorf("parseWhereClause(%q) error: %v", tt.input, err)
			continue
		}
		if c.Metavar != tt.metavar {
			t.Errorf("metavar = %q, want %q", c.Metavar, tt.metavar)
		}
		if c.Op != tt.op {
			t.Errorf("op = %q, want %q", c.Op, tt.op)
		}
		if c.Value != tt.value {
			t.Errorf("value = %q, want %q", c.Value, tt.value)
		}
	}
}

func TestBuildFormulaNoOptions(t *testing.T) {
	pat, _ := pattern.Parse("foo($X)")
	formula, constraints, err := buildFormula(pat, MatchOptions{Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if formula != nil {
		t.Error("expected nil formula when no options")
	}
	if len(constraints) != 0 {
		t.Error("expected no constraints")
	}
}

func TestBuildFormulaWithNot(t *testing.T) {
	pat, _ := pattern.Parse("foo($X)")
	opts := MatchOptions{
		Lang:    "go",
		NotPats: []string{"foo(1)"},
	}
	formula, _, err := buildFormula(pat, opts)
	if err != nil {
		t.Fatal(err)
	}
	and, ok := formula.(*pattern.And)
	if !ok {
		t.Fatalf("expected And, got %T", formula)
	}
	if len(and.Formulas) != 2 {
		t.Fatalf("formulas = %d, want 2", len(and.Formulas))
	}
	if _, ok := and.Formulas[1].(*pattern.Not); !ok {
		t.Errorf("formula[1] = %T, want Not", and.Formulas[1])
	}
}

func TestBuildFormulaWithWhere(t *testing.T) {
	pat, _ := pattern.Parse("$F($X)")
	opts := MatchOptions{
		Lang:         "go",
		WhereClauses: []string{"$F ~ /^unsafe_/"},
	}
	_, constraints, err := buildFormula(pat, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(constraints) != 1 {
		t.Fatalf("constraints = %d, want 1", len(constraints))
	}
	if constraints[0].Metavar != "F" || constraints[0].Op != "~" {
		t.Errorf("constraint = %+v", constraints[0])
	}
}

func TestExtractHints(t *testing.T) {
	pat, _ := pattern.Parse("foo($X)")
	hints := extractHints(pat)
	if len(hints) != 1 || hints[0] != "foo" {
		t.Errorf("hints = %v, want [foo]", hints)
	}
}

// --- E2E Integration Tests ---

// createTestRepo creates a git repo with a single commit containing the given files.
// Returns the repo path and a map of filename -> blob SHA.
func createTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repoDir := t.TempDir()

	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	for name, content := range files {
		fullPath := filepath.Join(repoDir, name)
		if dir := filepath.Dir(fullPath); dir != repoDir {
			os.MkdirAll(dir, 0o755)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("git add %s: %v", name, err)
		}
	}

	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}

	return repoDir
}

// seedStore creates store records for a repo and its files.
// It reads blob hashes from the git repo.
func seedStore(t *testing.T, s *store.Store, repoDir string, lang string, files map[string]string) {
	t.Helper()

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("tree: %v", err)
	}

	// Insert repo
	if _, err := s.Exec(`INSERT INTO repos (id, name, path) VALUES (1, 'test/repo', ?)`, repoDir); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// Insert commit
	if _, err := s.Exec(`INSERT INTO commits (id, repo_id, hash, author, message, timestamp) VALUES (1, 1, ?, 'test', 'initial', 1700000000)`, head.Hash().String()); err != nil {
		t.Fatalf("insert commit: %v", err)
	}

	// Insert ref
	if _, err := s.Exec(`INSERT INTO refs (id, repo_id, name, commit_id) VALUES (1, 1, 'refs/heads/main', 1)`); err != nil {
		t.Fatalf("insert ref: %v", err)
	}

	blobID := int64(1)
	for name := range files {
		entry, err := tree.FindEntry(name)
		if err != nil {
			t.Fatalf("find entry %s: %v", name, err)
		}

		if _, err := s.Exec(`INSERT INTO blobs (id, content_hash, language, parsed) VALUES (?, ?, ?, 1)`,
			blobID, entry.Hash.String(), lang); err != nil {
			t.Fatalf("insert blob: %v", err)
		}

		if _, err := s.Exec(`INSERT INTO file_revs (id, commit_id, path, blob_id) VALUES (?, 1, ?, ?)`,
			blobID, name, blobID); err != nil {
			t.Fatalf("insert file_rev: %v", err)
		}

		blobID++
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestE2EGoMetavarUnification(t *testing.T) {
	files := map[string]string{
		"main.go": `package main

func main() {
	foo(a, b, a)
	foo(a, b, c)
	bar(x)
}
`,
	}

	repoDir := createTestRepo(t, files)
	s := openTestStore(t)
	seedStore(t, s, repoDir, "go", files)

	results, err := Execute(context.Background(), s, MatchOptions{
		Pattern: "foo($X, ..., $X)",
		Lang:    "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Bindings["X"] != "a" {
		t.Errorf("$X = %q, want %q", results[0].Bindings["X"], "a")
	}
	if results[0].File != "main.go" {
		t.Errorf("file = %q, want main.go", results[0].File)
	}
}

func TestE2EGoSelfAssignment(t *testing.T) {
	files := map[string]string{
		"main.go": `package main

func main() {
	x := x
	y := z
}
`,
	}

	repoDir := createTestRepo(t, files)
	s := openTestStore(t)
	seedStore(t, s, repoDir, "go", files)

	results, err := Execute(context.Background(), s, MatchOptions{
		Pattern: "$X := $X",
		Lang:    "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
}

func TestE2EPythonCallWithNot(t *testing.T) {
	files := map[string]string{
		"app.py": `import os
os.system(cmd)
os.system("ls")
`,
	}

	repoDir := createTestRepo(t, files)
	s := openTestStore(t)
	seedStore(t, s, repoDir, "python", files)

	results, err := Execute(context.Background(), s, MatchOptions{
		Pattern: `os.system($X)`,
		Lang:    "python",
		NotPats: []string{`os.system("ls")`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Bindings["X"] != "cmd" {
		t.Errorf("$X = %q, want cmd", results[0].Bindings["X"])
	}
}
