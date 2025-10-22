package pgdbtemplate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NoOpMigrationRunner is a migration runner that does nothing.
type NoOpMigrationRunner struct{}

// RunMigrations does nothing and returns nil.
func (*NoOpMigrationRunner) RunMigrations(context.Context, DatabaseConnection) error {
	return nil
}

// FileMigrationRunner runs migrations from filesystem.
type FileMigrationRunner struct {
	migrationPaths []string
	orderingFunc   func([]string) []string
}

// NewFileMigrationRunner creates a new file-based migration runner.
//
// The caller is responsible for ensuring that the paths slice is not modified
// after being passed to orderingFunc. Upon the nil function provided, an
// alphabetical sorting will be used.
func NewFileMigrationRunner(paths []string, orderingFunc func([]string) []string) *FileMigrationRunner {
	if orderingFunc == nil {
		orderingFunc = AlphabeticalMigrationFilesSorting
	}
	return &FileMigrationRunner{
		migrationPaths: paths,
		orderingFunc:   orderingFunc,
	}
}

// RunMigrations executes all migration files on the connection.
func (r *FileMigrationRunner) RunMigrations(ctx context.Context, conn DatabaseConnection) error {
	var allFiles []string

	// Collect and order files from each path separately.
	for _, path := range r.migrationPaths {
		files, err := r.collectSQLFiles(path)
		if err != nil {
			return fmt.Errorf("failed to collect files from %s: %w", path, err)
		}

		// Order files within this directory.
		if len(files) > 0 {
			files = r.orderingFunc(files)
			allFiles = append(allFiles, files...)
		}
	}

	// Execute each file.
	for _, file := range allFiles {
		if err := r.executeFile(ctx, conn, file); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}
	}
	return nil
}

func (r *FileMigrationRunner) collectSQLFiles(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %q: %w", path, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, filepath.Join(path, entry.Name()))
		}
	}
	return files, nil
}

func (r *FileMigrationRunner) executeFile(ctx context.Context, conn DatabaseConnection, filePath string) error {
	content, err := os.ReadFile(filePath) // #nosec G304 -- Migration files are controlled by the application.
	if err != nil {
		return fmt.Errorf("failed to read migration file %q: %w", filePath, err)
	}

	_, err = conn.ExecContext(ctx, string(content))
	return err
}
