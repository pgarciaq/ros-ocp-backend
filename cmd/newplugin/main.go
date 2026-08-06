// Command newplugin scaffolds a production recommendation plugin under
// internal/plugins/<name>/, wires a blank import in internal/plugins/plugins.go,
// and prints a post-scaffold checklist.
//
// Usage:
//
//	go run ./cmd/newplugin -name machineset
//	go run ./cmd/newplugin -name foo-bar -phase enrich -priority 40 -traits csv,terms
//	go run ./cmd/newplugin -name demo -dry-run
//
// Or via Make:
//
//	make new-plugin NAME=machineset
//	make new-plugin NAME=foo PHASE=optimize PRIORITY=20 TRAITS=csv,terms DRY_RUN=1
//
// Default live traits: Plugin, APIProvider, RetentionProvider.
// Optional traits are emitted as commented stubs (or live when listed in -traits).
// See GitHub issue #410 and docs-site/development.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func main() {
	fs := flag.NewFlagSet("newplugin", flag.ExitOnError)
	name := fs.String("name", envOr("NAME", ""), "plugin id / directory ([a-z][a-z0-9-]*); also NAME=")
	phase := fs.String("phase", envOr("PHASE", "produce"), "produce|enrich|optimize; also PHASE=")
	priorityStr := fs.String("priority", envOr("PRIORITY", "50"), "priority within phase (lower runs first); also PRIORITY=")
	traits := fs.String("traits", envOr("TRAITS", ""), "comma-separated extra live traits: csv,ingest-hook,api,enrich,retention,terms")
	dryRun := fs.Bool("dry-run", envTruthy("DRY_RUN"), "print planned actions without writing; also DRY_RUN=1")
	repoRoot := fs.String("repo-root", "", "repository root (default: walk up from cwd looking for go.mod)")
	_ = fs.Parse(os.Args[1:])

	priority, err := strconv.Atoi(*priorityStr)
	if err != nil {
		fail("invalid -priority / PRIORITY: %v", err)
	}

	root := *repoRoot
	if root == "" {
		root, err = findRepoRoot()
		if err != nil {
			fail("%v", err)
		}
	}

	plan, err := BuildPlan(root, *name, *phase, priority, *traits)
	if err != nil {
		fail("%v", err)
	}

	fmt.Print(plan.summarize(*dryRun))
	if *dryRun {
		fmt.Println("(dry-run: no files written)")
		fmt.Print(checklist(plan.Opts))
		return
	}

	if err := plan.Apply(); err != nil {
		fail("write: %v", err)
	}
	fmt.Print(checklist(plan.Opts))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "newplugin: "+format+"\n", args...)
	os.Exit(1)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envTruthy(key string) bool {
	v := os.Getenv(key)
	return v == "1" || v == "true" || v == "TRUE" || v == "yes" || v == "YES"
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod; pass -repo-root")
		}
		dir = parent
	}
}
