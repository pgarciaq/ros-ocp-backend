package ingestion

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// vmSplitDayFile builds one VM usage CSV covering 15-minute slots
// [from, to) of 2026-10-25 for one VM, every row carrying guest-agent
// memory (the #707 evidence shape: a nise row-limit split).
func vmSplitDayFile(from, to int) string {
	var b strings.Builder
	b.WriteString(vmCSVHeader())
	b.WriteByte('\n')
	for i := from; i < to; i++ {
		start := fmt.Sprintf("2026-10-25T%02d:%02d:00Z", i/4, (i%4)*15)
		end := "2026-10-26T00:00:00Z"
		if i+1 < 96 {
			end = fmt.Sprintf("2026-10-25T%02d:%02d:00Z", (i+1)/4, ((i+1)%4)*15)
		}
		fmt.Fprintf(&b, "%s,%s,det-vm,vm-recs,worker-1,linux,1500,2000,4000,1048576,4194304,1572864,107374182400,53687091200,107374182400,120,80,1048576,524288\n",
			start, end)
	}
	return b.String()
}

// TestProcessVMCSV_MultiFileDayMerges is the #707 detector: a payload's VM
// usage split across two files (77 rows 00:00–19:00 + 19 rows 19:15–23:45,
// every row with agent memory) must produce ONE merged digest for the day —
// 96 samples, 96 agent samples, computed mem_available percentiles. Pre-fix,
// each file was digested independently with a full-replacement upsert: the
// last file's 19-sample digest clobbered the first's 77, and 19 agent
// samples fell one short of the 20-sample percentile minimum (NULL
// percentiles, usage-branch recommendation). Stock single-file fixtures
// cannot detect this.
func TestProcessVMCSV_MultiFileDayMerges(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	batch, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)

	n1, err := batch.AddCSV(ctx, strings.NewReader(vmSplitDayFile(0, 77)))
	require.NoError(t, err)
	assert.Equal(t, 77, n1, "file 1 carries the day's first 77 quarter-hour slots")

	n2, err := batch.AddCSV(ctx, strings.NewReader(vmSplitDayFile(77, 96)))
	require.NoError(t, err)
	assert.Equal(t, 19, n2, "file 2 carries the day's remaining 19 slots")

	require.NoError(t, batch.Flush(ctx))

	var sampleCount, agentCount int
	var availP95 *int64
	err = pool.QueryRow(ctx, `
		SELECT sample_count, agent_sample_count, mem_available_p95_kib
		FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = 'det-vm' AND namespace = 'vm-recs'
			AND bucket_date = '2026-10-25'`,
		orgID, cluster).Scan(&sampleCount, &agentCount, &availP95)
	require.NoError(t, err, "the merged digest row must exist")
	assert.Equal(t, 96, sampleCount, "same-day rows across files merge: 77 + 19")
	assert.Equal(t, 96, agentCount, "every row carries agent memory")
	require.NotNil(t, availP95, "96 agent samples clear the 20-sample percentile minimum")
}

// TestProcessVMCSV_SecondPayloadReplaces pins the cross-payload semantics
// (#707's non-goal guard): a second payload re-sending the same (vm, day)
// replaces the first payload's digest — batching must not accumulate
// across payloads (KMO's monthly-accumulator model).
func TestProcessVMCSV_SecondPayloadReplaces(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	b1, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	_, err = b1.AddCSV(ctx, strings.NewReader(vmSplitDayFile(0, 96)))
	require.NoError(t, err)
	require.NoError(t, b1.Flush(ctx))

	// The re-sent payload carries only half the day with different usage.
	b2, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	_, err = b2.AddCSV(ctx, strings.NewReader(vmSplitDayFile(48, 96)))
	require.NoError(t, err)
	require.NoError(t, b2.Flush(ctx))

	var sampleCount int
	err = pool.QueryRow(ctx, `
		SELECT sample_count FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = 'det-vm' AND bucket_date = '2026-10-25'`,
		orgID, cluster).Scan(&sampleCount)
	require.NoError(t, err)
	assert.Equal(t, 48, sampleCount, "a later payload replaces: 48 rows, not 96+48 accumulated")
}

// TestProcessVMCSV_BatchFileFailureIsolation pins Review Focus 5: a file
// that fails to parse takes its own error; batch-mates keep their rows and
// the flush still lands them.
func TestProcessVMCSV_BatchFileFailureIsolation(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	batch, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)

	n1, err := batch.AddCSV(ctx, strings.NewReader(vmSplitDayFile(0, 77)))
	require.NoError(t, err)
	assert.Equal(t, 77, n1)

	// A second file whose stream fails mid-read (row-level malformation is
	// a skip by design; a file-level failure is a transport/reader error).
	bad := &failingReader{head: vmCSVHeader() + "\n"}
	_, err = batch.AddCSV(ctx, bad)
	require.Error(t, err, "the failing file errors")

	// The well-formed file's rows still flush.
	require.NoError(t, batch.Flush(ctx))
	var sampleCount int
	err = pool.QueryRow(ctx, `
		SELECT sample_count FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = 'det-vm' AND bucket_date = '2026-10-25'`,
		orgID, cluster).Scan(&sampleCount)
	require.NoError(t, err)
	assert.Equal(t, 77, sampleCount, "batch-mates keep their rows when one file fails")
}

// failingReader yields a valid header then fails, simulating a download or
// stream error mid-file.
type failingReader struct {
	head string
	done bool
}

func (f *failingReader) Read(p []byte) (int, error) {
	if !f.done {
		f.done = true
		copy(p, f.head)
		return len(f.head), nil
	}
	return 0, fmt.Errorf("simulated mid-file stream failure")
}

// TestVMAgentMinimumSharedBothPaths pins Review Focus 2 (#707): a day with
// fewer than the shared 20 agent samples across ALL files withholds the
// mem_available percentiles on both digest paths — the processor's batch
// and the robne-side in-memory builder must behave identically.
func TestVMAgentMinimumSharedBothPaths(t *testing.T) {
	// 19 rows, every one carrying agent memory: below the shared minimum.
	csv19 := vmSplitDayFile(0, 19)

	// librobne/csv side (robne's path): percentiles withheld.
	rows, err := ParseVMCSVRows(strings.NewReader(csv19))
	require.NoError(t, err)
	require.Len(t, rows, 19)
	digests, _ := libcsv.DailyVMDigests(rows, nil, nil)
	require.Len(t, digests, 1)
	assert.Nil(t, digests[0].MemAvailableP95KiB,
		"librobne/csv withholds avail percentiles below the shared minimum")

	// Processor side (the batch): identical withholding.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	batch, err := NewVMDigestBatch(ctx, pool, "1234567", testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = batch.AddCSV(ctx, strings.NewReader(csv19))
	require.NoError(t, err)
	// A second file adds nothing for the day (empty beyond header).
	_, err = batch.AddCSV(ctx, strings.NewReader(vmCSVHeader()+"\n"))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))
	var availP95 *int64
	var agentCount int
	err = pool.QueryRow(ctx, `
		SELECT mem_available_p95_kib, agent_sample_count FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = 'det-vm' AND bucket_date = '2026-10-25'`,
		"1234567", testutil.TestClusterUUID).Scan(&availP95, &agentCount)
	require.NoError(t, err)
	assert.Equal(t, 19, agentCount)
	assert.Nil(t, availP95, "the processor withholds identically below the shared minimum")
}
