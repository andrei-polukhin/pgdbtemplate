# pgdbtemplate

A high-performance Go library for creating PostgreSQL test databases using template databases for lightning-fast test execution.

## Features

- **🚀 Fast test database creation** using PostgreSQL template databases
- **🔒 Thread-safe** concurrent test database management  
- **🎯 PostgreSQL-specific** with connection string validation
- **🧪 Flexible testing** support for various test scenarios
- **📦 Testcontainers integration** for containerized testing
- **🔧 Configurable** migration runners and connection providers

## Installation

```bash
go get github.com/andrei-polukhin/pgdbtemplate
```

## Quick Start

```go
package main

import (
    "context"
    "database/sql"
    "log"
    
    "github.com/andrei-polukhin/pgdbtemplate"
    _ "github.com/lib/pq"
)

func main() {
    // Create a connection provider.
    connStringFunc := func(dbName string) string {
        return fmt.Sprintf("postgres://user:pass@localhost/%s", dbName)
    }
    provider := pgdbtemplate.NewStandardPgConnectionProvider(connStringFunc)
    
    // Create migration runner.
    migrationRunner := pgdbtemplate.NewPgFileMigrationRunner(
        []string{"./migrations"}, 
        pgdbtemplate.AlphabeticalMigrationFilesSorting,
    )
    
    // Create template manager.
    config := pgdbtemplate.PgConfig{
        ConnectionProvider: provider,
        MigrationRunner:    migrationRunner,
        TemplateName:       "my_app_template",
        TestDBPrefix:       "test_",
    }
    
    tm, err := pgdbtemplate.NewPgTemplateManager(config)
    if err != nil {
        log.Fatal(err)
    }
    
    // Initialize template with migrations.
    ctx := context.Background()
    if err := tm.Initialize(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Create test database (fast!).
    testDB, testDBName, err := tm.CreateTestDatabase(ctx)
    if err != nil {
        log.Fatal(err)
    }
    defer testDB.Close()
    defer tm.DropTestDatabase(ctx, testDBName)
    
    // Use testDB for testing...
    log.Printf("Test database %s ready!", testDBName)
}
```

## Usage Examples

### 1. Standard Testing with Existing PostgreSQL

```go
package myapp_test

import (
    "context"
    "database/sql"
    "os"
    "testing"
    
    "github.com/andrei-polukhin/pgdbtemplate"
    _ "github.com/lib/pq"
)

var templateManager *pgdbtemplate.PgTemplateManager

func TestMain(m *testing.M) {
    // Setup template manager once.
    setupTemplateManager()
    
    // Run tests.
    code := m.Run()
    
    // Cleanup.
    templateManager.Cleanup(context.Background())
    os.Exit(code)
}

func setupTemplateManager() {
    connString := "postgres://postgres:password@localhost:5432/postgres?sslmode=disable"
    
    // Create connection provider.
    provider := createConnectionProvider(connString)
    
    // Create migration runner.
    migrationRunner := pgdbtemplate.NewPgFileMigrationRunner(
        []string{"./testdata/migrations"},
        pgdbtemplate.AlphabeticalMigrationFilesSorting,
    )
    
    // Configure template manager.
    config := pgdbtemplate.PgConfig{
        ConnectionProvider: provider,
        MigrationRunner:    migrationRunner,
        TemplateName:       "myapp_test_template",
        TestDBPrefix:       "test_myapp_",
    }
    
    var err error
    templateManager, err = pgdbtemplate.NewPgTemplateManager(config)
    if err != nil {
        panic(err)
    }
    
    // Initialize template database with migrations.
    if err := templateManager.Initialize(context.Background()); err != nil {
        panic(err)
    }
}

func createConnectionProvider(baseConnString string) pgdbtemplate.PgConnectionProvider {
    return &realConnectionProvider{
        connStringFunc: func(dbName string) string {
            return strings.Replace(baseConnString, "/postgres?", "/"+dbName+"?", 1)
        },
    }
}

// Custom connection provider that creates real connections.
type realConnectionProvider struct {
    connStringFunc func(string) string
}

func (r *realConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.PgDatabaseConnection, error) {
    connString := r.connStringFunc(databaseName)
    db, err := sql.Open("postgres", connString)
    if err != nil {
        return nil, err
    }
    
    if err := db.PingContext(ctx); err != nil {
        db.Close()
        return nil, err
    }
    
    return &pgdbtemplate.StandardPgDatabaseConnection{DB: db}, nil
}

func (r *realConnectionProvider) GetConnectionString(databaseName string) string {
    return r.connStringFunc(databaseName)
}

// Individual test function.
func TestUserRepository(t *testing.T) {
    ctx := context.Background()
    
    // Create isolated test database.
    testDB, testDBName, err := templateManager.CreateTestDatabase(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer testDB.Close()
    defer templateManager.DropTestDatabase(ctx, testDBName)
    
    // Your test code here using testDB.
    _, err = testDB.ExecContext(ctx, "INSERT INTO users (name, email) VALUES ($1, $2)", 
        "John Doe", "john@example.com")
    if err != nil {
        t.Fatal(err)
    }
    
    var count int
    err = testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
    if err != nil {
        t.Fatal(err)
    }
    
    if count != 1 {
        t.Errorf("Expected 1 user, got %d", count)
    }
}

// Test with custom database name.
func TestWithCustomDBName(t *testing.T) {
    ctx := context.Background()
    
    testDB, testDBName, err := templateManager.CreateTestDatabase(ctx, "custom_test_db")
    if err != nil {
        t.Fatal(err)
    }
    defer testDB.Close()
    defer templateManager.DropTestDatabase(ctx, testDBName)
    
    // testDBName will be "custom_test_db".
    // Your test code here...
}
```

