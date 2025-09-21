# Security Policy

## 🔒 Security Overview

The `pgdbtemplate` library is designed with security as a first-class concern.
This document outlines our security practices, vulnerability disclosure process,
and security considerations for users of this library.

## 🚨 Reporting Security Vulnerabilities

If you discover a security vulnerability in `pgdbtemplate`,
please help us by reporting it responsibly.

### 📞 Contact Information

**Please DO NOT report security vulnerabilities through public GitHub issues.**

Instead, please report security vulnerabilities using GitHub's
private vulnerability reporting:

- **GitHub Security Advisories**: [Report a vulnerability](https://github.com/andrei-polukhin/pgdbtemplate/security/advisories/new)
- **Benefits**: Private, secure, and tracked through GitHub's security features

### 📋 Disclosure Process

1. **Report**: Submit a vulnerability report via
  [GitHub Security Advisories](https://github.com/andrei-polukhin/pgdbtemplate/security/advisories/new)
2. **Acknowledgment**: You will receive an acknowledgment within 48 hours
3. **Investigation**: We will investigate and provide regular updates (at least weekly)
4. **Fix**: Once confirmed, we will work on a fix and coordinate disclosure
5. **Public Disclosure**: We will publish a security advisory once the fix is available

### 📝 What to Include in Your Report

Please include the following information in the description
of your vulnerability report:

- **Description**: A clear description of the vulnerability
- **Impact**: Potential impact and severity
- **Steps to Reproduce**: Detailed reproduction steps
- **Mitigation**: Any known workarounds or mitigations
- **Contact Information**: How we can reach you for follow-up

### 🏆 Recognition

We appreciate security researchers who help keep our users safe.
With your permission, we will acknowledge your contribution in our
security advisories and CONTRIBUTORS document.

## 🛡️ Security Considerations

### SQL Injection Protection

**✅ Secure by Design**

The library uses parameterized queries and proper SQL escaping:

```go
// ✅ SAFE: Uses QuoteIdentifier for database names.
dropQuery := fmt.Sprintf("DROP DATABASE %s",
    formatters.QuoteIdentifier(dbName))
```

**Database names are properly escaped** using PostgreSQL's
[`QuoteIdentifier`](https://www.postgresql.org/docs/current/functions-string.html)
to prevent SQL injection through database names.

### Connection Security

**🔐 Connection String Handling**

- Connection strings should never be logged or exposed
- Use environment variables or secure credential stores
- Avoid hardcoding credentials in source code

```go
// ✅ RECOMMENDED: Use environment variables.
connString := os.Getenv("DATABASE_URL")

// ❌ AVOID: Hardcoded credentials.
connString := "postgres://user:password@localhost/db"
```

**🔒 TLS Configuration**

Always configure TLS for production databases. Use TLS 1.2 or higher:

```go
// ✅ SECURE: Require TLS.
connString := "postgres://user:pass@host/db?sslmode=require"

// ✅ SECURE: Verify CA certificate.
connString := "postgres://user:pass@host/db?sslmode=verify-ca"

// ✅ SECURE: Full verification with client certs.
connString := "postgres://user:pass@host/db?sslmode=verify-full&sslcert=/path/to/client.crt&sslkey=/path/to/client.key&sslrootcert=/path/to/ca.crt"
```

### Database Permissions

**👤 Principle of Least Privilege**

The library requires specific PostgreSQL permissions:

**Required Permissions for Admin Database User:**
- `CREATE DATABASE` - Create template and test databases
- `CONNECT` - Connect to admin, template and test databases
- `DROP DATABASE` - Clean up template and test databases
- `ALTER DATABASE` - Mark created database as template

### Template Database Security

**🛡️ Template Isolation**

- Template databases are isolated from production data
- Test databases are created from templates (copy-on-write)
- No data leakage between test databases

**⚠️ Template Database Risks**

- **Data Persistence**: Template databases retain data between sessions
- **Permission Inheritance**: Test databases inherit template permissions
- **Cleanup Requirements**: Always clean up test databases
  via `DropTestDatabase` or full `Cleanup`

### Test Database Isolation

**🔐 Data Isolation Guarantees**

- Each test gets a separate database
- Databases are uniquely named with timestamps and counters
- Automatic cleanup prevents data leakage between tests

**Race Condition Prevention:**
- Thread-safe database naming with atomic counters
- Mutex-protected template initialization
- Concurrent test execution support

### Further Security Documentation

Visit [this](https://www.postgresql.org/support/security/) official PostgreSQL link
to read more broad PostgreSQL security information.

## 🔧 Security Best Practices for Users

### 1. Environment Configuration

```bash
# Use environment variables for credentials.
# This is the the simplest secure enough way to provide
# secrets from the machine to the application.
export POSTGRES_USER="test_user"
export POSTGRES_PASSWORD="secure_password"
export POSTGRES_HOST="localhost"
export POSTGRES_SSLMODE="require"  # Use TLS 1.2+
```

### 2. Connection Provider Setup

```go
// ✅ SECURE: Use connection pooling with limits.
provider := pgdbtemplatepgx.NewConnectionProvider(
    func(dbName string) string {
        return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=require",
            os.Getenv("POSTGRES_USER"),
            os.Getenv("POSTGRES_PASSWORD"),
            os.Getenv("POSTGRES_HOST"),
            dbName)
    },
    pgdbtemplatepgx.WithMaxConns(20),
    pgdbtemplatepgx.WithMaxConnLifetime(30*time.Minute),
)
```

### 3. Test Cleanup

```go
func TestMain(m *testing.M) {
    // Setup.
    setupTemplateManager()

    // ✅ CRITICAL: Always cleanup.
    defer func() {
        err := templateManager.Cleanup(context.Background())
        if err == nil {
            return
        }

        log.Printf("Cleanup failed: %v", err)
        // Handle cleanup errors appropriately.
    }()

    // Run tests.
    code := m.Run()
    os.Exit(code)
}
```

### 4. Logging Security

```go
// ❌ AVOID: Logging connection strings.
log.Printf("Connecting to: %s", connectionString)

// ✅ SAFE: Log without sensitive data.
log.Printf("Connecting to database: %s", dbName)
```

### 5. Connection Pooling Settings

**🔄 Pool Configuration Best Practices**

```go
// ✅ SECURE: Configure connection pool limits.
provider := pgdbtemplatepgx.NewConnectionProvider(
    connStringFunc,
    pgdbtemplatepgx.WithMaxConns(10),      // Limit max connections.
    pgdbtemplatepgx.WithMinConns(1),       // Maintain minimum connections.
    pgdbtemplatepgx.WithMaxConnLifetime(1*time.Hour), // Rotate connections.
    pgdbtemplatepgx.WithMaxConnIdleTime(10*time.Minute), // Ensure no zombie connections.
)
```

**Connection Lifetime Management:**
- Set `MaxConnLifetime` to prevent connection reuse attacks
- Configure `MaxConnIdleTime` for idle connection cleanup
- Monitor connection pool metrics

## 🔄 Security Updates

### Versioning and Updates

We follow semantic versioning for security updates:

- **PATCH versions** (1.2.3 → 1.2.4): Security fixes
- **MINOR versions** (1.2.3 → 1.3.0): New features, backward compatible
- **MAJOR versions** (1.2.3 → 2.0.0): Breaking changes

### Security Advisory Process

1. **Vulnerability Confirmed**: Assign CVE if applicable
2. **Fix Developed**: Create patch release
3. **Advisory Published**: GitHub Security Advisory + Release notes
4. **User Notification**: Dependabot alerts, release announcements

### Supported Versions

We provide security updates for:
- **Latest major version**: Full support
- **Previous major version**: Critical security fixes only
- **Older versions**: No security updates

## 📊 Security Metrics

### Automated Security Scanning

This repository uses automated security scanning through GitHub Actions:

- **Weekly Security Scans**: Comprehensive Go and GitHub Actions security analysis
  runs every Monday morning
- **Gosec**: Go security linter for detecting security issues in Go code
- **Staticcheck**: Advanced static analysis for Go code quality and security
- **Govulncheck**: Official Go vulnerability scanner for known CVEs in dependencies
- **Dependency Auditing**: Automated checks for dependency integrity and vulnerabilities

### CodeQL Security Scanning

This repository uses GitHub CodeQL for automated security analysis:

- **SAST (Static Application Security Testing)**: Automated code analysis on pushes
  and pull requests
- **Dependency Scanning**: Vulnerable dependency detection
- **Secret Detection**: Prevent credential leaks
- **Security Alerts**: Integrated with GitHub Security tab

### Dependency Security

- **Go Modules**: Regular dependency updates via Dependabot
- **Vulnerability Scanning**: Automated weekly checks for known CVEs
- **Minimal Dependencies**: Reduced attack surface through careful dependency selection

## 📞 Contact

For security-related questions or concerns:
- **Security Issues**: [GitHub Security Advisories](https://github.com/andrei-polukhin/pgdbtemplate/security/advisories/new)
- **General Support**: [GitHub Issues](https://github.com/andrei-polukhin/pgdbtemplate/issues)

## 📜 License

This security policy is part of the MIT-licensed `pgdbtemplate` project.
