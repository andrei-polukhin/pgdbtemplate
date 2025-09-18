package pgdbtemplate_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestFileMigrationRunner tests the migration runner functionality.
func TestFileMigrationRunner(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	// Set up mock database connection.
	conn := &mockDatabaseConnection{}

	// Create unique table names to avoid conflicts.
	uniqueSuffix := fmt.Sprintf("_%d_%d", time.Now().UnixNano(), os.Getpid())
	tableName := "test_users" + uniqueSuffix

	// Create temporary migration files.
	tempDir := c.TempDir()

	migration1 := fmt.Sprintf("CREATE TABLE %s (id SERIAL PRIMARY KEY, name VARCHAR(100));", tableName)
	migration2 := fmt.Sprintf("INSERT INTO %s (name) VALUES ('Alice'), ('Bob');", tableName)

	err := os.WriteFile(tempDir+"/001_users.sql", []byte(migration1), 0644)
	c.Assert(err, qt.IsNil)

	err = os.WriteFile(tempDir+"/002_data.sql", []byte(migration2), 0644)
	c.Assert(err, qt.IsNil)

	// Test migration runner creation.
	runner := pgdbtemplate.NewFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
	c.Assert(runner, qt.IsNotNil)

	// Run migrations on mock database.
	err = runner.RunMigrations(ctx, conn)
	c.Assert(err, qt.IsNil)

	// Verify queries were executed.
	c.Assert(len(conn.executed), qt.Equals, 2)
	c.Assert(strings.Contains(conn.executed[0], "CREATE TABLE"), qt.IsTrue)
	c.Assert(strings.Contains(conn.executed[1], "INSERT INTO"), qt.IsTrue)
}

// TestFileMigrationRunnerErrors tests error conditions in migration runner.
func TestFileMigrationRunnerErrors(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	c.Run("Invalid SQL causes error", func(c *qt.C) {
		conn := &mockDatabaseConnection{failOnInvalid: true}

		tempDir := c.TempDir()
		invalidSQL := "THIS IS NOT VALID SQL;"
		err := os.WriteFile(tempDir+"/001_invalid.sql", []byte(invalidSQL), 0644)
		c.Assert(err, qt.IsNil)

		runner := pgdbtemplate.NewFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
		err = runner.RunMigrations(ctx, conn)
		c.Assert(err, qt.ErrorMatches, ".*failed to execute migration.*")
	})

	c.Run("Non-existent directory", func(c *qt.C) {
		conn := &mockDatabaseConnection{}

		runner := pgdbtemplate.NewFileMigrationRunner([]string{"/non/existent/path"}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
		err := runner.RunMigrations(ctx, conn)
		c.Assert(err, qt.ErrorMatches, ".*failed to collect files.*")
	})

	c.Run("Empty migration directory", func(c *qt.C) {
		conn := &mockDatabaseConnection{}

		tempDir := c.TempDir()
		runner := pgdbtemplate.NewFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
		err := runner.RunMigrations(ctx, conn)
		c.Assert(err, qt.IsNil) // Empty directory should not error.
	})

	c.Run("File read permission error", func(c *qt.C) {
		conn := &mockDatabaseConnection{}

		tempDir := c.TempDir()
		validSQL := "SELECT 1;"
		filePath := tempDir + "/001_test.sql"
		err := os.WriteFile(filePath, []byte(validSQL), 0644)
		c.Assert(err, qt.IsNil)

		// Make file unreadable.
		err = os.Chmod(filePath, 0000)
		c.Assert(err, qt.IsNil)

		runner := pgdbtemplate.NewFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
		err = runner.RunMigrations(ctx, conn)
		c.Assert(err, qt.IsNotNil)
	})
}

// TestNewFileMigrationRunnerBranches tests both branches of NewFileMigrationRunner.
func TestNewFileMigrationRunnerBranches(t *testing.T) {
	c := qt.New(t)

	// Test with nil ordering function (should use default).
	runner1 := pgdbtemplate.NewFileMigrationRunner([]string{"/tmp"}, nil)
	c.Assert(runner1, qt.IsNotNil)

	// Test with non-nil ordering function (should use provided).
	customFunc := func(files []string) []string { return files }
	runner2 := pgdbtemplate.NewFileMigrationRunner([]string{"/tmp"}, customFunc)
	c.Assert(runner2, qt.IsNotNil)
}

// mockDatabaseConnection is a mock implementation of pgdbtemplate.DatabaseConnection.
type mockDatabaseConnection struct {
	executed      []string
	failOnInvalid bool
}

func (m *mockDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (any, error) {
	if m.failOnInvalid && strings.Contains(query, "THIS IS NOT VALID") {
		return nil, fmt.Errorf("invalid SQL")
	}
	m.executed = append(m.executed, query)
	return nil, nil
}

func (m *mockDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) pgdbtemplate.Row {
	return &migrationMockRow{}
}

func (m *mockDatabaseConnection) Close() error {
	return nil
}

// migrationMockRow is a mock implementation of pgdbtemplate.Row.
type migrationMockRow struct{}

func (r *migrationMockRow) Scan(dest ...any) error {
	return nil
}
