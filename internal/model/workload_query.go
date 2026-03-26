package model

import "fmt"

// WorkloadQuery captures parameters for querying workloads by organization.
// Callers use this with GORM or raw SQL to scope workloads to an org_id.
type WorkloadQuery struct {
	OrgID string
}

// BuildGetWorkloadsByOrgIDQuery validates orgID and returns a WorkloadQuery for DB lookups.
func BuildGetWorkloadsByOrgIDQuery(orgID string) (WorkloadQuery, error) {
	if orgID == "" {
		return WorkloadQuery{}, fmt.Errorf("org_id is required")
	}
	return WorkloadQuery{OrgID: orgID}, nil
}
