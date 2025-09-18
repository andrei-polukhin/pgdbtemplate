package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestCreateTestDatabaseCleanupOnConnectionFailure tests that a test database
// is dropped if it's created successfully but connection to it fails.
func TestCreateTestDatabaseCleanupOnConnectionFailure(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	// Create a connection provider that fails when connecting to test databases
	// but works for admin operations and template.
	failingProvider := &testDatabaseConnectionFailProvider{
		adminDBName:  "postgres",
		templateName: "cleanup_test_template_test", // This should work.
		failPattern:  "test_test_db_cleanup_",      // Fail only for test databases with this prefix.
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "cleanup_test_template_test",
		TestDBPrefix:       "test_test_db_cleanup_",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Initialize should succeed - creates template database and connects to it.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	// This should fail because connection to test database fails,
	// but the test database should be automatically dropped.
	_, _, err = tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.ErrorMatches, ".*failed to connect to test database.*")

	// Verify the test database was dropped (doesn't exist).
	adminConn, err := failingProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	// Check that no test databases with our prefix exist.
	var count int
	checkQuery := "SELECT COUNT(*) FROM pg_database WHERE datname LIKE $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, "test_cleanup_%").Scan(&count)
	c.Assert(err, qt.IsNil)
	c.Assert(count, qt.Equals, 0)
}

// TestCreateTestDatabaseCleanupOnConnectionFailureWithDropFailure tests
// that a test database tried to drop, but this fails, the error is reported.
func TestCreateTestDatabaseCleanupOnConnectionFailureWithDropFailure(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	// Create a connection provider that fails when connecting to test databases
	// but works for admin operations and template.
	failingProvider := &testDatabaseConnectionFailProvider{
		adminDBName:  "postgres",
		templateName: "cleanup_test_template_with_drop_test", // This should work.
		failPattern:  "test_test_db_with_drop_cleanup_",      // Fail only for test databases with this prefix.
		failDrop:     true,                                   // Simulate error on DROP DATABASE.
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "cleanup_test_template_with_drop_test",
		TestDBPrefix:       "test_test_db_with_drop_cleanup_",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Initialize should succeed - creates template database and connects to it.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	// This should fail because connection to test database fails,
	// but the test database should be automatically dropped.
	_, _, err = tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.ErrorMatches, "(?s).*failed to connect to test database.*")

	// Verify the test database was not dropped.
	adminConn, err := failingProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	// Check that test database with our prefix still exist.
	var name string
	checkQuery := "SELECT datname FROM pg_database WHERE datname LIKE $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, "test_test_db_with_drop_cleanup_%").Scan(&name)
	c.Assert(err, qt.IsNil)

	// Cleanup the database manually to avoid leftover.
	realProvider := createRealConnectionProvider()
	adminConn, err = realProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	_, err = adminConn.ExecContext(ctx, "DROP DATABASE "+name)
	c.Assert(err, qt.IsNil)
}

