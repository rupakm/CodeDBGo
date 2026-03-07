package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sageox/codedbgo/internal/codedb"
	"github.com/sageox/codedbgo/internal/codedb/index"
	"github.com/sageox/codedbgo/internal/codedb/search"
	"github.com/sageox/codedbgo/internal/paths"
)

var rootFlags struct {
	root string
}

// rootCmd is the parent "codedb" command (or "ox codedb" when integrated).
var rootCmd = &cobra.Command{
	Use:   "codedb",
	Short: "Code search engine for indexed git repositories",
	Long: `CodeDB indexes git repositories into SQLite + Bleve full-text search,
supports Sourcegraph-style queries, and extracts symbols.

Data stored at: ~/.local/share/sageox/codedb/`,
}

// indexCmd: codedb index <url>
var indexCmd = &cobra.Command{
	Use:   "index <url>",
	Short: "Clone and index a git repository",
	Args:  cobra.ExactArgs(1),
	RunE:  runIndex,
}

var indexFlags struct {
	depth int
}

// searchCmd: codedb search <query>
var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search indexed code using Sourcegraph query syntax",
	Long: `Search indexed code using Sourcegraph query syntax.

Filters: repo:, file:, -file:, lang:, type: (code|diff|commit|symbol),
  rev:, count:, case:, author:, before:, after:, message:,
  select: (repo|file|symbol), calls:, calledby:, returns:,
  patterntype: (literal|regexp), /regex/

Examples:
  codedb search "lang:go func main"
  codedb search "type:symbol lang:rust SFrame"
  codedb search "type:diff author:alice streaming"
  codedb search "type:commit before:2026-01-01 refactor"
  codedb search "lang:go func main" --sql`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

var searchFlags struct {
	showSQL    bool
	jsonOutput bool
	count      int
}

// sqlCmd: codedb sql <query>
var sqlCmd = &cobra.Command{
	Use:   "sql <sql>",
	Short: "Run raw SQL query against the database",
	Args:  cobra.ExactArgs(1),
	RunE:  runSQL,
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&rootFlags.root, "root", paths.DataDir(), "data directory")

	// Index flags
	indexCmd.Flags().IntVar(&indexFlags.depth, "depth", 10000, "max commits per ref (0 = unlimited)")

	// Search flags
	searchCmd.Flags().BoolVar(&searchFlags.showSQL, "sql", false, "print generated SQL instead of executing")
	searchCmd.Flags().BoolVar(&searchFlags.jsonOutput, "json", false, "output results as JSON")
	searchCmd.Flags().IntVar(&searchFlags.count, "count", 0, "override result count limit")

	// Register subcommands
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(sqlCmd)

	// Silence cobra's default error/usage behavior
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func openDB() (*codedb.DB, error) {
	return codedb.Open(rootFlags.root)
}

func runIndex(cmd *cobra.Command, args []string) error {
	url := args[0]
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	progress := func(msg string) {
		fmt.Fprintln(os.Stderr, msg)
	}

	opts := index.IndexOptions{
		MaxHistoryDepth: indexFlags.depth,
		Progress:        progress,
	}

	if err := db.IndexRepo(cmd.Context(), url, opts); err != nil {
		return err
	}

	// Parse symbols after indexing
	stats, err := db.ParseSymbols(cmd.Context(), progress)
	if err != nil {
		return fmt.Errorf("parse symbols: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Done. Parsed %d blobs, extracted %d symbols.\n",
		stats.BlobsParsed, stats.SymbolsExtracted)

	return nil
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	if searchFlags.count > 0 {
		query = fmt.Sprintf("count:%d %s", searchFlags.count, query)
	}

	if searchFlags.showSQL {
		translated, err := db.TranslateQuery(query)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "-- Sourcegraph query: %s\n", query)
		fmt.Fprintf(cmd.OutOrStdout(), "-- Parameters: %v\n", translated.Params)
		fmt.Fprintln(cmd.OutOrStdout(), translated.SQL)
		return nil
	}

	results, err := db.Search(cmd.Context(), query)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No results found.")
		return nil
	}

	if searchFlags.jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	// Determine search type for display formatting
	parsed, _ := search.ParseQuery(query)
	if parsed == nil {
		for _, r := range results {
			fmt.Fprintln(cmd.OutOrStdout(), r.FilePath)
		}
		return nil
	}

	st := parsed.Type
	if parsed.Filters.Calls != "" || parsed.Filters.CalledBy != "" || parsed.Filters.Returns != "" {
		st = search.SearchTypeSymbol
	}

	out := cmd.OutOrStdout()
	for _, r := range results {
		switch st {
		case search.SearchTypeCode:
			fmt.Fprintf(out, "%s (score: %.2f)\n", r.FilePath, r.Score)
			if r.Content != "" {
				fmt.Fprintf(out, "  %s\n", r.Content)
			}
			fmt.Fprintln(out)
		case search.SearchTypeDiff:
			fmt.Fprintf(out, "%s %s (score: %.2f)\n", r.CommitHash, r.FilePath, r.Score)
			if r.Message != "" {
				fmt.Fprintf(out, "  %s\n", r.Message)
			}
			fmt.Fprintln(out)
		case search.SearchTypeCommit:
			fmt.Fprintf(out, "%s (%s) %s\n", r.CommitHash, r.Author, strings.TrimSpace(r.Message))
		case search.SearchTypeSymbol:
			fmt.Fprintf(out, "%s:%d %s %s\n", r.FilePath, r.Line, r.SymbolKind, r.SymbolName)
		}
	}

	return nil
}

func runSQL(cmd *cobra.Command, args []string) error {
	query := args[0]
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	cols, rows, err := db.RawSQL(query)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(cols, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}
