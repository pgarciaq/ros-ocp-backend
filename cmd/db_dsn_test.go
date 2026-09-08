package cmd

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sane values must render exactly (query escaping aside, the layout matches
// the old Sprintf for every existing deployment), while hostile values
// round-trip through net/url with no token split (#549).
func TestBuildMigrateDSN(t *testing.T) {
	got, err := buildMigrateDSN("ros", "pw", "dbhost", "5432", "rosdb", "require", "/tmp/rosocp-rds-ca-abc.pem")
	require.NoError(t, err)
	assert.Equal(t, "postgres://ros:pw@dbhost:5432/rosdb?sslmode=require&sslrootcert=%2Ftmp%2Frosocp-rds-ca-abc.pem", got)
}

func TestBuildMigrateDSN_HostileValuesRoundTrip(t *testing.T) {
	// Generated passwords and copy-pasted values routinely carry
	// URL-significant characters that raw Sprintf would splinter across
	// user/host/path/query. Host itself stays plain: Go's url.Parse rejects
	// escapes in hosts, so a spaced host is validated, not encoded.
	got, err := buildMigrateDSN("r os", "p@w:o/r?d", "dbhost", "5432", "ros db", "require", "/tmp/ca file.pem")
	require.NoError(t, err)

	parsed, err := url.Parse(got)
	require.NoError(t, err, "built DSN must always parse")
	assert.Equal(t, "r os", parsed.User.Username())
	pw, _ := parsed.User.Password()
	assert.Equal(t, "p@w:o/r?d", pw)
	assert.Equal(t, "dbhost", parsed.Hostname())
	assert.Equal(t, "5432", parsed.Port())
	assert.Equal(t, "/ros db", parsed.Path)
	assert.Equal(t, "require", parsed.Query().Get("sslmode"))
	assert.Equal(t, "/tmp/ca file.pem", parsed.Query().Get("sslrootcert"))
}

func TestBuildMigrateDSN_MalformedHostAndPortFailClosed(t *testing.T) {
	// Hosts cannot be percent-encoded (url.Parse rejects escapes there), so
	// malformed values must error here — not as an obscure driver failure.
	_, err := buildMigrateDSN("ros", "pw", "db host", "5432", "rosdb", "require", "/tmp/ca.pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DB host")

	_, err = buildMigrateDSN("ros", "pw", "dbhost", "54x32", "rosdb", "require", "/tmp/ca.pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DB port")
}