// TestCreateTemplateDatabaseCleanupOnConnectionFailure tests that a template
// database is dropped if it's created successfully but connection to it fails.
func TestCreateTemplateDatabaseCleanupOnConnectionFailure(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	templateName := "test_template_conn_fail"

	// Create a connection provider that fails when connecting to the template
	// database.
	failingProvider := &templateConnectionFailProvider{
		adminDBName:  "postgres",
		templateName: templateName,
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       templateName,
		TestDBPrefix:       "test_test_db_conn_fail_",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// This should fail because connection to template database fails,
	// but the template database should be automatically dropped.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.ErrorMatches, ".*failed to connect to template database.*")

	// Verify the template database was dropped (doesn't exist).
	adminConn, err := failingProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	// Check that the template database doesn't exist.
	var exists bool
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	c.Assert(err, qt.ErrorIs, sql.ErrNoRows)
}

// TestCreateTemplateDatabaseCleanupOnMigrationFailure tests that a template
// database is dropped if it's created successfully but migration fails.
func TestCreateTemplateDatabaseCleanupOnMigrationFailure(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	templateName := "test_template_migration_fail"
	connProvider := createRealConnectionProvider()

	// Create a migration runner that always fails.
	failingMigrator := &failingMigrationRunner{
		errorMsg: "intentional migration failure",
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    failingMigrator,
		TemplateName:       templateName,
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// This should fail because migrations fail,
	// but the template database should be automatically dropped.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.ErrorMatches, ".*failed to run migrations on template.*intentional migration failure")

	// Verify the template database was dropped (doesn't exist).
	adminConn, err := connProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	// Check that the template database doesn't exist.
	var exists bool
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	c.Assert(err, qt.ErrorIs, sql.ErrNoRows)
}

// TestCreateTemplateDatabaseCleanupOnMarkTemplateFailure tests that a template
// database is dropped if it's created and migrations succeed but marking as
// template fails.
func TestCreateTemplateDatabaseCleanupOnMarkTemplateFailure(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	templateName := fmt.Sprintf("test_template_mark_fail_%d", time.Now().UnixNano())

	// Ensure the template database doesn't exist beforehand.
	realProvider := createRealConnectionProvider()
	adminConn, err := realProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", templateName)
	_, err = adminConn.ExecContext(ctx, dropQuery)
	c.Assert(err, qt.IsNil)
	err = adminConn.Close()
	c.Assert(err, qt.IsNil)

	// Create a connection provider that fails when executing
	// ALTER DATABASE ... WITH is_template TRUE.
	failingProvider := &markTemplateFailProvider{
		adminDBName: "postgres",
		failOnAlter: true,
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       templateName,
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// This should fail because marking as template fails,
	// but the template database should be automatically dropped.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.ErrorMatches, ".*failed to mark database as template.*intentional mark template failure.*")

	// Verify the template database was dropped (doesn't exist).
	adminConn, err = failingProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	// Check that the template database doesn't exist.
	var exists bool
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	c.Assert(err, qt.ErrorIs, sql.ErrNoRows)
}

// TestCreateTemplateDatabaseCleanupOnMarkTemplateFailureWithDropFailure tests
// when the template database is created and migrations succeed but marking as
// template fails, and then dropping the database also fails, the error is reported.
func TestCreateTemplateDatabaseCleanupOnMarkTemplateFailureWithDropFailure(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()
	templateName := "test_template_mark_fail_with_drop_failure"

	// Create a connection provider that fails when executing
	// ALTER DATABASE ... WITH is_template TRUE. Then, it will
	// also fail when dropping the database.
	failingProvider := &markTemplateFailProvider{
		adminDBName: "postgres",
		failDrop:    true,
		failOnAlter: true,
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       templateName,
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// This should fail because marking as template fails,
	// and also dropping the template database will fail.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.ErrorMatches, "(?s).*failed to mark database as template.*intentional mark template failure.*failed to drop template database.*intentional drop database failure")

	// Verify the template database was dropped (doesn't exist).
	adminConn, err := failingProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(adminConn.Close(), qt.IsNil)
	}()

	// Check that the template database exists.
	var exists bool
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	c.Assert(err, qt.IsNil)

	// Ensure the template database doesn't exist beforehand.
	realProvider := createRealConnectionProvider()
	adminConn, err = realProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", templateName)
	_, err = adminConn.ExecContext(ctx, dropQuery)
	c.Assert(err, qt.IsNil)
	err = adminConn.Close()
	c.Assert(err, qt.IsNil)
}

// Helper function to create a test connection provider for testing.
func createRealConnectionProvider() pgdbtemplate.ConnectionProvider {
	return &cleanupMockConnectionProvider{
		databases: map[string]bool{
			"postgres":  true,
			"template0": true,
			"template1": true,
		},
	}
}

// testDatabaseConnectionFailProvider fails when connecting to databases
// matching failPattern.
type testDatabaseConnectionFailProvider struct {
	adminDBName  string
	templateName string
	failPattern  string
	failOnAlter  bool
	failDrop     bool
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (p *testDatabaseConnectionFailProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	// Allow template database to work.
	if databaseName == p.templateName {
		return createRealConnectionProvider().Connect(ctx, databaseName)
	}

	// Allow admin database to work, but check for drop flag.
	if databaseName == p.adminDBName {
		realConn, err := createRealConnectionProvider().Connect(ctx, databaseName)
		if err != nil {
			return nil, err
		}
		// Wrap admin connection to intercept DROP DATABASE commands.
		if p.failDrop || p.failOnAlter {
			return &markTemplateFailConnection{
				DatabaseConnection: realConn,
				queryCount:         0,
				failOnDrop:         p.failDrop,
				failOnAlter:        p.failOnAlter,
			}, nil
		}
		return realConn, nil
	}

	// Fail for databases matching the fail pattern.
	if len(p.failPattern) > 0 && len(databaseName) >= len(p.failPattern) && databaseName[:len(p.failPattern)] == p.failPattern {
		return nil, fmt.Errorf("intentional connection failure for test database: %s", databaseName)
	}

	// For other databases, use real connection.
	return createRealConnectionProvider().Connect(ctx, databaseName)
}

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (*testDatabaseConnectionFailProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
}

// templateConnectionFailProvider fails when connecting to a specific
// template database.
type templateConnectionFailProvider struct {
	adminDBName  string
	templateName string
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (p *templateConnectionFailProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	if databaseName == p.templateName {
		return nil, fmt.Errorf("intentional connection failure for template database: %s", databaseName)
	}

	// For admin database, use real connection.
	return createRealConnectionProvider().Connect(ctx, databaseName)
}

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (*templateConnectionFailProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
}

// markTemplateFailProvider fails when executing
// ALTER DATABASE ... WITH is_template TRUE.
type markTemplateFailProvider struct {
	adminDBName string
	failDrop    bool
	failOnAlter bool
	provider    pgdbtemplate.ConnectionProvider
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (p *markTemplateFailProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	if p.provider == nil {
		p.provider = createRealConnectionProvider()
	}
	realConn, err := p.provider.Connect(ctx, databaseName)
	if err != nil {
		return nil, err
	}

	if databaseName == p.adminDBName {
		// Wrap admin connection to intercept ALTER DATABASE commands.
		return &markTemplateFailConnection{
			DatabaseConnection: realConn,
			queryCount:         0,
			failOnDrop:         p.failDrop,
			failOnAlter:        p.failOnAlter,
		}, nil
	}

	return realConn, nil
}

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (*markTemplateFailProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
}

// markTemplateFailConnection wraps a connection and fails on
// ALTER DATABASE ... WITH is_template TRUE.
type markTemplateFailConnection struct {
	pgdbtemplate.DatabaseConnection
	queryCount  int
	failOnAlter bool
	failOnDrop  bool
}

// ExecContext implements pgdbtemplate.DatabaseConnection.ExecContext.
func (c *markTemplateFailConnection) ExecContext(ctx context.Context, query string, args ...any) (any, error) {
	// Look for ALTER DATABASE statements with is_template TRUE.
	if c.failOnAlter && strings.Contains(query, "ALTER DATABASE") && strings.Contains(query, "is_template TRUE") {
		return nil, fmt.Errorf("intentional mark template failure")
	}
	if c.failOnDrop && strings.Contains(query, "DROP DATABASE") {
		return nil, fmt.Errorf("intentional drop database failure")
	}
	return c.DatabaseConnection.ExecContext(ctx, query, args...)
}

// failingMigrationRunner always fails with the specified error.
type failingMigrationRunner struct {
	errorMsg string
}

// RunMigrations implements pgdbtemplate.MigrationRunner.RunMigrations.
func (r *failingMigrationRunner) RunMigrations(ctx context.Context, conn pgdbtemplate.DatabaseConnection) error {
	return fmt.Errorf(r.errorMsg)
}

// cleanupMockConnectionProvider is a mock implementation of ConnectionProvider for testing.
type cleanupMockConnectionProvider struct {
	databases map[string]bool
	mu        sync.RWMutex
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (m *cleanupMockConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	return &cleanupMockDatabaseConnection{provider: m, dbName: databaseName}, nil
}

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (*cleanupMockConnectionProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
}

// cleanupMockDatabaseConnection is a mock implementation of DatabaseConnection.
type cleanupMockDatabaseConnection struct {
	provider *cleanupMockConnectionProvider
	dbName   string
}

// ExecContext implements pgdbtemplate.DatabaseConnection.ExecContext.
func (m *cleanupMockDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (any, error) {
	if strings.Contains(query, "CREATE DATABASE") {
		// Extract db name
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
func (m *cleanupMockDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) pgdbtemplate.Row {
	if strings.Contains(query, "SELECT datname FROM pg_database WHERE NOT datistemplate") {
		var dbs []any
		m.provider.mu.RLock()
		for db, exists := range m.provider.databases {
			if exists && db != "template0" && db != "template1" {
				dbs = append(dbs, db)
			}
		}
		m.provider.mu.RUnlock()
		return &cleanupMockRow{data: dbs}
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
		return &cleanupMockRow{data: dbs}
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
					return &cleanupMockRow{data: []any{true}}
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
					return &cleanupMockRow{data: []any{true}}
				}
			}
		}
		return &cleanupMockRow{err: sql.ErrNoRows}
	}
	if strings.Contains(query, "SELECT 1 FROM pg_database") {
		if len(args) > 0 {
			if dbName, ok := args[0].(string); ok {
				m.provider.mu.RLock()
				exists := m.provider.databases[dbName]
				m.provider.mu.RUnlock()
				if exists {
					return &cleanupMockRow{data: []any{1}}
				}
			}
		}
		return &cleanupMockRow{err: sql.ErrNoRows}
	}
	if strings.Contains(query, "SELECT 1 FROM information_schema.tables") {
		// Assume test_table exists if db exists
		m.provider.mu.RLock()
		exists := m.provider.databases[m.dbName]
		m.provider.mu.RUnlock()
		if exists {
			return &cleanupMockRow{data: []any{1}}
		}
		return &cleanupMockRow{err: sql.ErrNoRows}
	}
	return &cleanupMockRow{}
}

// Close implements pgdbtemplate.DatabaseConnection.Close.
func (*cleanupMockDatabaseConnection) Close() error {
	return nil
}

// cleanupMockRow is a mock implementation of pgdbtemplate.Row.
type cleanupMockRow struct {
	data []any
	err  error
}

func (r *cleanupMockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i < len(r.data) {
			switch v := d.(type) {
			case *string:
				*v = r.data[i].(string)
			case *int:
				*v = r.data[i].(int)
			}
		}
	}
	return nil
}
