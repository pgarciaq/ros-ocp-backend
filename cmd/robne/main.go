// robne is a standalone CLI that computes OpenShift container recommendations
// from local ROS CSVs using librobne (Phase 1 stdout; Phase 2a optional Postgres).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "robne",
		Short: "Offline/batch OpenShift recommendations (librobne)",
		Long: `robne reads NISE or operator ROS container CSVs (directory, file, or .tar.gz)
and writes container recommendations to stdout. --output postgres:// upserts
into a dedicated database this CLI owns (use case c; --apply-schema on bootstrap/upgrade).
--input postgres:// recomputes from stored all_hours digests.

Engine knobs live in YAML (user file + cwd overlay). Dollar rates live in
rate-card.json. ROBNE_NO_USER_CONFIG=1 skips home/XDG files only.

Contract: docs/plans/robne-cli-spec.md`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRecommendCmd())
	root.AddCommand(newValidateCmd())
	return root
}

type commonFlags struct {
	input        string
	configPath   string
	rateCardPath string
	plugins      string
	now          string
	format       string
	noUserConfig bool
	output       string
	pgURLFile    string
	applySchema  bool
}

func bindCommonFlags(cmd *cobra.Command, f *commonFlags, withRateCard, withFormat bool) {
	cmd.Flags().StringVar(&f.input, "input", "", "directory, .csv, .tar.gz, or postgres:// URL (recompute from stored digests)")
	_ = cmd.MarkFlagRequired("input")
	cmd.Flags().StringVar(&f.configPath, "config", "", "YAML overlay (skips ./robne.yaml; still overlays the user file)")
	cmd.Flags().StringVar(&f.plugins, "plugins", "", "comma-separated allowlist (Phase 1: container)")
	cmd.Flags().StringVar(&f.now, "now", "", "RFC3339 decay/staleness clock (does not slide term windows; default is max interval_end)")
	cmd.Flags().BoolVar(&f.noUserConfig, "no-user-config", false, "skip user YAML/JSON (same as ROBNE_NO_USER_CONFIG=1)")
	if withRateCard {
		cmd.Flags().StringVar(&f.rateCardPath, "rate-card", "", "rate-card JSON overlay (skips ./rate-card.json)")
	}
	if withFormat {
		cmd.Flags().StringVar(&f.format, "format", "json", "stdout format: json, csv, or table")
	}
}