### 2. Integration with Testcontainers-Go

```go
package myapp_test

import (
    "context"
    "database/sql"
    "fmt"
    "strings"
    "testing"
    "time"
    
    "github.com/andrei-polukhin/pgdbtemplate"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"
    _ "github.com/lib/pq"
)

var (
    pgContainer     *postgres.PostgresContainer
    templateManager *pgdbtemplate.PgTemplateManager
)

func TestMain(m *testing.M) {
    ctx := context.Background()
    
    // Start PostgreSQL container.
    if err := setupPostgresContainer(ctx); err != nil {
        panic(fmt.Sprintf("failed to setup postgres container: %w", err))
    }
    defer pgContainer.Terminate(ctx)
    
    // Setup template manager.
    if err := setupTemplateManagerWithContainer(ctx); err != nil {
        panic(fmt.Sprintf("failed to setup template manager: %w", err))
    }
    defer templateManager.Cleanup(ctx)
    
    // Run tests.
    m.Run()
}

func setupPostgresContainer(ctx context.Context) error {
    var err error
    pgContainer, err = postgres.RunContainer(ctx,
        testcontainers.WithImage("postgres:15"),
        postgres.WithDatabase("testdb"),
        postgres.WithUsername("testuser"),
        postgres.WithPassword("testpass"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2).
                WithStartupTimeout(5*time.Second),
        ),
    )
    return err
}

func setupTemplateManagerWithContainer(ctx context.Context) error {
    // Get connection details from container.
    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    if err != nil {
        return err
    }
    
    // Create connection provider.
    provider := &containerConnectionProvider{
        baseConnString: connStr,
    }
    
    // Create migration runner.
    migrationRunner := pgdbtemplate.NewPgFileMigrationRunner(
        []string{"./testdata/migrations"},
        pgdbtemplate.AlphabeticalMigrationFilesSorting,
    )
    
    // Configure template manager.
    config := pgdbtemplate.PgConfig{
        ConnectionProvider: provider,
        MigrationRunner:    migrationRunner,
        TemplateName:       "testcontainer_template",
        TestDBPrefix:       "tc_test_",
        AdminDBName:        "testdb", // Use the container's default database.
    }
    
    templateManager, err = pgdbtemplate.NewPgTemplateManager(config)
    if err != nil {
        return err
    }
    
    // Initialize template database.
    return templateManager.Initialize(ctx)
}

// Connection provider for testcontainers.
type containerConnectionProvider struct {
    baseConnString string
}

func (c *containerConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.PgDatabaseConnection, error) {
    connString := c.GetConnectionString(databaseName)
    
    db, err := sql.Open("postgres", connString)
    if err != nil {
        return nil, err
    }
    
    if err := db.PingContext(ctx); err != nil {
        db.Close()
        return nil, err
    }
    
    return &pgdbtemplate.StandardPgDatabaseConnection{DB: db}, nil
}

func (c *containerConnectionProvider) GetConnectionString(databaseName string) string {
    // Replace the database name in the connection string.
    parts := strings.Split(c.baseConnString, "/")
    if len(parts) > 3 {
        // Replace database part (remove query params first).
        dbPart := strings.Split(parts[3], "?")
        dbPart[0] = databaseName
        parts[3] = strings.Join(dbPart, "?")
    }
    return strings.Join(parts, "/")
}

// Example test using testcontainers.
func TestUserServiceWithContainer(t *testing.T) {
    ctx := context.Background()
    
    // Create test database from template.
    testDB, testDBName, err := templateManager.CreateTestDatabase(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer testDB.Close()
    defer templateManager.DropTestDatabase(ctx, testDBName)
    
    // Test your service with the isolated database.
    userService := NewUserService(testDB)
    
    user := &User{Name: "Alice", Email: "alice@example.com"}
    if err := userService.Create(ctx, user); err != nil {
        t.Fatal(err)
    }
    
    users, err := userService.List(ctx)
    if err != nil {
        t.Fatal(err)
    }
    
    if len(users) != 1 {
        t.Errorf("Expected 1 user, got %d", len(users))
    }
}

// Concurrent test example.
func TestConcurrentOperations(t *testing.T) {
    const numGoroutines = 10
    ctx := context.Background()
    
    // Create multiple test databases concurrently.
    results := make(chan error, numGoroutines)
    
    for i := 0; i < numGoroutines; i++ {
        go func(id int) {
            testDB, testDBName, err := templateManager.CreateTestDatabase(ctx)
            if err != nil {
                results <- err
                return
            }
            defer testDB.Close()
            defer templateManager.DropTestDatabase(ctx, testDBName)
            
            // Simulate some database operations.
            _, err = testDB.ExecContext(ctx, "INSERT INTO users (name) VALUES ($1)", 
                fmt.Sprintf("User_%d", id))
            results <- err
        }(i)
    }
    
    // Wait for all goroutines.
    for i := 0; i < numGoroutines; i++ {
        if err := <-results; err != nil {
            t.Fatal(err)
        }
    }
}
```

