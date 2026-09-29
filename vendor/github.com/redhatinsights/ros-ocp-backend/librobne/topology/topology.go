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

	// HCPSnapshot carries the report-scoped namespace-to-HostedCluster mapping
	// (#621/#622). Each entry pins one HCP namespace to the hosted incarnation
	// observed live at collection time; absent on pre-snapshot operators.
	HCPSnapshot []HCPSnapshotEntry `json:"hcpSnapshot,omitempty"`
}

// HCPSnapshotEntry pins one HCP namespace to one hosted incarnation.
// Plain scalars only (see TopologyFacts): no Kubernetes types, immune to
// per-cluster API version skew. JSON tags match the operator emission.
type HCPSnapshotEntry struct {
	// HCPNamespace is the management-cluster namespace serving the HC.
	HCPNamespace string `json:"hcpNamespace"`
	// HostedClusterID is HostedCluster spec.clusterID, cross-checked against
	// exactly one live HostedCluster. Empty means unproven.
	HostedClusterID string `json:"hostedClusterID,omitempty"`
	// HcUID is the HostedCluster object UID (incarnation signal).
	HcUID string `json:"hcUID,omitempty"`
	// HcpUID is the HostedControlPlane object UID (incarnation signal).
	HcpUID string `json:"hcpUID,omitempty"`
	// NamespaceUID is the HCP Namespace object UID (recreation signal).
	NamespaceUID string `json:"namespaceUID,omitempty"`
	// NamespaceCreatedAt is the HCP Namespace creation time (RFC3339).
	NamespaceCreatedAt string `json:"namespaceCreatedAt,omitempty"`
	// ObservedAt is the collection observation time (RFC3339).
	ObservedAt string `json:"observedAt,omitempty"`
	// Complete is false when any required inventory read failed; such
	// entries must not associate recommendations.
	Complete bool `json:"complete,omitempty"`
	// Diagnostics names the failed reads for incomplete entries.
	Diagnostics string `json:"diagnostics,omitempty"`
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
