package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	_ "github.com/lib/pq"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestFileMigrationRunner tests the migration runner functionality.
func TestFileMigrationRunner(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	// Set up real database connection.
	db := setupTestDatabase(c)
	defer func() {
		c.Assert(db.Close(), qt.IsNil)
	}()

	conn := &pgdbtemplate.StandardDatabaseConnection{DB: db}

	// Create unique table names to avoid conflicts.
	uniqueSuffix := fmt.Sprintf("_%d_%d", time.Now().UnixNano(), os.Getpid())
	tableName := "test_users" + uniqueSuffix

	// Ensure cleanup of any tables created during test.
	defer func() {
		_, err := db.Exec(fmt.Sprintf("DROP TABLE %s", tableName))
		c.Assert(err, qt.IsNil)
	}()

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

	// Run migrations on real database.
	err = runner.RunMigrations(ctx, conn)
	c.Assert(err, qt.IsNil)

	// Verify table was created and data inserted.
	var count int
	err = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	c.Assert(err, qt.IsNil)
	c.Assert(count, qt.Equals, 2)

	// Verify data content.
	var name string
	err = db.QueryRow(fmt.Sprintf("SELECT name FROM %s ORDER BY id LIMIT 1", tableName)).Scan(&name)
	c.Assert(err, qt.IsNil)
	c.Assert(name, qt.Equals, "Alice")
}

// TestAlphabeticalMigrationFilesSorting tests the sorting function.
func TestAlphabeticalMigrationFilesSorting(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	files := []string{
		"/path/003_third.sql",
		"/path/001_first.sql",
		"/path/002_second.sql",
	}

	sorted := pgdbtemplate.AlphabeticalMigrationFilesSorting(files)

	expected := []string{
		"/path/001_first.sql",
		"/path/002_second.sql",
		"/path/003_third.sql",
	}

	c.Assert(sorted, qt.DeepEquals, expected)

	// Verify original slice wasn't modified.
	c.Assert(files[0], qt.Equals, "/path/003_third.sql")
}

// setupTestDatabase creates a test database connection.
func setupTestDatabase(c *qt.C) *sql.DB {
	db, err := sql.Open("postgres", testConnectionString)
	c.Assert(err, qt.IsNil)

	err = db.Ping()
	c.Assert(err, qt.IsNil)

	return db
}

// TestFileMigrationRunnerErrors tests error conditions in migration runner.
func TestFileMigrationRunnerErrors(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	c.Run("Invalid SQL causes error", func(c *qt.C) {
		db := setupTestDatabase(c)
		defer func() { c.Assert(db.Close(), qt.IsNil) }()
		conn := &pgdbtemplate.StandardDatabaseConnection{DB: db}

		tempDir := c.TempDir()
		invalidSQL := "THIS IS NOT VALID SQL;"
		err := os.WriteFile(tempDir+"/001_invalid.sql", []byte(invalidSQL), 0644)
		c.Assert(err, qt.IsNil)

		runner := pgdbtemplate.NewFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
		err = runner.RunMigrations(ctx, conn)
		c.Assert(err, qt.IsNotNil)
		c.Assert(err, qt.ErrorMatches, ".*failed to execute migration.*")
	})

	c.Run("Non-existent directory", func(c *qt.C) {
		db := setupTestDatabase(c)
		defer func() { c.Assert(db.Close(), qt.IsNil) }()
		conn := &pgdbtemplate.StandardDatabaseConnection{DB: db}

		runner := pgdbtemplate.NewFileMigrationRunner([]string{"/non/existent/path"}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
		err := runner.RunMigrations(ctx, conn)
		c.Assert(err, qt.IsNotNil)
		c.Assert(err, qt.ErrorMatches, ".*failed to collect files.*")
	})

	c.Run("Empty migration directory", func(c *qt.C) {
		db := setupTestDatabase(c)
		defer func() { c.Assert(db.Close(), qt.IsNil) }()
		conn := &pgdbtemplate.StandardDatabaseConnection{DB: db}

		tempDir := c.TempDir()
		runner := pgdbtemplate.NewFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
		err := runner.RunMigrations(ctx, conn)
		c.Assert(err, qt.IsNil) // Empty directory should not error.
	})

	c.Run("File read permission error", func(c *qt.C) {
		db := setupTestDatabase(c)
		defer func() { c.Assert(db.Close(), qt.IsNil) }()
		conn := &pgdbtemplate.StandardDatabaseConnection{DB: db}

		tempDir := c.TempDir()
		validSQL := "SELECT 1;"
		filePath := tempDir + "/001_test.sql"
		err := os.WriteFile(filePath, []byte(validSQL), 0644)
		c.Assert(err, qt.IsNil)

		// Make file unreadable.
		err = os.Chmod(filePath, 0000)
		c.Assert(err, qt.IsNil)
		defer os.Chmod(filePath, 0644) // Restore permissions.

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
