package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

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

	// Note: Using mock provider, no real databases created - cleanup not needed
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

	// Note: Using mock provider, no real databases created - cleanup not needed.
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

	// Note: Using mock provider, no real databases created - cleanup not needed.
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

	// Note: Using mock provider, no real databases created - cleanup not needed.
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

	// Note: Using mock provider, no real databases created - cleanup not needed.
}

func setupTestConnectionProvider() pgdbtemplate.ConnectionProvider {
	return NewMockConnectionProvider()
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

// mockConnectionProvider is a mock implementation of ConnectionProvider
// for testing.
type mockConnectionProvider struct {
	connString string
	databases  map[string]bool
	mu         sync.RWMutex
}

// NewMockConnectionProvider creates a new mock connection provider.
func NewMockConnectionProvider() *mockConnectionProvider {
	return &mockConnectionProvider{
		databases: map[string]bool{
			"postgres":  true,
			"template0": true,
			"template1": true,
		},
	}
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (m *mockConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	return &tmMockDatabaseConnection{provider: m, dbName: databaseName}, nil
}

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (m *mockConnectionProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
}

// tmMockDatabaseConnection is a mock implementation of DatabaseConnection.
type tmMockDatabaseConnection struct {
	provider *mockConnectionProvider
	dbName   string
}

// ExecContext implements pgdbtemplate.DatabaseConnection.ExecContext.
func (m *tmMockDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (any, error) {
	if strings.Contains(query, "CREATE DATABASE") {
		parts := strings.Fields(query)
		if len(parts) >= 3 {
			dbName := strings.Trim(parts[2], `"`)
			m.provider.mu.Lock()
			if m.provider.databases == nil {
				m.provider.databases = make(map[string]bool)
			}
			if m.provider.databases[dbName] {
				m.provider.mu.Unlock()
				return nil, fmt.Errorf("database \"%s\" already exists", dbName)
			}
			m.provider.databases[dbName] = true
			m.provider.mu.Unlock()
		}
	} else if strings.Contains(query, "DROP DATABASE") {
		parts := strings.Fields(query)
		var dbName string
		if len(parts) >= 5 && parts[2] == "IF" && parts[3] == "EXISTS" {
			// Handle "DROP DATABASE IF EXISTS dbname"
			dbName = strings.Trim(parts[4], `"`)
		} else if len(parts) >= 3 {
			// Handle "DROP DATABASE dbname"
			dbName = strings.Trim(parts[2], `"`)
		}
		if dbName != "" {
			m.provider.mu.Lock()
			if m.provider.databases != nil && m.provider.databases[dbName] {
				delete(m.provider.databases, dbName)
			} else if !(len(parts) >= 5 && parts[2] == "IF" && parts[3] == "EXISTS") {
				// Only error if it's not an "IF EXISTS" query
				m.provider.mu.Unlock()
				return nil, fmt.Errorf("database \"%s\" does not exist", dbName)
			}
			m.provider.mu.Unlock()
		}
	}
	return nil, nil
}

// QueryRowContext implements pgdbtemplate.DatabaseConnection.QueryRowContext.
func (m *tmMockDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) pgdbtemplate.Row {
	if strings.Contains(query, "SELECT datname FROM pg_database WHERE NOT datistemplate") {
		var dbs []any
		m.provider.mu.RLock()
		for db, exists := range m.provider.databases {
			if exists && db != "template0" && db != "template1" {
				dbs = append(dbs, db)
			}
		}
		m.provider.mu.RUnlock()
		return &mockRow{data: dbs}
	}
	if strings.Contains(query, "SELECT datname FROM pg_database WHERE datistemplate") {
		var dbs []any
		m.provider.mu.RLock()
		for db, exists := range m.provider.databases {
			if exists && (db == "template0" || db == "template1") {
				dbs = append(dbs, db)
			}
		}
		m.provider.mu.RUnlock()
		return &mockRow{data: dbs}
	}
	if strings.Contains(query, "SELECT TRUE FROM pg_database WHERE datname") {
		// Handle both parameterized and literal queries
		if len(args) > 0 {
			// Parameterized query: SELECT TRUE FROM pg_database WHERE datname = $1
			if dbName, ok := args[0].(string); ok {
				m.provider.mu.RLock()
				exists := m.provider.databases[dbName]
				m.provider.mu.RUnlock()
				if exists {
					return &mockRow{data: []any{true}}
				}
			}
		} else if strings.Contains(query, "WHERE datname =") {
			// Literal query: SELECT TRUE FROM pg_database WHERE datname = 'db_name'
			parts := strings.Split(query, "WHERE datname =")
			if len(parts) == 2 {
				dbNameWithQuotes := strings.TrimSpace(parts[1])
				dbNameWithQuotes = strings.TrimSuffix(dbNameWithQuotes, " LIMIT 1")
				dbName := strings.Trim(dbNameWithQuotes, "'")
				m.provider.mu.RLock()
				exists := m.provider.databases[dbName]
				m.provider.mu.RUnlock()
				if exists {
					return &mockRow{data: []any{true}}
				}
			}
		}
		return &mockRow{err: sql.ErrNoRows}
	}
	if strings.Contains(query, "SELECT 1 FROM pg_database") {
		if len(args) > 0 {
			if dbName, ok := args[0].(string); ok {
				m.provider.mu.RLock()
				exists := m.provider.databases[dbName]
				m.provider.mu.RUnlock()
				if exists {
					return &mockRow{data: []any{1}}
				}
			}
		}
		return &mockRow{err: sql.ErrNoRows}
	}
	if strings.Contains(query, "SELECT 1 FROM information_schema.tables") {
		// Assume test_table exists if db exists
		m.provider.mu.RLock()
		exists := m.provider.databases[m.dbName]
		m.provider.mu.RUnlock()
		if exists {
			return &mockRow{data: []any{1}}
		}
		return &mockRow{err: sql.ErrNoRows}
	}
	return &mockRow{}
}

// Close implements pgdbtemplate.DatabaseConnection.Close.
func (*tmMockDatabaseConnection) Close() error {
	return nil
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

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (*oneTimeFailProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
}

// mockRow is a mock implementation of pgdbtemplate.Row.
type mockRow struct {
	data  []any
	err   error
	index int
}

// Scan implements pgdbtemplate.Row.Scan.
func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if r.index+i < len(r.data) {
			switch v := d.(type) {
			case *string:
				*v = r.data[r.index+i].(string)
			case *int:
				*v = r.data[r.index+i].(int)
			}
		}
	}
	r.index += len(dest)
	return nil
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

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (*mockDropTemplateDBProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
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
