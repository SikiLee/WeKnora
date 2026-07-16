//go:build !cgo

package utils

import (
	"errors"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

var errPostgresParserRequiresCGO = errors.New("PostgreSQL SQL parser is unavailable in this no-CGO build")

// Fail closed when libpg_query cannot be compiled. Validation callers receive
// an explicit parse failure; they never execute an unvalidated statement.
func parsePostgresSQL(string) (*pg_query.ParseResult, error) {
	return nil, errPostgresParserRequiresCGO
}

func deparsePostgresSQL(*pg_query.ParseResult) (string, error) {
	return "", errPostgresParserRequiresCGO
}
