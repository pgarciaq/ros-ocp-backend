package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaSeekSQL_WithSortValue(t *testing.T) {
	cursor := QuotaCursor{
		ClusterUUID: "c1",
		Namespace:   "ns-a",
		QuotaName:   "quota-1",
		SortValue:   []byte(`"ns-a"`),
	}
	clause, args, nextIdx, err := quotaSeekSQL("namespace", "asc", cursor, true, 3)
	require.NoError(t, err)
	assert.Contains(t, clause, "namespace")
	assert.Contains(t, clause, "cluster_uuid")
	assert.GreaterOrEqual(t, len(args), 3)
	assert.Greater(t, nextIdx, 3)
}

func TestQuotaGroupSeekSQL(t *testing.T) {
	cursor := QuotaCursor{GroupKey: "cluster-2"}
	clause, args, nextIdx := quotaGroupSeekSQL("namespace", cursor, 2)
	assert.Contains(t, clause, "namespace")
	assert.Equal(t, []interface{}{"cluster-2"}, args)
	assert.Equal(t, 3, nextIdx)
}
