package vm

import (
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
)

// buildNodeMemoryGiBMap returns the latest per-node allocatable memory (GiB) from node digests.
func buildNodeMemoryGiBMap(rows []node.DigestRow) map[string]float64 {
	latest := make(map[string]node.DigestRow)
	for _, r := range rows {
		node := strings.TrimSpace(r.Node)
		if node == "" {
			continue
		}
		if prev, ok := latest[node]; !ok || r.BucketDate.After(prev.BucketDate) {
			latest[node] = r
		}
	}
	out := make(map[string]float64, len(latest))
	for node, r := range latest {
		if r.MaxMemAllocKiB == nil || *r.MaxMemAllocKiB <= 0 {
			continue
		}
		out[node] = float64(*r.MaxMemAllocKiB) / float64(kibPerGiB)
	}
	return out
}

// resolveNUMANodeMemoryGiB returns the assumed per-NUMA-node memory cap for a VM's host.
// Uses node allocatable memory / NUMAAssumedSockets when node data exists; otherwise NUMANodeMemoryGiB.
func resolveNUMANodeMemoryGiB(nodeName string, nodeMemGiBByNode map[string]float64, cfg VMRecConfig) float64 {
	fallback := cfg.NUMANodeMemoryGiB
	if fallback <= 0 {
		fallback = 64
	}
	node := strings.TrimSpace(nodeName)
	if node == "" || nodeMemGiBByNode == nil {
		return fallback
	}
	totalGiB, ok := nodeMemGiBByNode[node]
	if !ok || totalGiB <= 0 {
		return fallback
	}
	sockets := cfg.NUMAAssumedSockets
	if sockets < 1 {
		sockets = 2
	}
	return totalGiB / float64(sockets)
}

// BuildNodeMemoryGiBMap returns the latest per-node allocatable memory (GiB) from node digests.
func BuildNodeMemoryGiBMap(rows []node.DigestRow) map[string]float64 {
	return buildNodeMemoryGiBMap(rows)
}
