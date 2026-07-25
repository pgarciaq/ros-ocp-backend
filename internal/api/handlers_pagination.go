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
	if cursorSortMismatch(cursor.OrderBy, opts.OrderBy, cursor.SortValue) {
		return nil // sort column changed; discard stale cursor, start from page 1
	}
	opts.HasCursor = true
	opts.AfterNamespace = cursor.Namespace
	opts.AfterWorkload = cursor.Workload
	opts.AfterWorkloadType = cursor.WorkloadType
	opts.AfterContainer = cursor.ContainerName
	opts.AfterContainerClusterUUID = cursor.ClusterUUID
	opts.AfterContainerSortPresent = true
	if len(cursor.SortValue) > 0 {
		sortVal, decodeErr := model.DecodePaginationSortValue(cursor.SortValue)
		if decodeErr != nil {
			return fmt.Errorf("invalid after parameter: %w", decodeErr)
		}
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
	if cursorSortMismatch(cursor.OrderBy, opts.OrderBy, cursor.SortValue) {
		return nil // sort column changed; discard stale cursor, start from page 1
	}
	opts.HasCursor = true
	opts.AfterNamespaceName = cursor.Namespace
	opts.AfterNSClusterUUID = cursor.ClusterUUID
	opts.AfterNSSortPresent = true
	if len(cursor.SortValue) > 0 {
		sortVal, decodeErr := model.DecodePaginationSortValue(cursor.SortValue)
		if decodeErr != nil {
			return fmt.Errorf("invalid after parameter: %w", decodeErr)
		}
		opts.AfterNSSortValue = sortVal
	}
	return nil
}

func containerNextCursor(page model.NativeListPage, orderBy string) string {
	if !page.HasNext || page.LastAnchor == nil {
		return ""
	}
	anchor := page.LastAnchor
	return EncodeContainerCursor(ContainerCursor{
		Namespace:     anchor.Namespace,
		Workload:      anchor.Workload,
		WorkloadType:  anchor.WorkloadType,
		ContainerName: anchor.ContainerName,
		ClusterUUID:   anchor.ClusterUUID,
		SortValue:     model.PaginationSortValueJSON(anchor.SortValue),
		OrderBy:       orderBy,
	})
}

func namespaceNextCursor(page model.NativeNamespaceListPage, orderBy string) string {
	if !page.HasNext || page.LastAnchor == nil {
		return ""
	}
	anchor := page.LastAnchor
	return EncodeNamespaceCursor(NamespaceCursor{
		Namespace:   anchor.Namespace,
		ClusterUUID: anchor.ClusterUUID,
		SortValue:   model.PaginationSortValueJSON(anchor.SortValue),
		OrderBy:     orderBy,
	})
}

func buildContainerListMeta(c echo.Context, page model.NativeListPage, opts listoptions.ListOptions, currency string) *Collection[*model.ListResponse] {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = containerNextCursor(page, opts.OrderBy)
	}

	resp := PaginatedCollectionResponse[*model.ListResponse](nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
	resp.Meta.Currency = currency
	return resp
}

func buildNamespaceDetailListMeta(c echo.Context, page model.NativeNamespaceListPage, opts listoptions.ListOptions, currency string) *Collection[*model.NamespaceDetailResponse] {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = namespaceNextCursor(page, opts.OrderBy)
	}

	resp := PaginatedCollectionResponse[*model.NamespaceDetailResponse](nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
	resp.Meta.Currency = currency
	return resp
}

func buildNamespaceSlimListMeta(c echo.Context, page model.NativeNamespaceListPage, opts listoptions.ListOptions, currency string) *Collection[*model.NamespaceListResponse] {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = namespaceNextCursor(page, opts.OrderBy)
	}

	resp := PaginatedCollectionResponse[*model.NamespaceListResponse](nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
	resp.Meta.Currency = currency
	return resp
}

func applyPVCCursor(c echo.Context, orderBy string) (PVCCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return PVCCursor{}, false, nil
	}
	cursor, err := DecodePVCCursor(after)
	if err != nil {
		return PVCCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return PVCCursor{}, false, nil
	}
	return cursor, true, nil
}

func applyNodeUtilCursor(c echo.Context, orderBy string) (NodeUtilCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return NodeUtilCursor{}, false, nil
	}
	cursor, err := DecodeNodeUtilCursor(after)
	if err != nil {
		return NodeUtilCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return NodeUtilCursor{}, false, nil
	}
	return cursor, true, nil
}

