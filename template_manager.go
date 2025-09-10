package pgdbtemplate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
)

// defaultAdminDBName is the default administrative database name
// per PostgreSQL conventions.
const defaultAdminDBName = "postgres"

// Atomic counters for thread-safe unique name generation.
var (
	// globalTemplateCounter is a global atomic counter for unique template names
	// across all template managers.
	globalTemplateCounter int64
	// globalTestDBCounter is a global atomic counter for unique test database names
	// across all template managers to prevent collisions when multiple TemplateManager
	// instances are created concurrently (each would start with counter=0 otherwise).
	globalTestDBCounter int64
)

// Row represents a database row result that can be scanned.
type Row interface {
	// Scan scans the row into the provided destination variables.
	Scan(dest ...any) error
}

// DatabaseConnection represents any PostgreSQL database connection.
type DatabaseConnection interface {
	// ExecContext executes a query with the given context and arguments.
	ExecContext(ctx context.Context, query string, args ...any) (any, error)
	// QueryRowContext executes a query that is expected to return a single row.
	QueryRowContext(ctx context.Context, query string, args ...any) Row
	// Close closes the database connection.
	Close() error
}

// ConnectionProvider creates PostgreSQL database connections.
type ConnectionProvider interface {
	// Connect creates a connection to the specified database.
	Connect(ctx context.Context, databaseName string) (DatabaseConnection, error)
	// GetConnectionString returns connection string for the database.
	GetConnectionString(databaseName string) string
}

// MigrationRunner executes migrations on a PostgreSQL database connection.
type MigrationRunner interface {
	// RunMigrations runs all migrations on the provided connection.
	RunMigrations(ctx context.Context, conn DatabaseConnection) error
}

// TemplateManager manages PostgreSQL template databases for fast test database
// creation.
type TemplateManager struct {
	provider ConnectionProvider
	migrator MigrationRunner

	templateName string
	testPrefix   string
	adminDBName  string

	mu          sync.Mutex
	initialized bool

	createdTestDBs sync.Map // Tracks created test databases for cleanup.
}

// Config holds configuration for the template manager.
type Config struct {
	// ConnectionProvider provides database connections.
	//
	// This field is required.
	ConnectionProvider ConnectionProvider
	// MigrationRunner runs migrations on the template database.
	//
	// This field is required.
	MigrationRunner MigrationRunner
	// TemplateName is the name of the template database.
	//
	// If empty, a unique name will be generated.
	TemplateName string
	// TestDBPrefix is the prefix for test database names.
	//
	// If empty, "test_" will be used.
	TestDBPrefix string
	// AdminDBName is the name of the administrative database to connect to
	// for creating and dropping databases.
	//
	// If empty, "postgres" will be used.
	AdminDBName string
}

