// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"sort"

	roscsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// KnownWorkloads pins the named-workload inventory of the live Agent lab
// (package 20260914T153624, #583 evidence). It is a tripwire, not a filter:
// W1 includes by namespace, and any name outside this set must be reviewed
// before inclusion. The live CSVs' blank workload value (owner-join gap) is
// deliberately absent: blanks skip UnknownWorkloads by design.
var KnownWorkloads = map[string]struct{}{
	"capi-provider":                      {},
	"catalog-operator":                   {},
	"certified-operators-catalog":        {},
	"cluster-api":                        {},
	"cluster-image-registry-operator":    {},
	"cluster-network-operator":           {},
	"cluster-node-tuning-operator":       {},
	"cluster-policy-controller":          {},
	"cluster-storage-operator":           {},
	"cluster-version-operator":           {},
	"community-operators-catalog":        {},
	"control-plane-operator":             {},
	"control-plane-pki-operator":         {},
	"csi-snapshot-controller":            {},
	"csi-snapshot-controller-operator":   {},
	"dns-operator":                       {},
	"etcd":                               {},
	"hosted-cluster-config-operator":     {},
	"ignition-server":                    {},
	"ignition-server-proxy":              {},
	"ingress-operator":                   {},
	"konnectivity-agent":                 {},
	"kube-apiserver":                     {},
	"kube-controller-manager":            {},
	"kube-scheduler":                     {},
	"kube-storage-version-migrator":      {},
	"machine-approver":                   {},
	"multus-admission-controller":        {},
	"network-node-identity":              {},
	"oauth-openshift":                    {},
	"olm-collect-profiles":               {},
	"olm-operator":                       {},
	"openshift-apiserver":                {},
	"openshift-controller-manager":       {},
	"openshift-oauth-apiserver":          {},
	"openshift-route-controller-manager": {},
	"ovnkube-control-plane":              {},
	"packageserver":                      {},
	"redhat-operators-catalog":           {},
}

// UnknownWorkloads returns the sorted distinct workload names in rows that
// are absent from the pinned inventory. Blank names (CSV owner-join gap)
// are skipped: they classify by namespace, never by tripwire.
func UnknownWorkloads(rows []roscsv.Row) []string {
	var unknown []string
	seen := map[string]bool{}
	for _, row := range rows {
		name := row.WorkloadName
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if _, ok := KnownWorkloads[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}