func applyNodeGPUCursor(c echo.Context, orderBy string) (NodeGPUCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return NodeGPUCursor{}, false, nil
	}
	cursor, err := DecodeNodeGPUCursor(after)
	if err != nil {
		return NodeGPUCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return NodeGPUCursor{}, false, nil
	}
	return cursor, true, nil
}

func applyGPUMIGCursor(c echo.Context, orderBy string) (GPUMIGCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return GPUMIGCursor{}, false, nil
	}
	cursor, err := DecodeGPUMIGCursor(after)
	if err != nil {
		return GPUMIGCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return GPUMIGCursor{}, false, nil
	}
	return cursor, true, nil
}

func pvcNextCursor(orderCol string, last PVCRecommendationResponse, sortValue interface{}) string {
	return EncodePVCCursor(PVCCursor{
		ClusterUUID:           last.ClusterUUID,
		Namespace:             last.Namespace,
		PersistentVolumeClaim: last.PersistentVolumeClaim,
		SortValue:             model.PaginationSortValueJSON(sortValue),
		OrderBy:               orderCol,
	})
}

func nodeUtilNextCursor(last model.NodeUtilizationRec, sortValue interface{}, orderBy string) string {
	return EncodeNodeUtilCursor(NodeUtilCursor{
		ClusterUUID: last.ClusterUUID,
		Node:        last.Node,
		SortValue:   model.PaginationSortValueJSON(sortValue),
		OrderBy:     orderBy,
	})
}

func nodeGPUNextCursor(last model.NodeGPURecommendation, sortValue interface{}, orderBy string) string {
	return EncodeNodeGPUCursor(NodeGPUCursor{
		ClusterUUID: last.ClusterUUID,
		NodeName:    last.NodeName,
		GPUModel:    last.GPUModel,
		Term:        last.Term,
		SortValue:   model.PaginationSortValueJSON(sortValue),
		OrderBy:     orderBy,
	})
}

func gpuMIGNextCursor(last model.GPUMIGRecommendationEntry, sortValue interface{}, orderBy string) string {
	return EncodeGPUMIGCursor(GPUMIGCursor{
		ClusterUUID: last.ClusterUUID,
		Namespace:   last.Namespace,
		Container:   last.Container,
		GPUModel:    last.GPUModel,
		Term:        last.Term,
		SortValue:   model.PaginationSortValueJSON(sortValue),
		OrderBy:     orderBy,
	})
}

func applyQuotaCursor(c echo.Context, orderBy string) (QuotaCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return QuotaCursor{}, false, nil
	}
	cursor, err := DecodeQuotaCursor(after)
	if err != nil {
		return QuotaCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return QuotaCursor{}, false, nil
	}
	return cursor, true, nil
}

func applyClusterQuotaCursor(c echo.Context, orderBy string) (ClusterQuotaCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return ClusterQuotaCursor{}, false, nil
	}
	cursor, err := DecodeClusterQuotaCursor(after)
	if err != nil {
		return ClusterQuotaCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return ClusterQuotaCursor{}, false, nil
	}
	return cursor, true, nil
}

func applyVMCursor(c echo.Context, orderBy string) (VMCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return VMCursor{}, false, nil
	}
	cursor, err := DecodeVMCursor(after)
	if err != nil {
		return VMCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return VMCursor{}, false, nil
	}
	return cursor, true, nil
}

func applyMachineSetCursor(c echo.Context, orderBy string) (MachineSetCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return MachineSetCursor{}, false, nil
	}
	cursor, err := DecodeMachineSetCursor(after)
	if err != nil {
		return MachineSetCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return MachineSetCursor{}, false, nil
	}
	return cursor, true, nil
}

func applySnapshotCursor(c echo.Context, orderBy string) (SnapshotCursor, bool, error) {
	after := c.QueryParam("after")
	if after == "" {
		return SnapshotCursor{}, false, nil
	}
	cursor, err := DecodeSnapshotCursor(after)
	if err != nil {
		return SnapshotCursor{}, false, fmt.Errorf("invalid after parameter: %w", err)
	}
	if cursorSortMismatch(cursor.OrderBy, orderBy, cursor.SortValue) {
		return SnapshotCursor{}, false, nil
	}
	return cursor, true, nil
}

// compareSortValues is a generic comparator for cursor sort values (string or float32).
func compareSortValues(a, b interface{}) int {
	switch av := a.(type) {
	case string:
		bv, _ := b.(string)
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
		return 0
	case float32:
		var bf float32
		switch x := b.(type) {
		case float32:
			bf = x
		case float64:
			bf = float32(x)
		}
		if av < bf {
			return -1
		}
		if av > bf {
			return 1
		}
		return 0
	default:
		return 0
	}
}
