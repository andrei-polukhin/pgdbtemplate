package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	_ "github.com/lib/pq"

	"github.com/andrei-polukhin/pgdbtemplate"
)

const (
	testTimeout = 30 * time.Second
)

// TestTemplateManager tests the complete template manager functionality.
func TestTemplateManager(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Set up connection provider with test database.
	connProvider := setupTestConnectionProvider()

	// Set up migration runner with test migrations.
	migrationRunner := setupTestMigrationRunner(c)

	// Create template manager.
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    migrationRunner,
		TemplateName:       fmt.Sprintf("test_template_main_%d_%d", time.Now().UnixNano(), os.Getpid()),
		TestDBPrefix:       fmt.Sprintf("test_db_main_%d_", time.Now().UnixNano()),
		AdminDBName:        "postgres",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Test initialization.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	// Verify template database was created.
	c.Assert(databaseExists(ctx, connProvider, config.TemplateName), qt.IsTrue)

	// Test creating test databases.
	testDB1, testDBName1, err := tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)
	c.Assert(testDB1, qt.IsNotNil)
	c.Assert(strings.HasPrefix(testDBName1, "test_db_"), qt.IsTrue)

	// Verify test database was created.
	c.Assert(databaseExists(ctx, connProvider, testDBName1), qt.IsTrue)

	// Test creating another test database.
	testDB2, testDBName2, err := tm.CreateTestDatabase(ctx, "custom_test_name")
	c.Assert(err, qt.IsNil)
	c.Assert(testDB2, qt.IsNotNil)
	c.Assert(testDBName2, qt.Equals, "custom_test_name")

	// Verify custom named test database was created.
	c.Assert(databaseExists(ctx, connProvider, testDBName2), qt.IsTrue)

	// Test that migrations were applied to test databases.
	c.Assert(hasTestTable(ctx, testDB1), qt.IsTrue)
	c.Assert(hasTestTable(ctx, testDB2), qt.IsTrue)

	// Clean up test databases.
	c.Assert(testDB1.Close(), qt.IsNil)
	c.Assert(testDB2.Close(), qt.IsNil)

	err = tm.DropTestDatabase(ctx, testDBName1)
	c.Assert(err, qt.IsNil)
	c.Assert(databaseExists(ctx, connProvider, testDBName1), qt.IsFalse)

	err = tm.DropTestDatabase(ctx, testDBName2)
	c.Assert(err, qt.IsNil)
	c.Assert(databaseExists(ctx, connProvider, testDBName2), qt.IsFalse)

	// Clean up template database.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.IsNil)
	c.Assert(databaseExists(ctx, connProvider, config.TemplateName), qt.IsFalse)

	// Verify system databases still exist and our test databases are gone.
	databases := listDatabases(ctx, c, connProvider)
	c.Assert(slices.Contains(databases, "postgres"), qt.IsTrue)
	c.Assert(slices.Contains(databases, "template0"), qt.IsTrue)
	c.Assert(slices.Contains(databases, "template1"), qt.IsTrue)
	// Verify our specific test databases are gone
	c.Assert(databaseExists(ctx, connProvider, config.TemplateName), qt.IsFalse)
	c.Assert(databaseExists(ctx, connProvider, testDBName1), qt.IsFalse)
	c.Assert(databaseExists(ctx, connProvider, testDBName2), qt.IsFalse)
}

// TestTemplateManagerConcurrentAccess tests concurrent usage.
func TestTemplateManagerConcurrentAccess(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	connProvider := setupTestConnectionProvider()
	migrationRunner := setupTestMigrationRunner(c)

	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    migrationRunner,
		TemplateName:       fmt.Sprintf("concurrent_template_%d_%d", time.Now().UnixNano(), os.Getpid()),
		TestDBPrefix:       fmt.Sprintf("concurrent_test_%d_", time.Now().UnixNano()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	// Create multiple test databases concurrently.
	const numGoroutines = 5
	results := make(chan string, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			testDB, testDBName, err := tm.CreateTestDatabase(ctx)
			if err != nil {
				errors <- err
				return
			}
			if err := testDB.Close(); err != nil {
				errors <- err
				return
			}
			results <- testDBName
		}(i)
	}

	// Collect results.
	var testDBNames []string
	for i := 0; i < numGoroutines; i++ {
		select {
		case name := <-results:
			testDBNames = append(testDBNames, name)
		case err := <-errors:
			c.Fatal(err)
		case <-ctx.Done():
			c.Fatal("timeout waiting for concurrent operations")
		}
	}

	c.Assert(len(testDBNames), qt.Equals, numGoroutines)

	// Verify all databases were created.
	for _, dbName := range testDBNames {
		c.Assert(databaseExists(ctx, connProvider, dbName), qt.IsTrue)
	}

	// Clean up concurrently.
	for _, dbName := range testDBNames {
		go func(name string) {
			err := tm.DropTestDatabase(ctx, name)
			if err != nil {
				errors <- err
				return
			}
			results <- name
		}(dbName)
	}

	// Wait for cleanup completion.
	for i := 0; i < len(testDBNames); i++ {
		select {
		case <-results:
		case err := <-errors:
			c.Fatal(err)
		case <-ctx.Done():
			c.Fatal("timeout waiting for cleanup")
		}
	}

	// Verify all test databases were dropped.
	for _, dbName := range testDBNames {
		c.Assert(databaseExists(ctx, connProvider, dbName), qt.IsFalse)
	}

	// Clean up template.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.IsNil)
}

