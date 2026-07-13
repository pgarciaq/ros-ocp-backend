package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
)

type clusterInstanceTypeItem struct {
	Name      string `json:"name"`
	Series    string `json:"series"`
	VCPU      int32  `json:"vcpu"`
	MemoryGiB int32  `json:"memory_gib"`
	GPUs      int32  `json:"gpus"`
}

type clusterPreferencesSummary struct {
	Configured        bool `json:"configured"`
	PreferenceCount   int  `json:"preference_count"`
	VMPreferenceCount int  `json:"vm_preference_count"`
}

type clusterInstanceTypesResponse struct {
	ClusterUUID   string                    `json:"cluster_uuid"`
	CollectedAt   string                    `json:"collected_at"`
	InstanceTypes []clusterInstanceTypeItem `json:"instance_types"`
	Preferences   clusterPreferencesSummary `json:"preferences"`
}

// GetClusterInstanceTypes handles GET /recommendations/openshift/instance-types/.
func GetClusterInstanceTypes(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	clusterUUIDStr := c.QueryParam("cluster_uuid")
	if clusterUUIDStr == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "bad_request",
			"message": "cluster_uuid query parameter is required",
		})
	}
	clusterUUID, err := uuid.Parse(clusterUUIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "bad_request",
			"message": "invalid cluster_uuid",
		})
	}

	rows, collectedAt, err := vm.ListClusterInstanceTypes(c.Request().Context(), db.GetPool(), orgID, clusterUUID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"error":   "internal_error",
			"message": err.Error(),
		})
	}

	items := make([]clusterInstanceTypeItem, len(rows))
	for i, row := range rows {
		items[i] = clusterInstanceTypeItem{
			Name:      row.Name,
			Series:    row.Series,
			VCPU:      row.VCPU,
			MemoryGiB: row.MemoryGiB,
			GPUs:      row.GPUs,
		}
	}

	prefSummary, err := vm.QueryClusterVMPreferencesSummary(c.Request().Context(), db.GetPool(), orgID, clusterUUID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"error":   "internal_error",
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, clusterInstanceTypesResponse{
		ClusterUUID:   clusterUUID.String(),
		CollectedAt:   collectedAt.UTC().Format(time.RFC3339),
		InstanceTypes: items,
		Preferences: clusterPreferencesSummary{
			Configured:        prefSummary.HasPreferences,
			PreferenceCount:   prefSummary.PreferenceCount,
			VMPreferenceCount: prefSummary.VMPreferenceCount,
		},
	})
}
