package pgdbtemplate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

const defaultAdminDBName = "postgres"

// DatabaseConnection represents any PostgreSQL database connection that can
// execute SQL.
type DatabaseConnection interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PingContext(ctx context.Context) error
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
	RunMigrations(ctx context.Context, conn DatabaseConnection) error
}

// TemplateManager manages PostgreSQL template databases for fast test database
// creation.
type TemplateManager struct {
	provider     ConnectionProvider
	migrator     MigrationRunner
	templateName string
	testPrefix   string
	adminDBName  string
	mu           sync.Mutex
	initialized  bool
}

// Config holds configuration for the template manager.
type Config struct {
	ConnectionProvider ConnectionProvider
	MigrationRunner    MigrationRunner
	TemplateName       string
	TestDBPrefix       string
	AdminDBName        string
}

// NewTemplateManager creates a new template manager and checks for PostgreSQL.
func NewTemplateManager(config Config) (*TemplateManager, error) {
	// Check that the connection string is for PostgreSQL.
	connStr := config.ConnectionProvider.GetConnectionString(defaultAdminDBName)
	if !isPostgresConnectionString(connStr) {
		return nil, fmt.Errorf("TemplateManager requires a PostgreSQL connection string, got: %s", connStr)
	}

	templateName := config.TemplateName
	if templateName == "" {
		templateName = fmt.Sprintf("template_db_%d", time.Now().Unix())
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
func isPostgresConnectionString(connStr string) bool {
	return strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://")
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
func (tm *TemplateManager) CreateTestDatabase(ctx context.Context, testDBName ...string) (_ DatabaseConnection, _ string, err error) {
	if err := tm.Initialize(ctx); err != nil {
		return nil, "", err
	}

	dbName := fmt.Sprintf("%s%d", tm.testPrefix, time.Now().UnixNano())
	if len(testDBName) > 0 && testDBName[0] != "" {
		dbName = testDBName[0]
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
		adminConn.ExecContext(ctx, dropQuery) // Ignore errors.
	}()

	// Connect to the new test database.
	testConn, err := tm.provider.Connect(ctx, dbName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to test database: %w", err)
	}

	return testConn, dbName, nil
}

// DropTestDatabase drops a test database.
func (tm *TemplateManager) DropTestDatabase(ctx context.Context, dbName string) error {
	// Connect to template database for DROP operations.
	// This is preferred over admin database as it reduces dependency on admin
	// permissions and the template database connection has the necessary
	// privileges.
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

	return nil
}

// Cleanup removes the template database.
func (tm *TemplateManager) Cleanup(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.initialized {
		return nil
	}

	// Drop template database.
	if err := tm.dropTemplateDatabase(ctx); err != nil {
		return fmt.Errorf("failed to drop template database: %w", err)
	}

	tm.initialized = false
	return nil
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
	if !errors.Is(err, sql.ErrNoRows) {
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
		adminConn.ExecContext(ctx, dropQuery) // Ignore errors.
	}()

	// Connect to template database and run migrations.
	templateConn, err := tm.provider.Connect(ctx, tm.templateName)
	if err != nil {
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer templateConn.Close()

	// Run migrations if migrator is provided.
	if tm.migrator != nil {
		if err := tm.migrator.RunMigrations(ctx, templateConn); err != nil {
			return fmt.Errorf("failed to run migrations on template: %w", err)
		}
	}

	// Mark database as template.
	markTemplateQuery := fmt.Sprintf("ALTER DATABASE %s WITH is_template TRUE", pq.QuoteIdentifier(tm.templateName))
	if _, err := adminConn.ExecContext(ctx, markTemplateQuery); err != nil {
		return fmt.Errorf("failed to mark database as template: %w", err)
	}
	return nil
}

// dropTemplateDatabase removes the template database.
func (tm *TemplateManager) dropTemplateDatabase(ctx context.Context) error {
	adminConn, err := tm.provider.Connect(ctx, tm.adminDBName)
	if err != nil {
		return err
	}
	defer adminConn.Close()

	// Unmark as template first.
	unmarkQuery := fmt.Sprintf("ALTER DATABASE %s WITH is_template FALSE", pq.QuoteIdentifier(tm.templateName))
	_, err = adminConn.ExecContext(ctx, unmarkQuery)
	if err != nil {
		return fmt.Errorf("failed to unmark template database: %w", err)
	}

	// Drop template database.
	dropQuery := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(tm.templateName))
	_, err = adminConn.ExecContext(ctx, dropQuery)
	return err
}