### 3. Custom Migration Runner

```go
// Custom migration runner that supports rollbacks.
type customMigrationRunner struct {
    upMigrations   []string
    downMigrations []string
}

func NewCustomMigrationRunner(upDir, downDir string) *customMigrationRunner {
    return &customMigrationRunner{
        upMigrations:   loadMigrationsFromDir(upDir),
        downMigrations: loadMigrationsFromDir(downDir),
    }
}

func (r *customMigrationRunner) RunMigrations(ctx context.Context, conn pgdbtemplate.PgDatabaseConnection) error {
    // Apply up migrations.
    for _, migration := range r.upMigrations {
        if _, err := conn.ExecContext(ctx, migration); err != nil {
            return fmt.Errorf("migration failed: %w", err)
        }
    }
    return nil
}

// Use custom migration runner.
func setupWithCustomMigrations() {
    customRunner := NewCustomMigrationRunner("./migrations/up", "./migrations/down")
    
    config := pgdbtemplate.PgConfig{
        ConnectionProvider: provider,
        MigrationRunner:    customRunner,
        TemplateName:       "custom_template",
    }
    
    tm, _ := pgdbtemplate.NewPgTemplateManager(config)
    // ...
}
```

## Migration Files Structure

Organize your migration files for automatic alphabetical ordering:

```
migrations/
├── 001_create_users_table.sql
├── 002_create_posts_table.sql
├── 003_add_user_posts_relation.sql
└── 004_add_indexes.sql
```

Example migration file (`001_create_users_table.sql`):
```sql
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
```

## Performance Benefits

Using template databases provides significant performance improvements:

- **Traditional approach**: ~500ms per test database (create + migrate)
- **Template approach**: ~50ms per test database (copy from template)
- **10x faster** test execution for database-heavy test suites

## Configuration Options

### PgConfig Fields

- `ConnectionProvider`: Interface for creating database connections
- `MigrationRunner`: Interface for running database migrations  
- `TemplateName`: Name of the template database (auto-generated if empty)
- `TestDBPrefix`: Prefix for test database names (default: "test_")
- `AdminDBName`: Admin database name for operations (default: "postgres")

### Environment Variables

```bash
# For tests
export POSTGRES_CONNECTION_STRING="postgres://user:pass@localhost:5432/postgres?sslmode=disable"
```

## Thread Safety

The library is designed for concurrent use:
- Template initialization is protected by mutex
- Each test database creation is isolated
- Safe for parallel test execution with `go test -parallel N`

## Best Practices

1. **Initialize once**: Set up the template manager in `TestMain()`
2. **Cleanup**: Always call `DropTestDatabase()` and `Cleanup()`
3. **Isolation**: Each test should use its own database
4. **Naming**: Use descriptive test database names for debugging
5. **Migration order**: Use numbered prefixes for deterministic ordering

## Requirements

- PostgreSQL 9.5+ (for template database support)
- Go 1.21+
- PostgreSQL driver (`github.com/lib/pq` recommended)

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) file for details.
