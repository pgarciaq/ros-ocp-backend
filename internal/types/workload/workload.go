package workload

import (
	"database/sql/driver"
	"fmt"
)

// WorkloadType is the owner kind of a workload (e.g. "deployment", "statefulset").
// As of COST-7274 this is no longer restricted to a fixed set; any valid Kubernetes
// owner kind string is accepted.
type WorkloadType string

// Well-known workload types retained as convenience constants for tests, seed data,
// and default idle exclusions. These do NOT form an exhaustive allowlist.
const (
	Daemonset             WorkloadType = "daemonset"
	Deployment            WorkloadType = "deployment"
	Deploymentconfig      WorkloadType = "deploymentconfig"
	Replicaset            WorkloadType = "replicaset"
	Replicationcontroller WorkloadType = "replicationcontroller"
	Statefulset           WorkloadType = "statefulset"
	Namespace             WorkloadType = "namespace"
)

func (p *WorkloadType) Scan(value interface{}) error {
	if value == nil {
		*p = ""
		return nil
	}
	strVal, ok := value.(string)
	if !ok {
		return fmt.Errorf("WorkloadType.Scan: expected string, got %T", value)
	}
	*p = WorkloadType(strVal)
	return nil
}

func (p WorkloadType) Value() (driver.Value, error) {
	return string(p), nil
}

func (p WorkloadType) String() string {
	return string(p)
}
