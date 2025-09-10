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
	// Verify our specific test databases are gone.
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
	testDBNames := make([]string, 0, numGoroutines)
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
	cleanupResults := make(chan string, len(testDBNames))
	cleanupErrors := make(chan error, len(testDBNames))

	for _, dbName := range testDBNames {
		go func(name string) {
			err := tm.DropTestDatabase(ctx, name)
			if err != nil {
				cleanupErrors <- err
				return
			}
			cleanupResults <- name
		}(dbName)
	}

	// Wait for cleanup completion.
	for i := 0; i < len(testDBNames); i++ {
		select {
		case <-cleanupResults:
		case err := <-cleanupErrors:
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
			c.Assert(err != nil, qt.Equals, !test.shouldSucceed)
		})
	}
}

// TestTemplateManagerErrors tests error conditions in template manager.
func TestTemplateManagerErrors(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	c.Run("Template name generation", func(c *qt.C) {
		provider := &mockConnectionProvider{connString: "postgres://localhost/test"}

		// Without template name - should auto-generate.
		config1 := pgdbtemplate.Config{
			ConnectionProvider: provider,
			MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		}
		_, err := pgdbtemplate.NewTemplateManager(config1)
		c.Assert(err, qt.IsNil)

		// With custom template name.
		config2 := pgdbtemplate.Config{
			ConnectionProvider: provider,
			MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
			TemplateName:       "custom_template",
		}
		_, err = pgdbtemplate.NewTemplateManager(config2)
		c.Assert(err, qt.IsNil)
	})

	c.Run("Missing ConnectionProvider", func(c *qt.C) {
		config := pgdbtemplate.Config{
			// No ConnectionProvider.
			MigrationRunner: &pgdbtemplate.NoOpMigrationRunner{},
		}
		_, err := pgdbtemplate.NewTemplateManager(config)
		c.Assert(err, qt.ErrorMatches, ".*ConnectionProvider.*required.*")
	})

	c.Run("Missing MigrationRunner", func(c *qt.C) {
		provider := &mockConnectionProvider{connString: "postgres://localhost/test"}
		config := pgdbtemplate.Config{
			ConnectionProvider: provider,
			// No MigrationRunner.
		}
		_, err := pgdbtemplate.NewTemplateManager(config)
		c.Assert(err, qt.ErrorMatches, ".*MigrationRunner.*required.*")
	})
}

// TestDropTestDatabaseError tests error handling in DropTestDatabase.
func TestDropTestDatabaseError(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	connProvider := setupTestConnectionProvider()
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       fmt.Sprintf("drop_error_template_%d_%d", time.Now().UnixNano(), os.Getpid()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Initialize the template so DropTestDatabase can work.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(tm.Cleanup(ctx), qt.IsNil)
	}()

	// Try to drop a non-existent database - this should error since
	// DROP DATABASE is used (not IF EXISTS).
	err = tm.DropTestDatabase(ctx, "non_existent_db_12345")
	c.Assert(err, qt.ErrorMatches, ".*does not exist.*")
}

// TestCreateTestDatabaseWithExistingName tests creating a database with an existing name.
func TestCreateTestDatabaseWithExistingName(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	connProvider := setupTestConnectionProvider()
	migrationRunner := setupTestMigrationRunner(c)

	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    migrationRunner,
		TemplateName:       fmt.Sprintf("existing_name_template_%d_%d", time.Now().UnixNano(), os.Getpid()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(tm.Cleanup(ctx), qt.IsNil)
	}()

	// Create first database with custom name.
	testDBName := fmt.Sprintf("existing_test_%d_%d", time.Now().UnixNano(), os.Getpid())
	testDB1, _, err := tm.CreateTestDatabase(ctx, testDBName)
	c.Assert(err, qt.IsNil)
	c.Assert(testDB1.Close(), qt.IsNil)

	// Try to create second database with same name -
	// should fail with "already exists" error.
	_, _, err = tm.CreateTestDatabase(ctx, testDBName)
	c.Assert(err, qt.ErrorMatches, ".*already exists.*")
}

// TestTemplateManagerCleanupErrorPaths tests error handling in Cleanup
// when DROP DATABASE fails.
func TestTemplateManagerCleanupErrorPaths(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	config := pgdbtemplate.Config{
		ConnectionProvider: &mockDropTemplateDBProvider{failDrop: true},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "template_cleanup_error",
		TestDBPrefix:       "test_template_cleanup_error_",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Simulate initialization and creation of test DBs.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx) // Should be no-op on re-initialization.
	c.Assert(err, qt.IsNil)

	_, _, err = tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)

	// Cleanup should fail due to DROP DATABASE error.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.ErrorMatches, "(?s).*drop error.*drop error.*")

	// TODO: cleanup created template and test databases.
}

