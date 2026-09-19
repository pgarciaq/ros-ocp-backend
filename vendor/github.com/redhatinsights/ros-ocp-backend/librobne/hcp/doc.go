// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

// Package hcp is the executable form of the locked W1 filter rules (#583,
// ADR-0331 update): on Agent-platform management clusters, HCP-namespace
// membership is the filter for management control-plane container rows, and
// the pinned live-lab workload inventory is the tripwire for scope drift.
// Pure data-plane helpers only: callers map their own row types in, so the
// package stays free of pipeline dependencies until W1 is greenlit.
package hcp
