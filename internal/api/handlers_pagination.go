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

func buildContainerListMeta(c echo.Context, page model.NativeListPage, opts listoptions.ListOptions) *Collection {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = containerNextCursor(page)
	}

	return PaginatedCollectionResponse(nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
}

func buildNamespaceListMeta(c echo.Context, page model.NativeNamespaceListPage, opts listoptions.ListOptions) *Collection {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = namespaceNextCursor(page)
	}

	return PaginatedCollectionResponse(nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
}
