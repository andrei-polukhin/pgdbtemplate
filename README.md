# pgdbtemplate

[![Go Reference](https://pkg.go.dev/badge/github.com/andrei-polukhin/pgdbtemplate.svg)](https://pkg.go.dev/github.com/andrei-polukhin/pgdbtemplate)

A high-performance Go library for creating PostgreSQL test databases using
template databases for lightning-fast test execution.

## Features

- **🚀 Lightning-fast test databases** - 1.3-1.5x faster than traditional approach,
  constant ~29ms performance
- **🔒 Thread-safe** concurrent test database management
- **📊 Scales with complexity** - performance advantage increases with schema complexity
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
	"fmt"
	"log"
	"time"

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

### 1. Standard Testing with Existing PostgreSQL

```go
package myapp_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/andrei-polukhin/pgdbtemplate"
	_ "github.com/lib/pq"
)

var templateManager *pgdbtemplate.TemplateManager

func TestMain(m *testing.M) {
	// Setup template manager once.
	if err := setupTemplateManager(); err != nil {
		log.Fatalf("failed to setup template manager: %v", err)
	}

	// Run tests.
	code := m.Run()

	// Cleanup.
	templateManager.Cleanup(context.Background())
	os.Exit(code)
}

func setupTemplateManager() error {
	baseConnString := "postgres://postgres:password@localhost:5432/postgres?sslmode=disable"

	// Create connection provider using the built-in standard provider.
	connStringFunc := func(dbName string) string {
		return strings.Replace(baseConnString, "/postgres?", "/"+dbName+"?", 1)
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
		TemplateName:       "myapp_test_template",
		TestDBPrefix:       "test_myapp_",
	}

	var err error
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

### 2. Integration with `testcontainers-go`

```go
package myapp_test

import (
	"context"
	"database/sql"
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
		parts := strings.Split(connStr, "/")
		if len(parts) > 3 {
			// Replace database part (remove query params first).
			dbPart := strings.Split(parts[3], "?")
			dbPart[0] = dbName
			parts[3] = strings.Join(dbPart, "?")
		}
		return strings.Join(parts, "/")
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
provider := pgdbtemplate.NewStandardConnectionProvider(
	func (dbName string) string {
		return "..."
	},
	pgdbtemplate.WithMaxOpenConns(25),
	pgdbtemplate.WithMaxIdleConns(10),
	pgdbtemplate.WithConnMaxLifetime(time.Hour),
	pgdbtemplate.WithConnMaxIdleTime(30*time.Minute),
)
```

## Advanced Cases

For advanced usage scenarios including custom connection providers and
custom migration runners, see **[ADVANCED.md](docs/ADVANCED.md)**.

## Performance Benefits

Using template databases provides significant performance improvements over
traditional database creation and migration:

### Real Benchmark Results (Apple M4 Pro)
- **Traditional approach**: ~30-44ms per database (scales with schema complexity)
- **Template approach**: **~29ms per database** (consistent regardless of complexity)
- **Performance gain increases with schema complexity**: 1.04x → 1.33x → 1.52x faster
- **Superior concurrency**: Thread-safe operations vs. traditional approach failures
- **Memory efficient**: 18% less memory usage per operation

### Schema Complexity Impact
| Schema Size | Traditional | Template | Performance Gain |
|-------------|-------------|----------|------------------|
| 1 Table     | ~30ms      | ~29ms    | **1.04x faster** |
| 3 Tables    | ~39ms      | ~29ms    | **1.33x faster** |
| 5 Tables    | ~44ms      | ~29ms    | **1.52x faster** |

### Scaling Benefits  
| Test Databases | Traditional | Template | Time Saved |
|---|---|---|---|
| 10 DBs | 450ms | 356ms | **21% faster** |
| 50 DBs | 2.25s | 1.60s | **29% faster** |
| 200 DBs | 9.0s | 6.2s | **31% faster** |

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

### Config Fields

- `ConnectionProvider`: Interface for creating database connections
- `MigrationRunner`: Interface for running database migrations  
- `TemplateName`: Name of the template database (auto-generated if empty)
- `TestDBPrefix`: Prefix for test database names (default: "test_")
- `AdminDBName`: Admin database name for operations (default: "postgres")

### Environment Variables

```bash
# For tests.
export POSTGRES_CONNECTION_STRING="postgres://user:pass@localhost:5432/postgres?sslmode=disable"
```

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
- PostgreSQL driver (`github.com/lib/pq` recommended)

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](docs/CONTRIBUTING.md) for
guidelines.

## License

MIT License - see [LICENSE](LICENSE) file for details.
