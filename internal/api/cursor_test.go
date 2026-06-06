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
			}),
		},
	}

	for _, tt := range results {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, containerNextCursor(tt.page))
		})
	}
}
