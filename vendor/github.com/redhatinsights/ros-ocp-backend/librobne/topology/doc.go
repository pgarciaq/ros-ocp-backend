// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

// Package topology classifies OpenShift cluster topology for HCP/fleet
// recommendations (W0). It is intentionally free of Kubernetes API
// dependencies: callers map live objects (operators) or manifests (service,
// CLI) into TopologyFacts and share this single classifier, so no plane can
// disagree with another about what a cluster is.
package topology
