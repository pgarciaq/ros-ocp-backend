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
	return nil
}

func containerNextCursor(results []model.NativeContainerResult) string {
	if len(results) == 0 {
		return ""
	}
	last := results[len(results)-1]
	return EncodeContainerCursor(ContainerCursor{
		Namespace:     last.Project,
		Workload:      last.Workload,
		ContainerName: last.Container,
	})
}

func namespaceNextCursor(results []model.NativeNamespaceResult) string {
	if len(results) == 0 {
		return ""
	}
	last := results[len(results)-1]
	return EncodeNamespaceCursor(NamespaceCursor{
		Namespace:   last.Project,
		ClusterUUID: last.ClusterUUID,
	})
}

func buildContainerListMeta(c echo.Context, page model.NativeListPage, opts listoptions.ListOptions) *Collection {
	offset := opts.Offset
	if opts.HasCursor {
		offset = 0
	}

	nextCursor := ""
	if page.HasNext {
		nextCursor = containerNextCursor(page.Results)
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
		nextCursor = namespaceNextCursor(page.Results)
	}

	return PaginatedCollectionResponse(nil, c.Request(), page.Count, opts.Limit, offset, page.HasNext, nextCursor)
}
