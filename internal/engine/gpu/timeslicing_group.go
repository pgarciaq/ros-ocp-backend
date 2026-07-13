package gpu

import (
	"time"
)

// GroupGPURecsByNodeAndModel groups GPU container recommendations by node × GPU model × term.
func GroupGPURecsByNodeAndModel(
	gpuRecs map[GPUContainerKey][]*GPURec,
	nodeMap map[GPUContainerKey]string,
	nodeLastSeen map[string]time.Time,
	clusterUUID string,
) []NodeGPUGroup {
	type groupKey struct {
		node  string
		model string
		term  string
	}
	grouped := map[groupKey]*NodeGPUGroup{}

	for key, recs := range gpuRecs {
		nodeName := nodeMap[key]
		if nodeName == "" {
			continue
		}

		for _, rec := range recs {
			gk := groupKey{node: nodeName, model: rec.GPUModelName, term: rec.Term}
			g, ok := grouped[gk]
			if !ok {
				g = &NodeGPUGroup{
					NodeName:    nodeName,
					ClusterUUID: clusterUUID,
					GPUModel:    rec.GPUModelName,
					Term:        rec.Term,
					LastSeen:    nodeLastSeen[nodeName],
				}
				grouped[gk] = g
			}
			g.Containers = append(g.Containers, NodeGPUContainer{
				Namespace: key.Namespace,
				Workload:  key.Workload,
				Container: key.ContainerName,
				Rec:       rec,
			})
		}
	}

	result := make([]NodeGPUGroup, 0, len(grouped))
	for _, g := range grouped {
		result = append(result, *g)
	}
	return result
}
