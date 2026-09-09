package model

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The pgx quality scans read positionally: every selected column needs
// exactly one scan destination and vice versa. Both sides of this assertion
// live in code (SELECT const vs row struct), so drift on either side breaks
// it without a database — unlike a hardcoded count, which only catches
// SQL-side changes (#567). Column *order* still needs the Docker suite.
func TestQualitySelectColumnCounts(t *testing.T) {
	tests := []struct {
		name         string
		selectClause string
		row          interface{}
		scanFn       string
	}{
		{"container", qualityContainerSelect, QualityRow{}, "scanQualityRows"},
		{"pvc", qualityPVCSelect, PVCQualityRow{}, "scanPVCQualityRows"},
		{"vm", qualityVMSelect, VMQualityRow{}, "scanVMQualityRows"},
		{"snapshot", qualitySnapshotSelect, SnapshotQualityRow{}, "scanSnapshotQualityRows"},
		{"gpu-mig", qualityGPUMIGSelect, GPUMIGQualityRow{}, "scanGPUMIGQualityRows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countSQLColumns(tt.selectClause)
			want := reflect.TypeOf(tt.row).NumField()
			assert.Equal(t, want, got,
				"SELECT column count (%d) must equal %s struct fields (%d) scanned by %s; "+
					"update the SELECT const, the scan function, and the struct together — "+
					"a non-column struct field does not belong on a Row type",
				got, tt.scanFn, want, tt.scanFn)
		})
	}
}
