package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestCreateTestDatabaseCleanupOnConnectionFailure tests that a test database
// is dropped if it's created successfully but connection to it fails.
func TestCreateTestDatabaseCleanupOnConnectionFailure(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	// Create a connection provider that fails when connecting to test databases
	// but works for admin operations and template.
	failingProvider := &testDatabaseConnectionFailProvider{
		adminDBName:  "postgres",
		templateName: "cleanup_template_test", // This should work.
		failPattern:  "test_cleanup_",         // Fail only for test databases with this prefix.
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       "cleanup_template_test",
		TestDBPrefix:       "test_cleanup_",
		AdminDBName:        "postgres",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Initialize should succeed - creates template database and connects to it.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	// This should fail because connection to test database fails,
	// but the test database should be automatically dropped.
	_, _, err = tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNotNil)
	c.Assert(err, qt.ErrorMatches, ".*failed to connect to test database.*")

	// Verify the test database was dropped (doesn't exist).
	adminConn, err := failingProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	defer adminConn.Close()

	// Check that no test databases with our prefix exist.
	var count int
	checkQuery := "SELECT COUNT(*) FROM pg_database WHERE datname LIKE $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, "test_cleanup_%").Scan(&count)
	c.Assert(err, qt.IsNil)
	c.Assert(count, qt.Equals, 0, qt.Commentf("Expected 0 test databases, but found %d - cleanup failed", count))

	// Cleanup template.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.IsNil)
}

