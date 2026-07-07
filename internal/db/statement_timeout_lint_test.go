package db_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBareSETStatementTimeout enforces the architectural invariant that only
// the pool's AfterConnect hook (setStatementTimeout) may issue a session-level
// SET statement_timeout. All other code must use SET LOCAL (transaction-scoped)
// to avoid poisoning pooled connections for subsequent callers.
//
// This test scans all .go files under internal/db/ for the pattern
// "SET statement_timeout" without "LOCAL", flagging violations except in:
//   - setStatementTimeout function definition (db.go — the AfterConnect hook)
//   - this test file itself
func TestNoBareSETStatementTimeout(t *testing.T) {
	dbDir := "."

	entries, err := os.ReadDir(dbDir)
	if err != nil {
		t.Fatalf("reading db dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		// Exempt this lint test itself and the timeout test (which
		// intentionally tests session-level SET for the AfterConnect hook)
		if entry.Name() == "statement_timeout_lint_test.go" ||
			entry.Name() == "statement_timeout_test.go" {
			continue
		}

		path := filepath.Join(dbDir, entry.Name())
		checkFileForBareSetTimeout(t, path)
	}
}

func checkFileForBareSetTimeout(t *testing.T, path string) {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	inAllowedFunc := false

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Track whether we're inside the allowed function
		if strings.Contains(line, "func setStatementTimeout(") {
			inAllowedFunc = true
		}
		if inAllowedFunc && line == "}" {
			inAllowedFunc = false
			continue
		}
		if inAllowedFunc {
			continue
		}

		upper := strings.ToUpper(line)
		if !strings.Contains(upper, "SET STATEMENT_TIMEOUT") {
			continue
		}

		// Allow SET LOCAL (transaction-scoped — correct usage)
		if strings.Contains(upper, "SET LOCAL STATEMENT_TIMEOUT") {
			continue
		}

		// Allow comments documenting the invariant
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		t.Errorf("%s:%d: bare SET statement_timeout without LOCAL — "+
			"use SET LOCAL inside a transaction, or WithHeavyStatementTimeout(). "+
			"Only setStatementTimeout (AfterConnect) may set session-level timeout.\n  line: %s",
			path, lineNum, strings.TrimSpace(line))
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning %s: %v", path, err)
	}
}
