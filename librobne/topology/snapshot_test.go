package topology

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHCPSnapshotEntry_JSONContract pins the operator emission field names
// (#622/#633 handshake): backend parsing must accept exactly what the
// operator sends. A renamed key on either side fails closed silently, so the
// names are tested, not trusted.
func TestHCPSnapshotEntry_JSONContract(t *testing.T) {
	t.Parallel()

	raw := `{"hcpSnapshot":[{` +
		`"hcpNamespace":"hc01-infra-hc01",` +
		`"hostedClusterID":"d5d31999-89ed-4c13-b5e8-c9193f62e630",` +
		`"hcUID":"uid-hc-1",` +
		`"hcpUID":"uid-hcp-1",` +
		`"namespaceUID":"uid-ns-1",` +
		`"namespaceCreatedAt":"2026-09-26T19:10:57Z",` +
		`"observedAt":"2026-09-28T12:00:00Z",` +
		`"complete":true,` +
		`"diagnostics":""}` +
		`],"hostedControlPlaneNamespaces":["hc01-infra-hc01"]}`

	var f TopologyFacts
	require.NoError(t, json.Unmarshal([]byte(raw), &f))
	require.Len(t, f.HCPSnapshot, 1)
	e := f.HCPSnapshot[0]
	assert.Equal(t, "hc01-infra-hc01", e.HCPNamespace)
	assert.Equal(t, "d5d31999-89ed-4c13-b5e8-c9193f62e630", e.HostedClusterID)
	assert.Equal(t, "uid-hc-1", e.HcUID)
	assert.Equal(t, "uid-hcp-1", e.HcpUID)
	assert.Equal(t, "uid-ns-1", e.NamespaceUID)
	assert.Equal(t, "2026-09-26T19:10:57Z", e.NamespaceCreatedAt)
	assert.Equal(t, "2026-09-28T12:00:00Z", e.ObservedAt)
	assert.True(t, e.Complete)
	assert.Empty(t, e.Diagnostics)

	round, err := json.Marshal(f)
	require.NoError(t, err)
	var back TopologyFacts
	require.NoError(t, json.Unmarshal(round, &back))
	assert.Equal(t, f, back, "snapshot entries must survive a marshal round-trip byte-identically in value")
}
