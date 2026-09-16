// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package topology

// ClusterTopology is the W0 classification of a cluster. The zero value is
// TopologyUnknown: absent or unrecognized facts never error, they degrade.
type ClusterTopology string

const (
	TopologyUnknown    ClusterTopology = "unknown"
	TopologyDedicated  ClusterTopology = "dedicated"
	TopologyHosted     ClusterTopology = "hosted"
	TopologyManagement ClusterTopology = "management"
)

// String returns the persistence/API spelling. Unknown spellings degrade to
// "unknown" so forward-compatible callers never emit an empty value.
func (t ClusterTopology) String() string {
	switch t {
	case TopologyDedicated, TopologyHosted, TopologyManagement:
		return string(t)
	default:
		return string(TopologyUnknown)
	}
}

// ParseClusterTopology maps a stored spelling back to a class. Anything
// outside the ADR-0328 vocabulary (including "") degrades to unknown.
func ParseClusterTopology(s string) ClusterTopology {
	switch ClusterTopology(s) {
	case TopologyDedicated, TopologyHosted, TopologyManagement:
		return ClusterTopology(s)
	default:
		return TopologyUnknown
	}
}

// TopologyFacts is the shared input contract for Classify. All three future
// consumers (service backend from manifests, CLI from payloads, in-cluster
// operator from live API reads) map their own sources into this struct:
// plain scalars only, no Kubernetes types, so the library stays free of
// client-go/apimachinery and immune to per-cluster API version skew.
// JSON tags match the operator manifest envelope (cr_status.topology).
type TopologyFacts struct {
	// ControlPlaneTopology mirrors Infrastructure.status.controlPlaneTopology
	// (HighlyAvailable, External, SingleReplica, ...). Empty when unreadable.
	ControlPlaneTopology string `json:"controlPlaneTopology,omitempty"`

	// ManagedByHypershift mirrors the hypershift.openshift.io/managed label.
	ManagedByHypershift bool `json:"managedByHypershift,omitempty"`

	// HostedClusterCount is the number of visible HostedCluster objects.
	HostedClusterCount int `json:"hostedClusterCount,omitempty"`

	// HostedControlPlaneNamespaces lists namespaces carrying the
	// hypershift.openshift.io/hosted-control-plane=true label.
	HostedControlPlaneNamespaces []string `json:"hostedControlPlaneNamespaces,omitempty"`
}

// Classify maps facts to a topology following ADR-0328: a management plane
// hosts control planes for others (HighlyAvailable plus hosted evidence), a
// hosted plane runs its control plane elsewhere (External, regardless of the
// managed flag — no management cluster is ever External), anything else
// standalone is dedicated, and absent or unrecognized facts are unknown.
func Classify(f TopologyFacts) ClusterTopology {
	if f.ControlPlaneTopology == "External" {
		return TopologyHosted
	}
	if f.ControlPlaneTopology == "HighlyAvailable" || f.ControlPlaneTopology == "SingleReplica" {
		if f.HostedClusterCount > 0 || len(f.HostedControlPlaneNamespaces) > 0 {
			return TopologyManagement
		}
		return TopologyDedicated
	}
	return TopologyUnknown
}
