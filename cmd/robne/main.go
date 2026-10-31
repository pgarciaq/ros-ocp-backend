// robne is a standalone CLI that computes OpenShift container recommendations
// from local ROS CSVs using librobne (Phase 1 stdout; Phase 2a optional Postgres).
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := newRootCmd()
	// Hidden docs generator (#703); excluded from the generated tree by
	// generateDocs, which builds a fresh public root.
	root.AddCommand(newManCmd())
	if err := root.Execute(); err != nil {
		var ce *cliExit
		if errors.As(err, &ce) {
			if ce.Err != nil && ce.Err.Error() != "" {
				fmt.Fprintln(os.Stderr, ce.Err)
			}
			os.Exit(ce.Code)
		}
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, err)
		}
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

Term windows resolve per plugin (#705): each term-bearing plugin uses its
documented table (container family 1/7/15, VM 7/15/30, PVC 7/30/90) unless
plugin_terms: overrides it; the top-level terms: list is container-family-
scoped.

Contract: docs/plans/robne-cli-spec.md`,
		Example: `  # Compute from a payload directory, defaults everywhere:
  robne recommend --input ./csvs/ --format json

  # A robne.yaml with per-plugin term overrides (copy robne.yaml.sample
  # for the full knob set; search: ~/.config/robne/robne.yaml + ./robne.yaml):
  plugin_terms:
    vm:
      - name: short_term
        window_days: 10
        min_data_days: 5

  # Pin the clock for CI goldens:
  robne recommend --input ./csvs/ --now 2026-08-01T02:00:00Z --format json`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRecommendCmd())
	root.AddCommand(newValidateCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newExplainCmd())
	root.AddCommand(newVersionCmd())
	return root
}

type commonFlags struct {
	inputs       []string
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
	cmd.Flags().StringArrayVar(&f.inputs, "input", nil, "directory, .csv, .tar.gz, or postgres:// URL (recompute from stored digests); repeatable — the first payload is the primary (recommendation cluster), later payloads are advisory context (e.g. the HCP management cluster's topology)")
	_ = cmd.MarkFlagRequired("input")
	cmd.Flags().StringVar(&f.configPath, "config", "", "YAML overlay (skips ./robne.yaml; still overlays the user file)")
	cmd.Flags().StringVar(&f.plugins, "plugins", "", "comma-separated allowlist (default: all shipped plugins; omit to skip missing inputs)")
	cmd.Flags().StringVar(&f.now, "now", "", "RFC3339 decay/staleness clock (does not slide term windows; default is max interval_end)")
	cmd.Flags().BoolVar(&f.noUserConfig, "no-user-config", false, "skip user YAML/JSON (same as ROBNE_NO_USER_CONFIG=1)")
	if withRateCard {
		cmd.Flags().StringVar(&f.rateCardPath, "rate-card", "", "rate-card JSON overlay (skips ./rate-card.json)")
	}
	if withFormat {
		cmd.Flags().StringVar(&f.format, "format", "json", "stdout format: json, csv, or table")
	}
}

// splitInputs divides the repeatable --input values into the primary (the
// recommendation cluster — first input, option (d), #721) and the advisory
// context payloads (later inputs, e.g. the HCP management cluster's
// topology). A postgres:// URL is valid only as the primary.
func splitInputs(inputs []string) (primary string, extra []string) {
	if len(inputs) == 0 {
		return "", nil
	}
	return inputs[0], inputs[1:]
}

// splitPrimary returns just the primary input (the first --input).
func splitPrimary(inputs []string) string {
	primary, _ := splitInputs(inputs)
	return primary
}
