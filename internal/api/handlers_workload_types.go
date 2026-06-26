package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// GetWorkloadTypes handles GET /recommendations/openshift/workload-types.
// Returns distinct workload_type values for the authenticated org.
func GetWorkloadTypes(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	ctx := c.Request().Context()
	pool := db.GetPool()

	rows, err := pool.Query(ctx,
		`SELECT DISTINCT workload_type
		 FROM org_container_keys
		 WHERE org_id = $1 AND workload_type != ''
		 ORDER BY workload_type`,
		orgID,
	)
	if err != nil {
		hlog.Errorf("failed to query workload types: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "failed to query workload types",
		})
	}
	defer rows.Close()

	var types []string
	for rows.Next() {
		var wt string
		if err := rows.Scan(&wt); err != nil {
			hlog.Errorf("failed to scan workload type row: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "failed to read workload types",
			})
		}
		types = append(types, wt)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("workload type rows iteration error: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "failed to read workload types",
		})
	}

	if types == nil {
		types = []string{}
	}

	return c.JSON(http.StatusOK, echo.Map{
		"data": types,
	})
}
