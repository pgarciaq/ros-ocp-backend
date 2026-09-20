// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

// Package hcp is the executable form of the locked W1 filter rules (#583,
// ADR-0331 update) plus the guardrail profile (#584): on Agent-platform
// management clusters, HCP-namespace membership routes container groups to
// controlplane floors — max(100m CPU / 128MiB absolute, 70% of window-median
// current request) — on cost and perf alike, replica recs suppressed, no
// group ever excluded. The workload pin doubles as tripwire for scope
// drift. Pure data-plane helpers only: callers map their own row types in,
// so the package stays free of pipeline dependencies until W1 is greenlit.
package hcp