// TestTemplateManagerDropTestDatabaseErrorPaths tests error handling in DropTestDatabase
// when pg_terminate_backend and DROP DATABASE fail.
func TestTemplateManagerDropTestDatabaseErrorPaths(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	config := pgdbtemplate.Config{
		ConnectionProvider: &mockDropTemplateDBProvider{failTerminate: true, failDrop: true},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "template_drop_error",
		TestDBPrefix:       "test_drop_error_",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	_, testDBName, err := tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)

	// DropTestDatabase should fail due to terminate error (first step).
	err = tm.DropTestDatabase(ctx, testDBName)
	c.Assert(err, qt.ErrorMatches, ".*terminate error.*")

	// TODO: cleanup created template and test databases.
}

func TestDropTemplateDatabaseConnectionError(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	tm, err := pgdbtemplate.NewTemplateManager(pgdbtemplate.Config{
		ConnectionProvider: &mockDropTemplateDBProvider{failConnect: true},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "template_db_conn_error",
	})
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.ErrorMatches, ".*connect error.*")

	_, _, err = tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.ErrorMatches, ".*connect error.*")

	err = tm.DropTestDatabase(ctx, "any_db")
	c.Assert(err, qt.ErrorMatches, ".*connect error.*")
}

func TestDropTemplateDatabaseUnmarkError(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	tm, err := pgdbtemplate.NewTemplateManager(pgdbtemplate.Config{
		ConnectionProvider: &mockDropTemplateDBProvider{failUnmark: true},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "template_db_unmark_error",
	})
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	err = tm.Cleanup(ctx)
	c.Assert(err, qt.ErrorMatches, ".*unmark error.*")

	// TODO: cleanup created template database.
}

func TestDropTemplateDatabaseQueryRowError(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	tm, err := pgdbtemplate.NewTemplateManager(pgdbtemplate.Config{
		ConnectionProvider: &mockDropTemplateDBProvider{failQueryRow: true},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "template_db_queryrow_error",
	})
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.ErrorMatches, ".*queryrow error.*")
}

func TestDropTemplateDatabaseBatchTerminateFailure(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	tm, err := pgdbtemplate.NewTemplateManager(pgdbtemplate.Config{
		ConnectionProvider: &mockDropTemplateDBProvider{failTerminate: true},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "template_batch_terminate_fail",
	})
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)
	_, _, err = tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)

	// Cleanup should report terminate error.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.ErrorMatches, ".*terminate error.*")

	// TODO: cleanup created template and test databases.
}

func TestInitializeCreateError(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	tm, err := pgdbtemplate.NewTemplateManager(pgdbtemplate.Config{
		ConnectionProvider: &mockDropTemplateDBProvider{failCreate: true, nonExistentQueryRow: true},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "fail_create_template",
	})
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.ErrorMatches, ".*failed to create template database.*create error.*")
}

func TestCleanupAdminConnectError(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	tm, err := pgdbtemplate.NewTemplateManager(pgdbtemplate.Config{
		ConnectionProvider: &oneTimeFailProvider{},
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "cleanup_admin_connect_error",
	})
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	err = tm.Cleanup(ctx)
	c.Assert(err, qt.ErrorMatches, "failed to connect to admin database:.*connect error for admin db")

	// TODO: cleanup created template database.
}

// testConnectionStringFunc creates a connection string for the given database name.
func testConnectionStringFunc(dbName string) string {
	// Use the package-level connection string from init().
	return pgdbtemplate.ReplaceDatabaseInConnectionString(testConnectionString, dbName)
}

func setupTestConnectionProvider() pgdbtemplate.ConnectionProvider {
	return pgdbtemplate.NewStandardConnectionProvider(testConnectionStringFunc)
}

func setupTestMigrationRunner(c *qt.C) pgdbtemplate.MigrationRunner {
	// Create temporary migration files.
	tempDir := c.TempDir()

	migration1 := `
	CREATE TABLE test_table (
		id SERIAL PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		created_at TIMESTAMP DEFAULT NOW()
	);`

	migration2 := `
	INSERT INTO test_table (name)
	VALUES ('test_data_1'), ('test_data_2');`

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

	var exists int
	query := "SELECT 1 FROM pg_database WHERE datname = $1 LIMIT 1"
	err = adminConn.QueryRowContext(ctx, query, dbName).Scan(&exists)
	return err == nil
}

func hasTestTable(ctx context.Context, conn pgdbtemplate.DatabaseConnection) bool {
	var exists int
	query := `
	SELECT 1
	FROM information_schema.tables
	WHERE table_name = 'test_table'
	LIMIT 1`
	err := conn.QueryRowContext(ctx, query).Scan(&exists)
	return err == nil
}

