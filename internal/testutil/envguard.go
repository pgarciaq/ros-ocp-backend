// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0
package testutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// keyringBudgetFailPct is the kernel-keyring usage at which the test
// environment guard fails the run. Each testcontainers postgres
// container holds a live crun join keyring; with the reaper unable to
// run under rootless podman (see the guard message), containers leak one
// per test process until the per-user budget (/proc/sys/kernel/keys/
// maxkeys, 200 by default) is exhausted — after which every container
// start fails with "Disk quota exceeded" and the suite dies as a mass
// setup failure that reads as a code regression (#902). 80% leaves
// headroom for the current run's containers and other user keys.
const keyringBudgetFailPct = 80

// checkKeyringBudget fails when the calling user's kernel keyring budget
// is too exhausted to start containers, with one actionable message
// naming the root cause and the reclaim command — the difference between
// "my environment is broken" and sixty identical "--- FAIL" blocks that
// get read as a code regression (#902).
//
// It reads (a /proc/key-users-shaped) file through readFile so tests can
// inject synthetic content. No /proc/key-users (macOS, non-Linux CI) or
// an unparseable file skips the check: the guard must never itself break
// a working environment.
func checkKeyringBudget(readFile func(string) ([]byte, error)) error {
	content, err := readFile("/proc/key-users")
	if err != nil {
		return nil
	}
	used, max, ok := parseKeyringUsage(string(content), os.Getuid())
	if !ok || max <= 0 {
		return nil
	}
	if used*100 < max*keyringBudgetFailPct {
		return nil
	}
	return fmt.Errorf(
		"test environment: kernel keyring budget %d/%d (%d%%) — the next container start "+
			"will fail as a mass setup regression that looks like a code failure (#902). "+
			"Root cause: leaked testcontainers postgres containers — the Ryuk reaper cannot "+
			"run under rootless podman (its container dies within a minute), so "+
			"TESTCONTAINERS_RYUK_DISABLED must stay set and each test process leaks its shared "+
			"container. Reclaim: podman rm -f $(podman ps -aq --filter label=org.testcontainers=true)",
		used, max, used*100/max,
	)
}

// parseKeyringUsage extracts (used, maxkeys) for uid from /proc/key-users
// content. Line shape (observed live): "<uid>: <nkeys> <nikeys>/<nkeys>
// <qnkeys>/<maxkeys> <qnbytes>/<maxbytes>" — the SECOND ratio group is the
// quota that blocks new keys (its denominator matches
// /proc/sys/kernel/keys/maxkeys); the first is instantiated/total, which
// reads 7/7 on a healthy host and would false-trigger the guard.
func parseKeyringUsage(content string, uid int) (used, max int, ok bool) {
	prefix := strconv.Itoa(uid) + ":"
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return 0, 0, false
		}
		parts := strings.SplitN(fields[3], "/", 2)
		if len(parts) != 2 {
			return 0, 0, false
		}
		u, errU := strconv.Atoi(parts[0])
		m, errM := strconv.Atoi(parts[1])
		if errU != nil || errM != nil {
			return 0, 0, false
		}
		return u, m, true
	}
	return 0, 0, false
}
