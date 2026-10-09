package costgroups

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestValidateSyncRequest(t *testing.T) {
	valid := SyncRequest{
		OrgID:    "1234567",
		SyncedAt: "2026-10-07T12:00:00Z",
		Entries: []SyncEntry{
			{Namespace: "acme-team", SystemDefault: false},
			{Namespace: "acme-%", IsPrefix: true, SystemDefault: false},
		},
	}
	require.NoError(t, ValidateSyncRequest(valid))

	for name, mutate := range map[string]func(*SyncRequest){
		"empty org":     func(r *SyncRequest) { r.OrgID = "  " },
		"bad timestamp": func(r *SyncRequest) { r.SyncedAt = "yesterday" },
		"empty ns":      func(r *SyncRequest) { r.Entries[0].Namespace = "" },
		"oversize":      func(r *SyncRequest) { r.Entries = make([]SyncEntry, maxEntriesPerSync+1) },
		"whitespace ns": func(r *SyncRequest) { r.Entries[0].Namespace = "has space" },
	} {
		r := valid
		r.Entries = append([]SyncEntry(nil), valid.Entries...)
		mutate(&r)
		require.Error(t, ValidateSyncRequest(r), name)
	}
}

func TestSyncOrgCostGroups_ReplaceSemantics(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-costgroups-" + t.Name()
	svc := NewSyncService(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_platform_namespaces WHERE org_id = $1`, orgID)
	})

	n, err := svc.SyncOrgCostGroups(ctx, SyncRequest{
		OrgID: orgID, SyncedAt: "2026-10-07T12:00:00Z",
		Entries: []SyncEntry{
			{Namespace: "acme-team"},
			{Namespace: "acme-%", IsPrefix: true, SystemDefault: true},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Second sync drops acme-team: removals must stop excluding (the
	// unsafe direction is stale rows, never the replace itself).
	n, err = svc.SyncOrgCostGroups(ctx, SyncRequest{
		OrgID: orgID, SyncedAt: "2026-10-07T13:00:00Z",
		Entries: []SyncEntry{
			{Namespace: "acme-%", IsPrefix: true, SystemDefault: true},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	exact, prefixes, err := LoadOrgPlatformNamespaces(ctx, pool, orgID)
	require.NoError(t, err)
	assert.Empty(t, exact, "removed exact entry must be gone")
	assert.Equal(t, []string{"acme-"}, prefixes, "wildcard suffix stripped at read")

	// Idempotent re-POST converges.
	n, err = svc.SyncOrgCostGroups(ctx, SyncRequest{
		OrgID: orgID, SyncedAt: "2026-10-07T14:00:00Z",
		Entries: []SyncEntry{
			{Namespace: "acme-%", IsPrefix: true, SystemDefault: true},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestLoadOrgPlatformNamespaces_EmptyOrg(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	exact, prefixes, err := LoadOrgPlatformNamespaces(ctx, pool, "org-never-synced-"+t.Name())
	require.NoError(t, err, "never-synced org is fallback-to-compiled, never error")
	assert.Empty(t, exact)
	assert.Empty(t, prefixes)
}