// NewTemplateManager creates a new template manager and checks for PostgreSQL.
func NewTemplateManager(config Config) (*TemplateManager, error) {
	// Validate required fields.
	if config.ConnectionProvider == nil {
		return nil, fmt.Errorf("ConnectionProvider is required")
	}
	if config.MigrationRunner == nil {
		return nil, fmt.Errorf("MigrationRunner is required")
	}

	// Check that the connection string is for PostgreSQL.
	connStr := config.ConnectionProvider.GetConnectionString(defaultAdminDBName)
	if !isPostgresConnectionString(connStr) {
		return nil, fmt.Errorf("TemplateManager requires a PostgreSQL connection string, got: %s", connStr)
	}

	templateName := config.TemplateName
	if templateName == "" {
		templateName = fmt.Sprintf("template_db_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&globalTemplateCounter, 1))
	}

	testPrefix := config.TestDBPrefix
	if testPrefix == "" {
		testPrefix = "test_"
	}

	adminDBName := config.AdminDBName
	if adminDBName == "" {
		adminDBName = defaultAdminDBName
	}

	return &TemplateManager{
		provider:     config.ConnectionProvider,
		migrator:     config.MigrationRunner,
		templateName: templateName,
		testPrefix:   testPrefix,
		adminDBName:  adminDBName,
	}, nil
}

// isPostgresConnectionString checks if the connection string is for PostgreSQL.
//
// This function validates both URL and DSN formats, but rejects empty strings.
func isPostgresConnectionString(connStr string) bool {
	if strings.TrimSpace(connStr) == "" {
		return false
	}

	_, err := pgx.ParseConfig(connStr)
	return err == nil
}

// Initialize sets up the template database with all migrations.
func (tm *TemplateManager) Initialize(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.initialized {
		return nil
	}

	if err := tm.createTemplateDatabase(ctx); err != nil {
		return fmt.Errorf("failed to create template database: %w", err)
	}

	tm.initialized = true
	return nil
}

// CreateTestDatabase creates a new test database from the template.
//
// The caller is expected to call Initialize() before using this method.
func (tm *TemplateManager) CreateTestDatabase(ctx context.Context, testDBName ...string) (_ DatabaseConnection, _ string, err error) {
	var dbName string
	if len(testDBName) > 0 && testDBName[0] != "" {
		dbName = testDBName[0]
	} else {
		dbName = fmt.Sprintf("%s%d_%d", tm.testPrefix, time.Now().UnixNano(), atomic.AddInt64(&globalTestDBCounter, 1))
	}

	// Connect to admin database for CREATE DATABASE operations.
	// We cannot use the template database connection because PostgreSQL
	// doesn't allow creating databases from a template that has active connections.
	adminConn, err := tm.provider.Connect(ctx, tm.adminDBName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to admin database: %w", err)
	}
	defer adminConn.Close()

	// Create test database from template.
	query := fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s",
		pq.QuoteIdentifier(dbName), pq.QuoteIdentifier(tm.templateName))
	if _, err := adminConn.ExecContext(ctx, query); err != nil {
		return nil, "", fmt.Errorf("failed to create test database %s: %w", dbName, err)
	}

	// Drop the test database if any further steps fail.
	defer func() {
		if err == nil {
			return
		}
		dropQuery := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(dbName))
		_, dropErr := adminConn.ExecContext(ctx, dropQuery)

		// Also remove from tracking only if cleanup succeeded.
		if dropErr == nil {
			tm.createdTestDBs.Delete(dbName)
			return
		}
		// Append drop error to the original error.
		err = errors.Join(err, fmt.Errorf(
			"failed to drop test database %q: %w",
			dbName,
			dropErr,
		))
	}()

	// Connect to the new test database.
	testConn, err := tm.provider.Connect(ctx, dbName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to test database: %w", err)
	}

	// Track the created test database for cleanup.
	tm.createdTestDBs.Store(dbName, true)

	return testConn, dbName, nil
}

// DropTestDatabase drops a test database.
//
// The caller is expected to call Initialize() before using this method.
func (tm *TemplateManager) DropTestDatabase(ctx context.Context, dbName string) error {
	// Connect to template database for DROP operations.
	// This is preferred to follow the least privilege principle.
	templateConn, err := tm.provider.Connect(ctx, tm.templateName)
	if err != nil {
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer templateConn.Close()

	// Terminate active connections to the database.
	terminateQuery := `
	   SELECT pg_terminate_backend(pid) 
	   FROM pg_stat_activity 
	   WHERE datname = $1 AND pid <> pg_backend_pid()
	`
	_, err = templateConn.ExecContext(ctx, terminateQuery, dbName)
	if err != nil {
		return fmt.Errorf("failed to terminate connections to database %q: %w", dbName, err)
	}

	// Drop the database.
	dropQuery := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(dbName))
	if _, err := templateConn.ExecContext(ctx, dropQuery); err != nil {
		return fmt.Errorf("failed to drop database %s: %w", dbName, err)
	}

	// Remove from tracking map if it was tracked.
	tm.createdTestDBs.Delete(dbName)

	return nil
}

// Cleanup removes all tracked test databases and the template database.
//
// The caller is expected to call Initialize() before using this method.
func (tm *TemplateManager) Cleanup(ctx context.Context) (errs error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.initialized {
		return nil
	}

	// Connect to leader database.
	adminConn, err := tm.provider.Connect(ctx, tm.adminDBName)
	if err != nil {
		return fmt.Errorf("failed to connect to admin database: %w", err)
	}
	defer adminConn.Close()

	// First, clean up all tracked test databases.
	// Any errors are collected and returned after attempting to drop the template.
	if err := tm.cleanupTrackedTestDatabases(ctx, adminConn); err != nil {
		errs = fmt.Errorf("failed to clean up tracked test databases: %w", err)
	}

	// Drop template database.
	// Any errors are appended to errs.
	if err := tm.dropTemplateDatabase(ctx, adminConn); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to drop template database: %w", err))
	}

	tm.initialized = false
	return errs
}

