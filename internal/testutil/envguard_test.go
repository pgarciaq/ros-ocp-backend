// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0
package testutil

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// realKeyUsers is the observed /proc/key-users shape on the affected
// workstation (#902): fields after "uid:" are key count, count/maxkeys,
// and the byte quotas.
const realKeyUsers = `    0:    44 44/200 44/200 12345/20000
4203706:   107 107/200 107/200 6627/20000
`

func readFileReturning(content string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(content), nil }
}

func readFileFailing(err error) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return nil, err }
}

func TestParseKeyringUsage(t *testing.T) {
	used, max, ok := parseKeyringUsage(realKeyUsers, 4203706)
	if !ok || used != 107 || max != 200 {
		t.Fatalf("parse: got (%d, %d, %v), want (107, 200, true)", used, max, ok)
	}
	// A different uid's line must not match.
	if _, _, ok := parseKeyringUsage(realKeyUsers, 999); ok {
		t.Fatal("parse: unknown uid must report not-ok")
	}
}

// keyUsersLine builds a /proc/key-users-shaped line for the uid the test
// process actually runs as — checkKeyringBudget parses for os.Getuid(),
// so a hardcoded uid made these tests pass on the workstation that
// authored them and fail on CI (whose runner uid differs). maxkeys is
// the universally observed default (200, /proc/sys/kernel/keys/maxkeys).
func keyUsersLine(count int) string {
	uid := os.Getuid()
	return fmt.Sprintf("%d:   %d %d/%d %d/200 12672/20000\n", uid, count, count, count, count)
}

func TestCheckKeyringBudgetPassesOnHealthyBudget(t *testing.T) {
	// The post-cleanup state observed live: 7/200.
	if err := checkKeyringBudget(readFileReturning(keyUsersLine(7))); err != nil {
		t.Fatalf("healthy budget must pass, got: %v", err)
	}
	// 107/200 (54%) — the leak state observed before the failure threshold.
	if err := checkKeyringBudget(readFileReturning(keyUsersLine(107))); err != nil {
		t.Fatalf("half-full budget must pass, got: %v", err)
	}
}

func TestCheckKeyringBudgetFailsWhenNearlyFull(t *testing.T) {
	// The failure state from the issue: 194/200. The guard must fail with
	// ONE actionable message naming the reclaim command and the root cause,
	// so the mass setup failure cannot be misread as a code regression.
	err := checkKeyringBudget(readFileReturning(keyUsersLine(194)))
	if err == nil {
		t.Fatal("nearly-full budget must fail")
	}
	msg := err.Error()
	for _, want := range []string{"keyring", "194/200", "podman rm -f", "testcontainers", "#902"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message must mention %q, got: %s", want, msg)
		}
	}
}

func TestCheckKeyringBudgetSkipsWhenUnreadable(t *testing.T) {
	// No /proc/key-users (macOS, non-Linux CI): skip, never fail.
	if err := checkKeyringBudget(readFileFailing(errors.New("no such file"))); err != nil {
		t.Fatalf("unreadable /proc/key-users must skip, got: %v", err)
	}
	// Unparseable content: skip rather than fail the suite.
	if err := checkKeyringBudget(readFileReturning("garbage\n")); err != nil {
		t.Fatalf("unparseable content must skip, got: %v", err)
	}
}

func TestCheckKeyringBudgetEdgeThresholds(t *testing.T) {
	// 159/200 = 79.5% passes; 160/200 = 80% fails — the threshold contract.
	if err := checkKeyringBudget(readFileReturning(keyUsersLine(159))); err != nil {
		t.Fatalf("79.5%% must pass, got: %v", err)
	}
	if err := checkKeyringBudget(readFileReturning(keyUsersLine(160))); err == nil {
		t.Fatal("80% must fail")
	}
}