// TestTemplateManagerValidation tests validation logic.
func TestTemplateManagerValidation(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	tests := []struct {
		name          string
		connString    string
		shouldSucceed bool
	}{
		{"Valid postgres URL", "postgres://user:pass@localhost/db", true},
		{"Valid postgres DSN", "postgres://localhost/db", true},
		{"Invalid MySQL URL", "mysql://user:pass@localhost/db", false},
		{"Invalid SQLite URL", "sqlite://test.db", false},
		{"Empty connection string", "", false},
	}

	for _, test := range tests {
		c.Run(test.name, func(c *qt.C) {
			provider := &mockConnectionProvider{connString: test.connString}
			config := pgdbtemplate.Config{
				ConnectionProvider: provider,
				MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
			}

			_, err := pgdbtemplate.NewTemplateManager(config)
			if test.shouldSucceed {
				c.Assert(err, qt.IsNil)
			} else {
				c.Assert(err, qt.IsNotNil)
			}
		})
	}
}

// Helper functions and test utilities.

// testConnectionStringFunc creates a connection string for the given database name.
func testConnectionStringFunc(dbName string) string {
	// Replace database name in connection string.
	parts := strings.Split(testConnString, "/")
	if len(parts) > 3 {
		parts[3] = strings.Split(parts[3], "?")[0] // Remove query params.
		parts[3] = dbName
	}
	result := strings.Join(parts, "/")
	if strings.Contains(testConnString, "?") {
		queryStart := strings.Index(testConnString, "?")
		result += testConnString[queryStart:]
	}
	return result
}

func setupTestConnectionProvider() pgdbtemplate.ConnectionProvider {
	return &realConnectionProvider{connStringFunc: testConnectionStringFunc}
}

func setupTestMigrationRunner(c *qt.C) pgdbtemplate.MigrationRunner {
	// Create temporary migration files.
	tempDir := c.TempDir()

	migration1 := `
CREATE TABLE test_table (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
`

	migration2 := `
INSERT INTO test_table (name) VALUES ('test_data_1'), ('test_data_2');
`

	migration1Path := tempDir + "/001_create_table.sql"
	migration2Path := tempDir + "/002_insert_data.sql"

	err := os.WriteFile(migration1Path, []byte(migration1), 0644)
	c.Assert(err, qt.IsNil)

	err = os.WriteFile(migration2Path, []byte(migration2), 0644)
	c.Assert(err, qt.IsNil)

	return pgdbtemplate.NewFileMigrationRunner([]string{tempDir}, pgdbtemplate.AlphabeticalMigrationFilesSorting)
}

func databaseExists(ctx context.Context, provider pgdbtemplate.ConnectionProvider, dbName string) bool {
	adminConn, err := provider.Connect(ctx, "postgres")
	if err != nil {
		return false
	}
	defer adminConn.Close()

	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)"
	row := adminConn.QueryRowContext(ctx, query, dbName)
	err = row.Scan(&exists)
	return err == nil && exists
}

func hasTestTable(ctx context.Context, conn pgdbtemplate.DatabaseConnection) bool {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'test_table')"
	row := conn.QueryRowContext(ctx, query)
	err := row.Scan(&exists)
	return err == nil && exists
}

func listDatabases(ctx context.Context, c *qt.C, provider pgdbtemplate.ConnectionProvider) []string {
	adminConn, err := provider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	rows, err := adminConn.(*pgdbtemplate.StandardDatabaseConnection).Query("SELECT datname FROM pg_database WHERE datistemplate = false")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(rows.Close(), qt.IsNil)
	}()

	var databases []string
	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		c.Assert(err, qt.IsNil)
		databases = append(databases, name)
	}
	c.Assert(rows.Err(), qt.IsNil)

	// Also include template databases for verification.
	rows2, err := adminConn.(*pgdbtemplate.StandardDatabaseConnection).Query("SELECT datname FROM pg_database WHERE datistemplate = true")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(rows2.Close(), qt.IsNil)
	}()

	for rows2.Next() {
		var name string
		err := rows2.Scan(&name)
		c.Assert(err, qt.IsNil)
		databases = append(databases, name)
	}
	c.Assert(rows.Err(), qt.IsNil)

	return databases
}

// Mock implementations for testing.

type mockConnectionProvider struct {
	connString string
}

func (m *mockConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	return nil, nil
}

func (m *mockConnectionProvider) GetConnectionString(databaseName string) string {
	return m.connString
}

// realConnectionProvider creates actual database connections for testing.
type realConnectionProvider struct {
	connStringFunc func(string) string
}

func (r *realConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	connString := r.connStringFunc(databaseName)
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() // Ignore close error since we're already in an error state
		return nil, err
	}

	return &pgdbtemplate.StandardDatabaseConnection{DB: db}, nil
}

func (r *realConnectionProvider) GetConnectionString(databaseName string) string {
	return r.connStringFunc(databaseName)
}