// TestCreateTemplateDatabaseCleanupOnConnectionFailure tests that a template database
// is dropped if it's created successfully but connection to it fails.
func TestCreateTemplateDatabaseCleanupOnConnectionFailure(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	templateName := "test_template_conn_fail"

	// Create a connection provider that fails when connecting to the template database.
	failingProvider := &templateConnectionFailProvider{
		adminDBName:  "postgres",
		templateName: templateName,
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       templateName,
		TestDBPrefix:       "test_",
		AdminDBName:        "postgres",
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
	defer adminConn.Close()

	// Check that the template database doesn't exist.
	var exists bool
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	c.Assert(err, qt.Equals, sql.ErrNoRows, qt.Commentf("Template database should not exist after failed initialization"))
}

// TestCreateTemplateDatabaseCleanupOnMigrationFailure tests that a template database
// is dropped if it's created successfully but migration fails.
func TestCreateTemplateDatabaseCleanupOnMigrationFailure(t *testing.T) {
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
		TestDBPrefix:       "test_",
		AdminDBName:        "postgres",
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
	defer adminConn.Close()

	// Check that the template database doesn't exist.
	var exists bool
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	c.Assert(err, qt.Equals, sql.ErrNoRows, qt.Commentf("Template database should not exist after failed migration"))
}

// TestCreateTemplateDatabaseCleanupOnMarkTemplateFailure tests that a template database
// is dropped if it's created and migrations succeed but marking as template fails.
func TestCreateTemplateDatabaseCleanupOnMarkTemplateFailure(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	templateName := fmt.Sprintf("test_template_mark_fail_%d", time.Now().UnixNano())

	// Ensure the template database doesn't exist beforehand.
	realProvider := createRealConnectionProvider()
	adminConn, err := realProvider.Connect(ctx, "postgres")
	c.Assert(err, qt.IsNil)
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", templateName)
	adminConn.ExecContext(ctx, dropQuery) // Ignore errors.
	adminConn.Close()

	// Create a connection provider that fails when executing ALTER DATABASE ... WITH is_template TRUE.
	failingProvider := &markTemplateFailProvider{
		adminDBName: "postgres",
	}

	config := pgdbtemplate.Config{
		ConnectionProvider: failingProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       templateName,
		TestDBPrefix:       "test_",
		AdminDBName:        "postgres",
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
	defer adminConn.Close()

	// Check that the template database doesn't exist.
	var exists bool
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1"
	err = adminConn.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	c.Assert(err, qt.Equals, sql.ErrNoRows, qt.Commentf("Template database should not exist after failed mark template"))
}

// Helper function to create a test connection provider for testing.
func createRealConnectionProvider() pgdbtemplate.ConnectionProvider {
	connString := os.Getenv("POSTGRES_CONNECTION_STRING")
	if connString == "" {
		connString = "postgres://postgres:password@localhost:5432/postgres?sslmode=disable"
	}

	connStringFunc := func(dbName string) string {
		// Replace database name in connection string.
		parts := strings.Split(connString, "/")
		if len(parts) > 3 {
			parts[3] = strings.Split(parts[3], "?")[0] // Remove query params.
			parts[3] = dbName
		}
		result := strings.Join(parts, "/")
		if strings.Contains(connString, "?") {
			queryStart := strings.Index(connString, "?")
			result += connString[queryStart:]
		}
		return result
	}

	return &testRealConnectionProvider{connStringFunc: connStringFunc}
}

// testRealConnectionProvider creates actual database connections for testing.
type testRealConnectionProvider struct {
	connStringFunc func(string) string
}

func (r *testRealConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	connString := r.connStringFunc(databaseName)
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return &pgdbtemplate.StandardDatabaseConnection{DB: db}, nil
}

func (r *testRealConnectionProvider) GetConnectionString(databaseName string) string {
	return r.connStringFunc(databaseName)
}

// Mock connection providers for testing failure scenarios

// testDatabaseConnectionFailProvider fails when connecting to databases matching failPattern.
type testDatabaseConnectionFailProvider struct {
	adminDBName  string
	templateName string
	failPattern  string
}

func (p *testDatabaseConnectionFailProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	// Allow admin database and template database to work.
	if databaseName == p.adminDBName || databaseName == p.templateName {
		return createRealConnectionProvider().Connect(ctx, databaseName)
	}

	// Fail for databases matching the fail pattern.
	if len(p.failPattern) > 0 && len(databaseName) >= len(p.failPattern) && databaseName[:len(p.failPattern)] == p.failPattern {
		return nil, fmt.Errorf("intentional connection failure for test database: %s", databaseName)
	}

	// For other databases, use real connection.
	return createRealConnectionProvider().Connect(ctx, databaseName)
}

func (p *testDatabaseConnectionFailProvider) GetConnectionString(databaseName string) string {
	return createRealConnectionProvider().GetConnectionString(databaseName)
}

// templateConnectionFailProvider fails when connecting to a specific template database.
type templateConnectionFailProvider struct {
	adminDBName  string
	templateName string
}

func (p *templateConnectionFailProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	if databaseName == p.templateName {
		return nil, fmt.Errorf("intentional connection failure for template database: %s", databaseName)
	}

	// For admin database, use real connection.
	return createRealConnectionProvider().Connect(ctx, databaseName)
}

func (p *templateConnectionFailProvider) GetConnectionString(databaseName string) string {
	return createRealConnectionProvider().GetConnectionString(databaseName)
}

// markTemplateFailProvider fails when executing ALTER DATABASE ... WITH is_template TRUE.
type markTemplateFailProvider struct {
	adminDBName string
}

func (p *markTemplateFailProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	realConn, err := createRealConnectionProvider().Connect(ctx, databaseName)
	if err != nil {
		return nil, err
	}

	if databaseName == p.adminDBName {
		// Wrap admin connection to intercept ALTER DATABASE commands.
		return &markTemplateFailConnection{DatabaseConnection: realConn, queryCount: 0}, nil
	}

	return realConn, nil
}

func (p *markTemplateFailProvider) GetConnectionString(databaseName string) string {
	return createRealConnectionProvider().GetConnectionString(databaseName)
}

// markTemplateFailConnection wraps a connection and fails on ALTER DATABASE ... WITH is_template TRUE.
type markTemplateFailConnection struct {
	pgdbtemplate.DatabaseConnection
	queryCount int
}

func (c *markTemplateFailConnection) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	// Look for ALTER DATABASE statements with is_template TRUE.
	if strings.Contains(query, "ALTER DATABASE") && strings.Contains(query, "is_template TRUE") {
		return nil, fmt.Errorf("intentional mark template failure")
	}
	return c.DatabaseConnection.ExecContext(ctx, query, args...)
}

// failingMigrationRunner always fails with the specified error.
type failingMigrationRunner struct {
	errorMsg string
}

func (r *failingMigrationRunner) RunMigrations(ctx context.Context, conn pgdbtemplate.DatabaseConnection) error {
	return fmt.Errorf(r.errorMsg)
}
