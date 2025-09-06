package pgdbtemplate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PgFileMigrationRunner runs migrations from filesystem.
type PgFileMigrationRunner struct {
	migrationPaths []string
	orderingFunc   func([]string) []string
}

// NewPgFileMigrationRunner creates a new file-based migration runner.
//
// The caller is responsible for ensuring that the paths slice is not modified
// after being passed to orderingFunc. Upon the nil function provided, an alphabetical
// sorting will be used.
func NewPgFileMigrationRunner(paths []string, orderingFunc func([]string) []string) *PgFileMigrationRunner {
	if orderingFunc == nil {
		orderingFunc = AlphabeticalMigrationFilesSorting
	}
	return &PgFileMigrationRunner{
		migrationPaths: paths,
		orderingFunc:   orderingFunc,
	}
}

// RunMigrations executes all migration files on the connection.
func (r *PgFileMigrationRunner) RunMigrations(ctx context.Context, conn PgDatabaseConnection) error {
	var allFiles []string

	// Collect all SQL files from all paths.
	for _, path := range r.migrationPaths {
		files, err := r.collectSQLFiles(path)
		if err != nil {
			return fmt.Errorf("failed to collect files from %s: %w", path, err)
		}
		allFiles = append(allFiles, files...)
	}

	// Order files.
	if r.orderingFunc != nil {
		allFiles = r.orderingFunc(allFiles)
	}

	// Execute each file.
	for _, file := range allFiles {
		if err := r.executeFile(ctx, conn, file); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}
	}
	return nil
}

func (r *PgFileMigrationRunner) collectSQLFiles(path string) ([]string, error) {
	var files []string

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(filePath, ".sql") {
			files = append(files, filePath)
		}
		return nil
	})
	return files, err
}

func (r *PgFileMigrationRunner) executeFile(ctx context.Context, conn PgDatabaseConnection, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	_, err = conn.ExecContext(ctx, string(content))
	return err
}

// AlphabeticalMigrationFilesSorting makes a copy of the provided slice
// and sorts migration files alphabetically in the copied slice.
//
// The original slice is not modified.
func AlphabeticalMigrationFilesSorting(files []string) []string {
	sorted := make([]string, len(files))
	copy(sorted, files)
	sort.Strings(sorted)
	return sorted
}
