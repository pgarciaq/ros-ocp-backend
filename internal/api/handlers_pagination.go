package api

import (
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func applyContainerListCursor(c echo.Context, opts *listoptions.ListOptions) error {
	after := c.QueryParam("after")
	if after == "" {
		return nil
	}
	cursor, err := DecodeContainerCursor(after)
	if err != nil {
		return fmt.Errorf("invalid after parameter: %w", err)
	}
	opts.HasCursor = true
	opts.AfterNamespace = cursor.Namespace
	opts.AfterWorkload = cursor.Workload
	opts.AfterContainer = cursor.ContainerName
	opts.AfterContainerClusterUUID = cursor.ClusterUUID
	if len(cursor.SortValue) > 0 {
		sortVal, decodeErr := model.DecodePaginationSortValue(cursor.SortValue)
		if decodeErr != nil {
			return fmt.Errorf("invalid after parameter: %w", decodeErr)
		}
		opts.AfterContainerSortPresent = true
		opts.AfterContainerSortValue = sortVal
	}
	return nil
}

func applyNamespaceListCursor(c echo.Context, opts *listoptions.ListOptions) error {
	after := c.QueryParam("after")
	if after == "" {
		return nil
	}
	cursor, err := DecodeNamespaceCursor(after)
	if err != nil {
		return fmt.Errorf("invalid after parameter: %w", err)
	}
	opts.HasCursor = true
	opts.AfterNamespaceName = cursor.Namespace
	opts.AfterNSClusterUUID = cursor.ClusterUUID
	if len(cursor.SortValue) > 0 {
		sortVal, decodeErr := model.DecodePaginationSortValue(cursor.SortValue)
		if decodeErr != nil {
			return fmt.Errorf("invalid after parameter: %w", decodeErr)
		}
		opts.AfterNSSortPresent = true
		opts.AfterNSSortValue = sortVal
	}
	return nil
}

func containerNextCursor(page model.NativeListPage) string {
	if !page.HasNext || page.LastAnchor == nil {
		return ""
	}
	anchor := page.LastAnchor
	return EncodeContainerCursor(ContainerCursor{
		Namespace:     anchor.Namespace,
		Workload:      anchor.Workload,
		ContainerName: anchor.ContainerName,
		ClusterUUID:   anchor.ClusterUUID,
		SortValue:     model.PaginationSortValueJSON(anchor.SortValue),
	})
}

func namespaceNextCursor(page model.NativeNamespaceListPage) string {
	if !page.HasNext || page.LastAnchor == nil {
		return ""
	}
	anchor := page.LastAnchor
	return EncodeNamespaceCursor(NamespaceCursor{
		Namespace:   anchor.Namespace,
		ClusterUUID: anchor.ClusterUUID,
		SortValue:   model.PaginationSortValueJSON(anchor.SortValue),
	})
}

func buildContainerListMeta(c echo.Context, orgID string, page model.NativeListPage, opts listoptions.ListOptions) *Collection {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = containerNextCursor(page)
	}

	resp := PaginatedCollectionResponse(nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
	resp.Meta.Currency = resolveListCurrencyFromRequest(c, orgID)
	return resp
}

func buildNamespaceListMeta(c echo.Context, orgID string, page model.NativeNamespaceListPage, opts listoptions.ListOptions) *Collection {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = namespaceNextCursor(page)
	}

	resp := PaginatedCollectionResponse(nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
	resp.Meta.Currency = resolveListCurrencyFromRequest(c, orgID)
	return resp
}

func applyPVCCursor(c echo.Context) (PVCCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return PVCCursor{}, false, nil
	}
	cursor, err := DecodePVCCursor(after)
	if err != nil {
		return PVCCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func applyNodeUtilCursor(c echo.Context) (NodeUtilCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return NodeUtilCursor{}, false, nil
	}
	cursor, err := DecodeNodeUtilCursor(after)
	if err != nil {
		return NodeUtilCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func applyNodeGPUCursor(c echo.Context) (NodeGPUCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return NodeGPUCursor{}, false, nil
	}
	cursor, err := DecodeNodeGPUCursor(after)
	if err != nil {
		return NodeGPUCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func applyGPUMIGCursor(c echo.Context) (GPUMIGCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return GPUMIGCursor{}, false, nil
	}
	cursor, err := DecodeGPUMIGCursor(after)
	if err != nil {
		return GPUMIGCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func pvcNextCursor(orderCol string, last PVCRecommendationResponse, sortValue interface{}) string {
	return EncodePVCCursor(PVCCursor{
		ClusterUUID:           last.ClusterUUID,
		Namespace:             last.Namespace,
		PersistentVolumeClaim: last.PersistentVolumeClaim,
		SortValue:             model.PaginationSortValueJSON(sortValue),
	})
}

func nodeUtilNextCursor(last model.NodeUtilizationRec, sortValue interface{}) string {
	return EncodeNodeUtilCursor(NodeUtilCursor{
		ClusterUUID: last.ClusterUUID,
		Node:        last.Node,
		SortValue:   model.PaginationSortValueJSON(sortValue),
	})
}

func nodeGPUNextCursor(last model.NodeGPURecommendation, sortValue interface{}) string {
	return EncodeNodeGPUCursor(NodeGPUCursor{
		ClusterUUID: last.ClusterUUID,
		NodeName:    last.NodeName,
		GPUModel:    last.GPUModel,
		Term:        last.Term,
		SortValue:   model.PaginationSortValueJSON(sortValue),
	})
}

func gpuMIGNextCursor(last model.GPUMIGRecommendationEntry, sortValue interface{}) string {
	return EncodeGPUMIGCursor(GPUMIGCursor{
		ClusterUUID: last.ClusterUUID,
		Namespace:   last.Namespace,
		Container:   last.Container,
		GPUModel:    last.GPUModel,
		Term:        last.Term,
		SortValue:   model.PaginationSortValueJSON(sortValue),
	})
}

func applyQuotaCursor(c echo.Context) (QuotaCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return QuotaCursor{}, false, nil
	}
	cursor, err := DecodeQuotaCursor(after)
	if err != nil {
		return QuotaCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func applyClusterQuotaCursor(c echo.Context) (ClusterQuotaCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return ClusterQuotaCursor{}, false, nil
	}
	cursor, err := DecodeClusterQuotaCursor(after)
	if err != nil {
		return ClusterQuotaCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func applyVMCursor(c echo.Context) (VMCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return VMCursor{}, false, nil
	}
	cursor, err := DecodeVMCursor(after)
	if err != nil {
		return VMCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func applyMachineSetCursor(c echo.Context) (MachineSetCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return MachineSetCursor{}, false, nil
	}
	cursor, err := DecodeMachineSetCursor(after)
	if err != nil {
		return MachineSetCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}

func applySnapshotCursor(c echo.Context) (SnapshotCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return SnapshotCursor{}, false, nil
	}
	cursor, err := DecodeSnapshotCursor(after)
	if err != nil {
		return SnapshotCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	return cursor, true, nil
}
