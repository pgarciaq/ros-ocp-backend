package db

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Quoting must be transparent for sane values and atomic for hostile ones:
// every assertion below round-trips through the real pgxpool parser, which
// is also the production consumer (#549).
func TestQuoteKeywordValue_RoundTripsThroughParser(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		dbname   string
		host     string
		port     string
		sslmode  string
		wantHost string
	}{
		// Sane values parse exactly as before (no behavior change).
		{"plain", "ros", "rosdb", "dbhost", "5432", "require", "dbhost"},
		// A space or quote cannot break out into extra keywords: the whole
		// hostile string stays one Host value, and the trailing sslmode
		// still takes effect (no downgrade).
		{"space injection stays atomic", "ros", "rosdb", "dbhost sslmode=disable", "5432", "require", "dbhost sslmode=disable"},
		{"quote injection stays atomic", "ros", "rosdb", "dbhost' sslmode='disable", "5432", "require", "dbhost' sslmode='disable"},
		{"backslash survives", `C:\temp\ca`, "rosdb", `db\host`, "5432", "require", `db\host`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := "user=" + quoteKeywordValue(tt.user) +
				" dbname=" + quoteKeywordValue(tt.dbname) +
				" host=" + quoteKeywordValue(tt.host) +
				" port=" + quoteKeywordValue(tt.port) +
				" sslmode=" + quoteKeywordValue(tt.sslmode)
			cfg, err := pgxpool.ParseConfig(dsn)
			require.NoError(t, err, "quoted DSN must always parse")
			assert.Equal(t, tt.user, cfg.ConnConfig.User)
			assert.Equal(t, tt.dbname, cfg.ConnConfig.Database)
			assert.Equal(t, tt.wantHost, cfg.ConnConfig.Host)
			// The trailing sslmode keyword — not anything smuggled inside a
			// value — governs TLS: require must leave TLS enabled.
			assert.NotNil(t, cfg.ConnConfig.TLSConfig, "sslmode=require must enable TLS")
		})
	}

	// Empty values are syntactically valid when quoted (pgx itself applies
	// libpq defaults such as the OS user afterwards — parser behavior, not
	// quoting behavior — so this asserts parse success only).
	t.Run("empty value parses", func(t *testing.T) {
		_, err := pgxpool.ParseConfig("user=" + quoteKeywordValue("") + " dbname='rosdb'")
		require.NoError(t, err)
	})
}
