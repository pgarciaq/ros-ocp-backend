package api

import (
	"encoding/base64"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeContainerCursor(t *testing.T) {
	original := ContainerCursor{
		Namespace:     "my-ns",
		Workload:      "my-wl",
		ContainerName: "my-cn",
	}
	encoded := EncodeContainerCursor(original)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeContainerCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestDecodeContainerCursor_Invalid(t *testing.T) {
	_, err := DecodeContainerCursor("not-valid-base64!!!")
	require.Error(t, err)

	_, err = DecodeContainerCursor(base64.RawURLEncoding.EncodeToString([]byte("{invalid")))
	require.Error(t, err)
}

func TestEncodeDecodeNamespaceCursor(t *testing.T) {
	original := NamespaceCursor{
		Namespace:   "openshift-config",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		SortValue:   []byte(`"2020-01-01T00:00:00Z"`),
		OrderBy:     "last_reported",
		OrderHow:    "desc",
	}
	encoded := EncodeNamespaceCursor(original)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeNamespaceCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestEncodeDecodeQuotaCursor(t *testing.T) {
	original := QuotaCursor{
		ClusterUUID: "550e8400-e29b-41d4-a716-446655440000",
		Namespace:   "openshift-config",
		QuotaName:   "compute-quota",
		SortValue:   []byte(`"production"`),
	}
	encoded := EncodeQuotaCursor(original)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeQuotaCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestEncodeDecodeVMCursor(t *testing.T) {
	original := VMCursor{
		ClusterUUID: "550e8400-e29b-41d4-a716-446655440000",
		VMName:      "rhel9-vm",
		Namespace:   "vms",
		Term:        "medium_term",
		Engine:      "cost",
		SortValue:   []byte(`100`),
	}
	encoded := EncodeVMCursor(original)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeVMCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestEncodeDecodeSnapshotCursor(t *testing.T) {
	original := SnapshotCursor{
		ClusterUUID:  "550e8400-e29b-41d4-a716-446655440000",
		Namespace:    "openshift-storage",
		SnapshotName: "snap-velero-001",
		SortValue:    []byte(`120`),
	}
	encoded := EncodeSnapshotCursor(original)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeSnapshotCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestCursorSortMismatch(t *testing.T) {
	tests := []struct {
		name           string
		cursorOrderBy  string
		requestOrderBy string
		sortValue      []byte
		wantMismatch   bool
	}{
		{
			name:           "matching sort column",
			cursorOrderBy:  "clusters.last_reported_at",
			requestOrderBy: "clusters.last_reported_at",
			sortValue:      []byte(`"2026-06-16T01:33:52Z"`),
			wantMismatch:   false,
		},
		{
			name:           "different sort column - stale cursor",
			cursorOrderBy:  "clusters.last_reported_at",
			requestOrderBy: "recommendation_sets.estimated_savings_cents",
			sortValue:      []byte(`"2026-06-16T01:33:52Z"`),
			wantMismatch:   true,
		},
		{
			name:           "old cursor without OrderBy but with SortValue",
			cursorOrderBy:  "",
			requestOrderBy: "recommendation_sets.estimated_savings_cents",
			sortValue:      []byte(`"2026-06-16T01:33:52Z"`),
			wantMismatch:   true,
		},
		{
			name:           "no sort value - safe regardless of column",
			cursorOrderBy:  "clusters.last_reported_at",
			requestOrderBy: "recommendation_sets.estimated_savings_cents",
			sortValue:      nil,
			wantMismatch:   false,
		},
		{
			name:           "empty sort value - safe",
			cursorOrderBy:  "",
			requestOrderBy: "clusters.last_reported_at",
			sortValue:      []byte{},
			wantMismatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cursorSortMismatch(tt.cursorOrderBy, tt.requestOrderBy, tt.sortValue)
			assert.Equal(t, tt.wantMismatch, got)
		})
	}
}

func TestStaleCursorDiscardedOnSortChange(t *testing.T) {
	// Simulate the bug scenario: cursor created while sorting by last_reported
	// (timestamp), then user changes sort to estimated_monthly_savings (bigint).
	staleCursor := EncodeContainerCursor(ContainerCursor{
		Namespace:     "my-ns",
		Workload:      "my-wl",
		ContainerName: "my-cn",
		SortValue:     []byte(`"2026-06-16T01:33:52.444023+00:00"`),
		OrderBy:       "clusters.last_reported_at",
	})

	decoded, err := DecodeContainerCursor(staleCursor)
	require.NoError(t, err)

	// The request now sorts by a bigint column — cursor should be discarded.
	newOrderBy := "recommendation_sets.estimated_savings_cents"
	assert.True(t, cursorSortMismatch(decoded.OrderBy, newOrderBy, decoded.SortValue),
		"stale cursor with timestamp SortValue must be discarded when sort changes to bigint column")

	// A cursor with matching OrderBy should NOT be discarded.
	matchingCursor := EncodeContainerCursor(ContainerCursor{
		Namespace:     "my-ns",
		Workload:      "my-wl",
		ContainerName: "my-cn",
		SortValue:     []byte(`42000`),
		OrderBy:       newOrderBy,
	})
	decoded2, err := DecodeContainerCursor(matchingCursor)
	require.NoError(t, err)
	assert.False(t, cursorSortMismatch(decoded2.OrderBy, newOrderBy, decoded2.SortValue),
		"cursor with matching OrderBy should be used normally")
}

func TestContainerNextCursor(t *testing.T) {
	anchor := &model.ContainerPaginationAnchor{
		Namespace: "b", Workload: "w2", ContainerName: "c2",
	}
	results := []struct {
		name string
		page model.NativeListPage
		want string
	}{
		{
			name: "no next page",
			page: model.NativeListPage{HasNext: false},
			want: "",
		},
		{
			name: "uses last anchor",
			page: model.NativeListPage{HasNext: true, LastAnchor: anchor},
			want: EncodeContainerCursor(ContainerCursor{
				Namespace: "b", Workload: "w2", ContainerName: "c2",
				OrderBy: "clusters.last_reported_at",
			}),
		},
	}

	for _, tt := range results {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, containerNextCursor(tt.page, "clusters.last_reported_at"))
		})
	}
}
