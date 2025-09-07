package pgdbtemplate

import (
	"net/url"
	"strings"
)

// ReplaceDatabaseInConnectionString replaces the database name
// in a PostgreSQL connection string.
//
// It supports both URL format (postgres://...) and
// DSN format (host=... dbname=...).
func ReplaceDatabaseInConnectionString(connStr, dbName string) string {
	// Handle postgres:// and postgresql:// URLs.
	if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
		u, err := url.Parse(connStr)
		if err != nil {
			// Fallback to simple replacement if URL parsing fails.
			return strings.Replace(connStr, "/postgres", "/"+dbName, 1)
		}
		u.Path = "/" + dbName
		return u.String()
	}

	// Handle DSN format (host=localhost user=postgres dbname=postgres ...).
	if strings.Contains(connStr, "dbname=") {
		// Simple replacement for DSN format.
		parts := strings.Fields(connStr)
		for i, part := range parts {
			if strings.HasPrefix(part, "dbname=") {
				parts[i] = "dbname=" + dbName
				break
			}
		}
		return strings.Join(parts, " ")
	}

	// Fallback: assume it ends with a database name.
	lastSlash := strings.LastIndex(connStr, "/")
	if lastSlash >= 0 {
		return connStr[:lastSlash+1] + dbName
	}
	return connStr + "/" + dbName
}
