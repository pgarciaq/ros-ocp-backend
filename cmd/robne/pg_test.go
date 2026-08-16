package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePostgresDSN_RequiresExplicitScheme(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		output  string
		wantErr string
	}{
		{name: "postgres", output: "postgres://localhost:5432/robne?sslmode=disable"},
		{name: "postgresql", output: "postgresql://localhost:5432/robne?sslmode=disable"},
		{name: "path looks like dsn", output: "localhost:5432/robne", wantErr: "scheme"},
		{name: "sqlite file", output: "./robne.db", wantErr: "scheme"},
		{name: "https", output: "https://localhost/robne", wantErr: "scheme"},
		{name: "empty", output: "", wantErr: "scheme"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsn, err := resolvePostgresDSN(tc.output, "")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.output, dsn)
		})
	}
}

func TestResolvePostgresDSN_URLFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pg.url")
	want := "postgres://robne:secret@127.0.0.1:5432/robne?sslmode=disable"
	require.NoError(t, os.WriteFile(path, []byte(want+"\n"), 0o600))

	dsn, err := resolvePostgresDSN("postgres://", path)
	require.NoError(t, err)
	assert.Equal(t, want, dsn)

	require.NoError(t, os.WriteFile(path, []byte("https://example.invalid/db\n"), 0o600))
	_, err = resolvePostgresDSN("postgres://", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")
}

func TestRequirePostgresIdentity(t *testing.T) {
	t.Parallel()
	err := requirePostgresIdentity("1234567", "local-cluster")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UUID")

	err = requirePostgresIdentity("", "02059694-68ab-4d58-8809-de1e91f1d0e5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "org_id")

	require.NoError(t, requirePostgresIdentity("1234567", "02059694-68ab-4d58-8809-de1e91f1d0e5"))
}

func TestSchemaPlan(t *testing.T) {
	t.Parallel()
	const head uint = 181
	cases := []struct {
		name    string
		in      schemaStatus
		apply   bool
		wantUp  bool
		wantErr string
	}{
		{name: "dirty", in: schemaStatus{Dirty: true, Version: 10}, wantErr: "dirty"},
		{name: "empty without flag", in: schemaStatus{Empty: true}, wantErr: "--apply-schema"},
		{name: "empty with flag", in: schemaStatus{Empty: true}, apply: true, wantUp: true},
		{name: "behind without flag", in: schemaStatus{Version: 10}, wantErr: "--apply-schema"},
		{name: "behind with flag", in: schemaStatus{Version: 10}, apply: true, wantUp: true},
		{name: "at head", in: schemaStatus{Version: head}, wantUp: false},
		{name: "at head extra flag", in: schemaStatus{Version: head}, apply: true, wantUp: false},
		{name: "newer binary", in: schemaStatus{Version: head + 5}, wantErr: "newer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			up, err := schemaPlan(tc.in, head, tc.apply)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.False(t, up)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantUp, up)
		})
	}
}
