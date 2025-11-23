# Advanced Usage Examples

This document covers advanced usage patterns for extending `pgdbtemplate`
with custom implementations.

## Custom Connection Provider

Implement your own connection logic for special authentication
or connection requirements:

```go
import (
	pgdbtemplatepq "github.com/andrei-polukhin/pgdbtemplate-pq"
)

// customConnectionProvider is a custom provider with special authentication logic.
type customConnectionProvider struct {
	baseConnString string
	authToken      string
}

// Connect implements pgdbtemplate.ConnectionProvider.Connect.
func (p *customConnectionProvider) Connect(ctx context.Context, databaseName string) (pgdbtemplate.DatabaseConnection, error) {
	connString := strings.Replace(p.baseConnString, "/postgres?", "/"+databaseName+"?", 1)

	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}

	// Custom authentication logic here.
	if err := p.authenticateWithToken(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return &pgdbtemplatepq.DatabaseConnection{DB: db}, nil
}

// GetNoRowsSentinel implements pgdbtemplate.ConnectionProvider.GetNoRowsSentinel.
func (*customConnectionProvider) GetNoRowsSentinel() error {
	return sql.ErrNoRows
}

// authenticateWithToken performs custom token-based authentication.
func (p *customConnectionProvider) authenticateWithToken(ctx context.Context, db *sql.DB) error {
	// Custom authentication logic...
	return nil
}
```

**Use cases**: OAuth tokens, AWS RDS IAM auth, multi-tenant apps, custom SSL configs.

## Custom Migration Runner

Implement custom migration logic for specialized requirements:

```go
import (
	pgdbtemplatepgx "github.com/andrei-polukhin/pgdbtemplate-pgx"
)

// customMigrationRunner is a custom migration runner that supports rollbacks.
type customMigrationRunner struct {
	upMigrations   []string
	downMigrations []string
}

// NewCustomMigrationRunner creates a new custom migration runner with rollback support.
func NewCustomMigrationRunner(upDir, downDir string) *customMigrationRunner {
	return &customMigrationRunner{
		upMigrations:   loadMigrationsFromDir(upDir),
		downMigrations: loadMigrationsFromDir(downDir),
	}
}

// RunMigrations implements pgdbtemplate.MigrationRunner.RunMigrations.
func (r *customMigrationRunner) RunMigrations(ctx context.Context, conn pgdbtemplate.DatabaseConnection) error {
	// Apply up migrations.
	for _, migration := range r.upMigrations {
		if _, err := conn.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}

// Example helper function to load migrations from directory
func loadMigrationsFromDir(dir string) []string {
	// Implementation would read SQL files from directory
	// This is just a placeholder - implement according to your needs
	return []string{
		"CREATE TABLE example (id SERIAL PRIMARY KEY);",
		// ... more migrations
	}
}
```

**Use cases**: Rollback support, conditional migrations, multi-schema setups,
external migration sources.

## Usage in multiple packages

As described in [the `go test` documentation][go-test-documentation],
running `go test ./...` causes the execution of one test binary per package.
By default, the test binaries are run in parallel, which can lead to issues when
multiple test binaries attempt to create a template database on the same
PostgreSQL server.

When using testcontainers, this is avoided by default, since each test binary
creates its own isolated PostgreSQL container. However, it also means that each
test binary has to create its own template database, which can be
resource-intensive depending on the migration runner, and the number of
migrations.

This can be solved by making sure containers are reused across test binaries,
and that there isn't an attempt to create the template database in each test
binary.

[go-test-documentation]: https://pkg.go.dev/cmd/go#hdr-Test_packages

### Reusing Containers Across Test Binaries

To reuse containers across test binaries, you can set the use an experimental
testcontainer feature called ["reusable container"][reusable-container-docs]:

```go
pg, err := testcontainers.Run(ctx, "postgres:18",
	testcontainers.WithReuseByName("postgres-myproject"),
	// other options...
)
```

[reusable-container-docs]: https://golang.testcontainers.org/features/creating_container/#reusable-container

### Creating the Template Database Only Once

To create the template database only once accross multiple test binaries, you
need to use something outside the memory of the test binaries, such as a file or
an environment variable.

One approach is to `gofrs/flock` to create a file lock around the template database
creation logic. Once a test binary has created the template database, the
library is able to notice that and skip the creation in subsequent test binaries.
All you need to do is prevent a race condition around the initial creation.

```go
fileLock := flock.New("/tmp/myproject_template_db.lock")
err := fileLock.Lock()
require.NoError(t, err)
err := setupTemplateManager(ctx)
require.NoError(t, err)
err = fileLock.Unlock()
require.NoError(t, err)
```

## Environment-Specific Providers

```go
func createEnvironmentSpecificProvider() pgdbtemplate.ConnectionProvider {
	env := os.Getenv("ENVIRONMENT")
	
	switch env {
	case "production":
		return &customConnectionProvider{
			baseConnString: os.Getenv("PROD_DB_URL"),
			authToken:      os.Getenv("PROD_AUTH_TOKEN"),
		}
	case "staging":
		return &customConnectionProvider{
			baseConnString: os.Getenv("STAGING_DB_URL"),
			authToken:      os.Getenv("STAGING_AUTH_TOKEN"),
		}
	default:
		// Development environment uses standard provider.
		return pgdbtemplatepgx.NewConnectionProvider(func(dbName string) string {
			return fmt.Sprintf("postgres://localhost:5432/%s?sslmode=disable", dbName)
		})
	}
}
```

That's it! These patterns cover most advanced use cases
while keeping the implementations flexible.
