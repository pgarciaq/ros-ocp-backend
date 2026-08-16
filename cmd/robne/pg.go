package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
)

const robneSourceID = "robne"

func resolvePostgresDSN(output, urlFile string) (string, error) {
	raw := strings.TrimSpace(output)
	if urlFile != "" {
		b, err := os.ReadFile(urlFile) //nolint:gosec // G304: explicit --pg-url-file
		if err != nil {
			return "", fmt.Errorf("read --pg-url-file: %w", err)
		}
		raw = strings.TrimSpace(string(b))
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "", fmt.Errorf("--output must use scheme postgres:// or postgresql:// (got %q)", output)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("--output scheme must be postgres or postgresql, not %q", u.Scheme)
	}
	return raw, nil
}

func requirePostgresIdentity(orgID, clusterUUID string) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("org_id is required in YAML when --output is PostgreSQL")
	}
	if _, err := uuid.Parse(strings.TrimSpace(clusterUUID)); err != nil {
		return fmt.Errorf("cluster_uuid %q is not an RFC 4122 UUID (required for PostgreSQL write; do not hash a display name)", clusterUUID)
	}
	return nil
}

type schemaStatus struct {
	Version uint
	Dirty   bool
	Empty   bool
}

func schemaPlan(st schemaStatus, head uint, apply bool) (needUp bool, err error) {
	if st.Dirty {
		return false, fmt.Errorf("schema_migrations is dirty at version %d; repair the database before writing", st.Version)
	}
	if st.Empty {
		if !apply {
			return false, fmt.Errorf("database is empty (no schema_migrations); pass --apply-schema to bootstrap a dedicated robne database")
		}
		return true, nil
	}
	if st.Version > head {
		return false, fmt.Errorf("database schema version %d is newer than this binary (%d); install a matching or newer robne (never migrate Down)", st.Version, head)
	}
	if st.Version < head {
		if !apply {
			return false, fmt.Errorf("database schema version %d is behind this binary (%d); pass --apply-schema to upgrade a dedicated robne database", st.Version, head)
		}
		return true, nil
	}
	return false, nil
}
