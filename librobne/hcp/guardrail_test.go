// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// GuardrailFor is the routing predicate shared by generation (profile
// choice now) and future exposure (labeling later): one function, two
// call sites, no stored tag. Namespace membership alone decides.
func TestGuardrailFor(t *testing.T) {
	set := NewNamespaceSet([]string{"hc01-infra-hc01"})
	cases := []struct {
		ns   string
		set  map[string]bool
		want bool
		why  string
	}{
		// HCP-namespace rows take the guardrail profile.
		{"hc01-infra-hc01", set, true, "known HCP namespace must route to guardrails"},
		// Every other namespace takes the generic profile: the predicate
		// never inspects pod or workload names.
		{"openshift-etcd", set, false, "non-HCP namespace must stay generic despite etcd-named pods"},
		{"kube-system", set, false, "non-HCP namespace must stay generic"},
		// HCP-shaped but unknown: exact membership, not pattern.
		{"hc02-infra-hc02", set, false, "unknown HCP-shaped namespace must stay generic"},
		// No known namespaces (or nil set): guardrails off, never error.
		{"hc01-infra-hc01", nil, false, "nil set must route generic without panic"},
		{"", set, false, "empty namespace must route generic"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, GuardrailFor(tc.ns, tc.set), "%s", tc.why)
	}
}
