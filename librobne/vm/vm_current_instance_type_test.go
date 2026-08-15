package vm

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommendVM_PopulatesCurrentInstanceType(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *Digest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.CPURequestMC = 4000
		d.CPULimitMC = 4000
		d.MemRequestKiB = 16 * 1024 * 1024
		d.OrgID = "1234567"
		d.ClusterUUID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.CurrentInstanceType)
	assert.Equal(t, "u1.xlarge", *rec.CurrentInstanceType)
}
