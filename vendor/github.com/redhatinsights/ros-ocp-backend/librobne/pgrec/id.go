package pgrec

import (
	"fmt"

	"github.com/google/uuid"
)

// NativeIDNamespace matches internal/model/types so container_id stays stable.
var NativeIDNamespace = uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")

// NativeContainerID generates a deterministic UUID v5 for a container recommendation.
func NativeContainerID(clusterUUID, namespace, workload, workloadType, container string) string {
	name := fmt.Sprintf("%s/%s/%s/%s/%s", clusterUUID, namespace, workload, workloadType, container)
	return uuid.NewSHA1(NativeIDNamespace, []byte(name)).String()
}
