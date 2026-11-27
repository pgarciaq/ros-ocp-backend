// Package sqlutil holds the SQL fragment constructors shared by the API and
// engine packages that build raw-SQL readers.
//
// The ILIKE constructors exist so that every contains-pattern in the
// repository is built in exactly one place: user input must never reach an
// ILIKE argument unescaped, or `%` and `_` in a filter value act as wildcards
// (#956, #957).
package sqlutil

import "strings"

// EscapeILIKE escapes ILIKE metacharacters so user input is matched literally.
// Postgres treats backslash as the escape character, so callers that add an
// explicit ESCAPE clause must use '\'.
func EscapeILIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// ContainsPattern is the single construction point for a literal
// case-insensitive contains match: the value is escaped and wrapped in `%`.
// Postgres ILIKE defaults to backslash as the escape character, so the result
// can be bound with or without an explicit ESCAPE '\' clause.
func ContainsPattern(s string) string { return "%" + EscapeILIKE(s) + "%" }

// StarPattern converts Koku-style `*` wildcards into SQL `%` while escaping
// everything the caller meant literally. Only `*` is a wildcard; `%`, `_` and
// `\` in the value match themselves (#957). A value that is only `*` becomes
// `%`, which keeps "all rows" working.
//
// Callers decide whether the result is used with ILIKE (the value contains
// `*`) or with `=` (it does not); StarPattern is only meaningful for the
// wildcard form.
func StarPattern(s string) string {
	segments := strings.Split(s, "*")
	for i, segment := range segments {
		segments[i] = EscapeILIKE(segment)
	}
	return strings.Join(segments, "%")
}
