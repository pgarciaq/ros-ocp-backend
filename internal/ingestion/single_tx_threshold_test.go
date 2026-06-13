package ingestion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSingleIngestTxEligible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		rowCount              int
		groupCount            int
		digestBatchesFlushed  int
		wantSingleTx          bool
	}{
		{
			name:         "small payload within both thresholds",
			rowCount:     1000,
			groupCount:   200,
			wantSingleTx: true,
		},
		{
			name:         "row count at threshold",
			rowCount:     ingestSingleTxRowThreshold,
			groupCount:   100,
			wantSingleTx: true,
		},
		{
			name:         "row count above threshold",
			rowCount:     ingestSingleTxRowThreshold + 1,
			groupCount:   100,
			wantSingleTx: false,
		},
		{
			name:         "group count at threshold",
			rowCount:     1000,
			groupCount:   ingestSingleTxGroupThreshold,
			wantSingleTx: true,
		},
		{
			name:         "group count above threshold",
			rowCount:     1000,
			groupCount:   ingestSingleTxGroupThreshold + 1,
			wantSingleTx: false,
		},
		{
			name:                 "incremental flush disables single tx",
			rowCount:             1000,
			groupCount:           100,
			digestBatchesFlushed: 1,
			wantSingleTx:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := singleIngestTxEligible(tt.rowCount, tt.groupCount, tt.digestBatchesFlushed)
			assert.Equal(t, tt.wantSingleTx, got)
		})
	}
}
