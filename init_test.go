package pgdbtemplate_test

import "os"

var testConnectionString string

func init() {
	testConnectionString = os.Getenv("POSTGRES_CONNECTION_STRING")
	if testConnectionString == "" {
		panic("POSTGRES_CONNECTION_STRING environment variable is required for tests")
	}
}