func listDatabases(ctx context.Context, c *qt.C, provider pgdbtemplate.ConnectionProvider) []string {
	adminConn, err := provider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	standardDBConn, ok := adminConn.(*pgdbtemplate.StandardDatabaseConnection)
	c.Assert(ok, qt.IsTrue)

	rows, err := standardDBConn.Query(
		"SELECT datname FROM pg_database WHERE NOT datistemplate",
	)
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
	rows2, err := standardDBConn.Query(
		"SELECT datname FROM pg_database WHERE datistemplate",
	)
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

// mockConnectionProvider is a mock implementation of ConnectionProvider
// for testing.
type mockConnectionProvider struct {
	connString string
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (*mockConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	return nil, nil
}

// GetConnectionString implements pgdbtemplate.ConnectionProvider.GetConnectionString.
func (m *mockConnectionProvider) GetConnectionString(databaseName string) string {
	return m.connString
}

// oneTimeFailProvider is a ConnectionProvider
// that fails on the second Connect call.
type oneTimeFailProvider struct {
	calls int
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (p *oneTimeFailProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	p.calls++
	if p.calls == 1 {
		// First call (Initialize) succeeds.
		return &mockDropTemplateDBConnection{}, nil
	}
	// Second call (Cleanup) fails.
	return nil, fmt.Errorf("connect error for admin db")
}

// GetConnectionString implements pgdbtemplate.ConnectionProvider.GetConnectionString.
func (p *oneTimeFailProvider) GetConnectionString(databaseName string) string {
	return "postgres://user:password@localhost:5432/dbname"
}

// mockRow is a mock implementation of pgdbtemplate.Row.
type mockRow struct {
	err error
}

// Scan implements pgdbtemplate.Row.Scan.
func (r *mockRow) Scan(dest ...any) error {
	return r.err
}

// mockDropTemplateDBProvider simulates errors
// during template database drop.
type mockDropTemplateDBProvider struct {
	failConnect         bool // Simulate connection error.
	failUnmark          bool // Simulate error when unmarking template.
	failDrop            bool // Simulate error when dropping database.
	failTerminate       bool // Simulate error when terminating connections.
	failQueryRow        bool // Simulate error in QueryRowContext.
	nonExistentQueryRow bool // Simulate query for non-existent database.
	failClose           bool // Simulate error in Close.
	failMultiDrop       bool // Simulate multiple drop errors in cleanup.
	failCreate          bool // Simulate error when creating database.
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (m *mockDropTemplateDBProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	if m.failConnect {
		return nil, fmt.Errorf("connect error")
	}
	return &mockDropTemplateDBConnection{
		failUnmark:          m.failUnmark,
		failDrop:            m.failDrop,
		failTerminate:       m.failTerminate,
		failQueryRow:        m.failQueryRow,
		nonExistentQueryRow: m.nonExistentQueryRow,
		failClose:           m.failClose,
		failMultiDrop:       m.failMultiDrop,
		failCreate:          m.failCreate,
	}, nil
}

// GetConnectionString implements pgdbtemplate.ConnectionProvider.GetConnectionString.
func (m *mockDropTemplateDBProvider) GetConnectionString(databaseName string) string {
	return "postgres://user:password@localhost:5432/dbname" // Mock value.
}

// mockDropTemplateDBConnection simulates errors
// during template database operations.
type mockDropTemplateDBConnection struct {
	failUnmark          bool // Simulate error when unmarking template.
	failDrop            bool // Simulate error when dropping database.
	failTerminate       bool // Simulate error when terminating connections.
	failQueryRow        bool // Simulate error in QueryRowContext.
	nonExistentQueryRow bool // Simulate query for non-existent database.
	failClose           bool // Simulate error in Close.
	failMultiDrop       bool // Simulate multiple drop errors in cleanup.
	failCreate          bool // Simulate error when creating database.
}

// ExecContext implements pgdbtemplate.DatabaseConnection.ExecContext.
func (m *mockDropTemplateDBConnection) ExecContext(ctx context.Context, query string, args ...any) (any, error) {
	if m.failUnmark && strings.Contains(query, "is_template FALSE") {
		return nil, fmt.Errorf("unmark error")
	}
	if m.failTerminate && strings.Contains(query, "pg_terminate_backend") {
		return nil, fmt.Errorf("terminate error")
	}
	if m.failMultiDrop && strings.Contains(query, "DROP DATABASE") {
		return nil, fmt.Errorf("multi drop error")
	}
	if m.failDrop && strings.Contains(query, "DROP DATABASE") {
		return nil, fmt.Errorf("drop error")
	}
	if m.failCreate && strings.Contains(query, "CREATE DATABASE") {
		return nil, fmt.Errorf("create error")
	}
	return nil, nil
}

// QueryRowContext implements pgdbtemplate.DatabaseConnection.QueryRowContext.
func (m *mockDropTemplateDBConnection) QueryRowContext(ctx context.Context, query string, args ...any) pgdbtemplate.Row {
	if m.failQueryRow {
		return &mockRow{err: fmt.Errorf("queryrow error")}
	}
	if m.nonExistentQueryRow {
		return &mockRow{err: sql.ErrNoRows}
	}
	return &mockRow{}
}

// Close implements pgdbtemplate.DatabaseConnection.Close.
func (m *mockDropTemplateDBConnection) Close() error {
	if m.failClose {
		return fmt.Errorf("close error")
	}
	return nil
}
