package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProductionFilesDoNotImportEngineGodPackage(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(name)) //nolint:gosec // G304: local cmd/robne sources
		require.NoError(t, err)
		if strings.Contains(string(b), "ros-ocp-backend/internal/engine") {
			t.Errorf("%s must not import internal/engine", name)
		}
	}
}
