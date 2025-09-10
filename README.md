# pgdbtemplate

[![Go Reference](https://pkg.go.dev/badge/github.com/andrei-polukhin/pgdbtemplate.svg)](https://pkg.go.dev/github.com/andrei-polukhin/pgdbtemplate)
[![CI](https://github.com/andrei-polukhin/pgdbtemplate/actions/workflows/test.yml/badge.svg)](https://github.com/andrei-polukhin/pgdbtemplate/actions/workflows/test.yml)
[![Coverage](https://codecov.io/gh/andrei-polukhin/pgdbtemplate/branch/main/graph/badge.svg)](https://codecov.io/gh/andrei-polukhin/pgdbtemplate)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/andrei-polukhin/pgdbtemplate/blob/main/LICENSE)

A high-performance Go library for creating PostgreSQL test databases using
template databases for lightning-fast test execution.

## Features

- **🚀 Lightning-fast test databases** - 1.2-1.6x faster than traditional approach
  of running migrations every time, scales to 500 databases, ~17% less memory usage
- **🔒 Thread-safe** - concurrent test database management
- **📊 Scales with complexity** - performance advantage increases with schema complexity
- **🎯 PostgreSQL-specific** with connection string validation
- **⚡ Multiple drivers** - supports both `database/sql` and `pgx` drivers
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
	"fmt"
	"log"

	"github.com/andrei-polukhin/pgdbtemplate"
	_ "github.com/lib/pq"
)

func main() {
	// Create a connection provider with pooling options.
	connStringFunc := func(dbName string) string {
		return fmt.Sprintf("postgres://user:pass@localhost/%s", dbName)
	}
	provider := pgdbtemplate.NewStandardConnectionProvider(connStringFunc)

	// Create migration runner.
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{"./migrations"}, 
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	// Create template manager.
	config := pgdbtemplate.Config{
		ConnectionProvider: provider,
		MigrationRunner:    migrationRunner,
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
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

### 1. Pgx Testing with Existing PostgreSQL

```go
package myapp_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/andrei-polukhin/pgdbtemplate"
)

var templateManager *pgdbtemplate.TemplateManager

func TestMain(m *testing.M) {
	// Setup template manager once.
	if err := setupPgxTemplateManager(); err != nil {
		log.Fatalf("failed to setup template manager: %v", err)
	}

	// Run tests.
	code := m.Run()

	// Cleanup.
	templateManager.Cleanup(context.Background())
	os.Exit(code)
}

func setupPgxTemplateManager() error {
	baseConnString := "postgres://postgres:password@localhost:5432/postgres?sslmode=disable"

	// Create pgx connection provider with connection pooling.
	connStringFunc := func(dbName string) string {
		return pgdbtemplate.ReplaceDatabaseInConnectionString(baseConnString, dbName)
	}
	
	// Configure connection pool settings using options.
	provider := pgdbtemplate.NewPgxConnectionProvider(
		connStringFunc,
		pgdbtemplate.WithPgxMaxConns(10),
		pgdbtemplate.WithPgxMinConns(2),
	)

	// Create migration runner.
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{"./testdata/migrations"},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	// Configure template manager.
	config := pgdbtemplate.Config{
		ConnectionProvider: provider,
		MigrationRunner:    migrationRunner,
	}

	templateManager, err = pgdbtemplate.NewTemplateManager(config)
	if err != nil {
		return fmt.Errorf("failed to create template manager: %w", err)
	}

	// Initialize template database with migrations.
	if err := templateManager.Initialize(context.Background()); err != nil {
		return fmt.Errorf("failed to initialize template: %w", err)
	}
	return nil
}

// Individual test function using pgx.
func TestUserRepositoryPgx(t *testing.T) {
	ctx := context.Background()

	// Create isolated test database with pgx connection.
	testConn, testDBName, err := templateManager.CreateTestDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer testConn.Close()
	defer templateManager.DropTestDatabase(ctx, testDBName)

	// Use pgx-specific features like native PostgreSQL types.
	_, err = testConn.ExecContext(ctx, 
		"INSERT INTO users (name, email, created_at) VALUES ($1, $2, NOW())", 
		"Jane Doe", "jane@example.com")
	if err != nil {
		t.Fatal(err)
	}

	var count int
	err = testConn.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
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

	testConn, testDBName, err := templateManager.CreateTestDatabase(ctx, "custom_test_db")
	if err != nil {
		t.Fatal(err)
	}
	defer testConn.Close()
	defer templateManager.DropTestDatabase(ctx, testDBName)

	// testDBName will be "custom_test_db".
	// Your test code here...
}
```

### 2. Integration with `testcontainers-go`

```go
package myapp_test

import (
	"context"
	"fmt"
	"log"
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
	templateManager *pgdbtemplate.TemplateManager
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start PostgreSQL container.
	if err := setupPostgresContainer(ctx); err != nil {
		log.Fatalf("failed to setup postgres container: %v", err)
	}
	defer pgContainer.Terminate(ctx)

	// Setup template manager.
	if err := setupTemplateManagerWithContainer(ctx); err != nil {
		log.Fatalf("failed to setup template manager: %v", err)
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

	// Create connection provider using the built-in standard provider.
	connStringFunc := func(dbName string) string {
		// Replace the database name in the connection string.
		return pgdbtemplate.ReplaceDatabaseInConnectionString(connStr, dbName)
	}
	provider := pgdbtemplate.NewStandardConnectionProvider(connStringFunc)

	// Create migration runner.
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{"./testdata/migrations"},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	// Configure template manager.
	config := pgdbtemplate.Config{
		ConnectionProvider: provider,
		MigrationRunner:    migrationRunner,
		AdminDBName:        "testdb", // Use the container's default database.
	}

	templateManager, err = pgdbtemplate.NewTemplateManager(config)
	if err != nil {
		return err
	}

	// Initialize template database.
	return templateManager.Initialize(ctx)
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

### 3. Connection Pooling and Options

The `StandardConnectionProvider` supports common database connection pooling
options without requiring custom implementations:

```go
import "time"

provider := pgdbtemplate.NewStandardConnectionProvider(
	func(dbName string) string {
		return "..."
	},
	pgdbtemplate.WithMaxOpenConns(25),
	pgdbtemplate.WithMaxIdleConns(10),
	pgdbtemplate.WithConnMaxLifetime(time.Hour),
	pgdbtemplate.WithConnMaxIdleTime(30*time.Minute),
)
```

The same applies to `PgxConnectionProvider`:

```go
provider := pgdbtemplate.NewPgxConnectionProvider(
	func(dbName string) string {
		return "..."
	},
	pgdbtemplate.WithPgxMaxConns(10),
	pgdbtemplate.WithPgxMinConns(2),
)
```

## Advanced Cases

For advanced usage scenarios including custom connection providers and
custom migration runners, see **[ADVANCED.md](docs/ADVANCED.md)**.

## Performance Benefits

Using template databases provides significant performance improvements over
traditional database creation and migration:

### Real Benchmark Results (Apple M4 Pro)
- **Traditional approach**: ~28.9–43.1ms per database (scales with schema complexity)
- **Template approach**: **~28.2–28.8ms per database** (consistent regardless of complexity)
- **Performance gain increases with schema complexity**: 1.03x → 1.43x → 1.50x faster
- **Superior concurrency**: Thread-safe operations with ~86.5 ops/sec vs ~78.5 ops/sec traditional
- **Memory efficient**: 17% less memory usage per operation

### Schema Complexity Impact
| Schema Size | Traditional | Template | Performance Gain |
|-------------|-------------|----------|------------------|
| 1 Table     | ~28.9ms    | ~28.2ms  | **1.03x faster** |
| 3 Tables    | ~39.5ms    | ~27.6ms  | **1.43x faster** |
| 5 Tables    | ~43.1ms    | ~28.8ms  | **1.50x faster** |

### Scaling Benefits  
| Test Databases | Traditional | Template | Time Saved |
|---|---|---|---|
| 20 DBs | 906.8ms (45.3ms/db) | 613.8ms (29.4ms/db) | **32% faster** |
| 50 DBs | 2.29s (45.8ms/db) | 1.53s (29.8ms/db) | **33% faster** |
| 200 DBs | 9.21s (46.0ms/db) | 5.84s (29.2ms/db) | **37% faster** |
| 500 DBs | 22.31s (44.6ms/db) | 14.82s (29.6ms/db) | **34% faster** |

For comprehensive benchmark analysis, methodology, and detailed results,
see **[BENCHMARKS.md](docs/BENCHMARKS.md)**.

## Migration Files Structure

Organize your migration files for automatic alphabetical ordering:

```
migrations/
├── 001_create_users_table.sql
├── 002_create_posts_table.sql
├── 003_add_user_posts_relation.sql
└── 004_add_indexes.sql
```

## Configuration Options

### Environment Variables

```bash
# Required for running tests - the library will panic if this is not set.
export POSTGRES_CONNECTION_STRING="postgres://user:pass@localhost:5432/postgres?sslmode=disable"
```

**Note**: The `POSTGRES_CONNECTION_STRING` environment variable is **mandatory**
when running tests. The library will panic during test initialization
if this variable is not set.

## Thread Safety

The library is **fully thread-safe** and designed for concurrent use
in production test suites:

### Concurrency Guarantees
- **Template initialization**: Protected by mutex - safe to call from multiple goroutines
- **Database creation**: Each `CreateTestDatabase()` call is fully isolated
- **Unique naming**: Automatic collision-free database naming
  with timestamps and atomic counters
- **Parallel testing**: Safe for `go test -parallel N` with any parallelism level

The template manager internally handles all synchronization,
making it safe to use in any concurrent testing scenario.

## Best Practices

1. **Initialize once**: Set up the template manager in `TestMain()`
2. **Cleanup**: Always call `DropTestDatabase()` and `Cleanup()`
3. **Isolation**: Each test should use its own database
4. **Naming**: Use descriptive test database names for debugging
5. **Migration order**: Use numbered prefixes for deterministic ordering

## Requirements

- PostgreSQL 9.5+ (for template database support)
- Go 1.21+
- PostgreSQL driver (`github.com/lib/pq` or `github.com/jackc/pgx/v5`)

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](docs/CONTRIBUTING.md) for
guidelines.

## License

MIT License - see [LICENSE](LICENSE) file for details.