// createTemplateDatabase creates and initializes the template database.
func (tm *TemplateManager) createTemplateDatabase(ctx context.Context) (err error) {
	// Connect to leader database.
	adminConn, err := tm.provider.Connect(ctx, tm.adminDBName)
	if err != nil {
		return fmt.Errorf("failed to connect to admin database: %w", err)
	}
	defer adminConn.Close()

	// Check if template already exists.
	checkQuery := "SELECT TRUE FROM pg_database WHERE datname = $1 LIMIT 1"
	var exists bool
	err = adminConn.QueryRowContext(ctx, checkQuery, tm.templateName).Scan(&exists)
	if err == nil {
		// Template already exists, return early.
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, pgx.ErrNoRows) {
		// Unexpected error.
		return fmt.Errorf("failed to check if template exists: %w", err)
	}

	// Create template database as it does not exist.
	createQuery := fmt.Sprintf("CREATE DATABASE %s", pq.QuoteIdentifier(tm.templateName))
	if _, err := adminConn.ExecContext(ctx, createQuery); err != nil {
		return fmt.Errorf("failed to create template database: %w", err)
	}

	// Should any further steps fail, ensure we drop the created template database.
	defer func() {
		if err == nil {
			return
		}

		dropQuery := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(tm.templateName))
		_, dropErr := adminConn.ExecContext(ctx, dropQuery)
		if dropErr == nil {
			return
		}
		// Append drop error to the original error.
		err = errors.Join(err, fmt.Errorf("failed to drop template database: %w", dropErr))
	}()

	// Connect to template database and run migrations.
	// We do not use admin database to follow the least privilege principle.
	templateConn, err := tm.provider.Connect(ctx, tm.templateName)
	if err != nil {
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer templateConn.Close()

	// Run migrations.
	if err := tm.migrator.RunMigrations(ctx, templateConn); err != nil {
		return fmt.Errorf("failed to run migrations on template: %w", err)
	}

	// Mark database as template.
	markTemplateQuery := fmt.Sprintf("ALTER DATABASE %s WITH is_template TRUE", pq.QuoteIdentifier(tm.templateName))
	if _, err := adminConn.ExecContext(ctx, markTemplateQuery); err != nil {
		return fmt.Errorf("failed to mark database as template: %w", err)
	}
	return nil
}

// dropTemplateDatabase removes the template database.
func (tm *TemplateManager) dropTemplateDatabase(ctx context.Context, adminConn DatabaseConnection) error {
	// Unmark as template first.
	unmarkQuery := fmt.Sprintf("ALTER DATABASE %s WITH is_template FALSE", pq.QuoteIdentifier(tm.templateName))
	_, err := adminConn.ExecContext(ctx, unmarkQuery)
	if err != nil {
		return fmt.Errorf("failed to unmark template database: %w", err)
	}

	// Drop template database.
	dropQuery := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(tm.templateName))
	_, err = adminConn.ExecContext(ctx, dropQuery)
	return err
}

// cleanupTrackedTestDatabases removes all test databases tracked by this manager.
func (tm *TemplateManager) cleanupTrackedTestDatabases(ctx context.Context, adminConn DatabaseConnection) (errs error) {
	// Collect all tracked database names to avoid modifying map
	// during iteration.
	var dbNames []string
	tm.createdTestDBs.Range(func(key, value any) bool {
		if dbName, ok := key.(string); ok {
			dbNames = append(dbNames, dbName)
		}
		return true
	})
	if len(dbNames) == 0 {
		return nil // No databases to clean up.
	}

	// Batch terminate active connections for all databases at once.
	// Connections might already be closed, so we append the error,
	// but continue with cleanup.
	if err := tm.batchTerminateConnections(ctx, adminConn, dbNames); err != nil {
		errs = fmt.Errorf("failed to terminate connections for some databases: %w", err)
	}

	// Drop all databases individually.
	// PostgreSQL doesn't allow DROP DATABASE in transactions/batches.
	for _, dbName := range dbNames {
		dropQuery := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(dbName))
		_, err := adminConn.ExecContext(ctx, dropQuery)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to drop database %q: %w", dbName, err))
			continue // Continue cleaning up other databases.
		}

		// Remove from tracking map only if drop was successful.
		tm.createdTestDBs.Delete(dbName)
	}
	return errs
}

// batchTerminateConnections terminates active connections for multiple databases
// in a single query.
func (tm *TemplateManager) batchTerminateConnections(ctx context.Context, adminConn DatabaseConnection, dbNames []string) error {
	// Build quoted literals for each database name.
	// Using QuoteLiteral is safe here since database names are controlled by this library
	// and provide better performance than parameterized queries (~30% faster).
	quotedNames := make([]string, len(dbNames))
	for i, dbName := range dbNames {
		quotedNames[i] = pq.QuoteLiteral(dbName)
	}

	terminateQuery := fmt.Sprintf(`
		SELECT pg_terminate_backend(pid) 
		FROM pg_stat_activity 
		WHERE datname IN (%s) AND pid <> pg_backend_pid()
	`, strings.Join(quotedNames, ", "))

	_, err := adminConn.ExecContext(ctx, terminateQuery)
	return err
}
