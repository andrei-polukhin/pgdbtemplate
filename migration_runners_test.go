package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/frankban/quicktest"
	_ "github.com/lib/pq"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestPgFileMigrationRunner tests the migration runner functionality.
func TestPgFileMigrationRunner(t *testing.T) {
	c := quicktest.New(t)
	ctx := context.Background()

	// Set up real database connection.
	db := setupTestDatabase(c)
	defer db.Close()

	conn := &pgdbtemplate.StandardPgDatabaseConnection{DB: db}

	// Create temporary migration files.
	tempDir := c.TempDir()

	migration1 := "CREATE TABLE test_users (id SERIAL PRIMARY KEY, name VARCHAR(100));"
	migration2 := "INSERT INTO test_users (name) VALUES ('Alice'), ('Bob');"

	err := os.WriteFile(tempDir+"/001_users.sql", []byte(migration1), 0644)
	c.Assert(err, quicktest.IsNil)

	err = os.WriteFile(tempDir+"/002_data.sql", []byte(migration2), 0644)
	c.Assert(err, quicktest.IsNil)

	// Test migration runner creation.
	runner := pgdbtemplate.NewPgFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
	c.Assert(runner, quicktest.Not(quicktest.IsNil))

	// Run migrations on real database.
	err = runner.RunMigrations(ctx, conn)
	c.Assert(err, quicktest.IsNil)

	// Verify table was created and data inserted.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test_users").Scan(&count)
	c.Assert(err, quicktest.IsNil)
	c.Assert(count, quicktest.Equals, 2)

	// Verify data content.
	var name string
	err = db.QueryRow("SELECT name FROM test_users ORDER BY id LIMIT 1").Scan(&name)
	c.Assert(err, quicktest.IsNil)
	c.Assert(name, quicktest.Equals, "Alice")

	// Clean up.
	_, err = db.Exec("DROP TABLE test_users")
	c.Assert(err, quicktest.IsNil)
}

// TestAlphabeticalMigrationFilesSorting tests the sorting function.
func TestAlphabeticalMigrationFilesSorting(t *testing.T) {
	c := quicktest.New(t)

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

	c.Assert(sorted, quicktest.DeepEquals, expected)

	// Verify original slice wasn't modified.
	c.Assert(files[0], quicktest.Equals, "/path/003_third.sql")
}

// setupTestDatabase creates a test database connection.
func setupTestDatabase(c *quicktest.C) *sql.DB {
	connString := os.Getenv("POSTGRES_CONNECTION_STRING")
	if connString == "" {
		connString = "postgres://postgres:password@localhost:5432/postgres?sslmode=disable"
	}

	db, err := sql.Open("postgres", connString)
	c.Assert(err, quicktest.IsNil)

	err = db.Ping()
	c.Assert(err, quicktest.IsNil)

	return db
}
