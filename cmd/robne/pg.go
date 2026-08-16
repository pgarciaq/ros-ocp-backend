package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
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

func isPostgresURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql":
		return true
	default:
		return false
	}
}

func samePostgresDB(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return false
	}
	if !isPostgresURL(a) || !isPostgresURL(b) {
		return false
	}
	userA, userB := "", ""
	if ua.User != nil {
		userA = ua.User.Username()
	}
	if ub.User != nil {
		userB = ub.User.Username()
	}
	return ua.Host == ub.Host && ua.Path == ub.Path && userA == userB
}

func digestWindow(terms []types.TermConfig, end time.Time) (time.Time, time.Time) {
	days := types.MaxWindowDays(terms, 0)
	if days < 1 {
		days = 1
	}
	return end.AddDate(0, 0, -days), end
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
