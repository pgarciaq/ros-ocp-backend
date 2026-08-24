package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// version is the binary identity. make robne injects git describe; go test / go build stay "devel".
var version = "devel"

type envelopeBump struct {
	name string
	v    int
}

func envelopeCapability() []envelopeBump {
	return []envelopeBump{
		{"container", recommendJSONVersion},
		{"namespace", recommendJSONVersionWithNamespace},
		{"node", recommendJSONVersionWithNode},
		{"gpu", recommendJSONVersionWithGPU},
		{"pvc", recommendJSONVersionWithPVC},
		{"vm", recommendJSONVersionWithVM},
		{"quota", recommendJSONVersionWithQuota},
		{"cluster_quota", recommendJSONVersionWithClusterQuota},
		{"snapshot", recommendJSONVersionWithSnapshot},
		{"business_hours", recommendJSONVersionWithBusinessHours},
		{"business_hours_plugins", recommendJSONVersionWithBusinessHoursPlugins},
	}
}

func jsonEnvelopeMax() int {
	max := 0
	for _, row := range envelopeCapability() {
		if row.v > max {
			max = row.v
		}
	}
	return max
}

func binaryVersion() string {
	if strings.TrimSpace(version) == "" {
		return "devel"
	}
	return version
}

func writeVersion(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "robne %s\njson_envelope_max %d\n\n", binaryVersion(), jsonEnvelopeMax()); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "plugin\tenvelope"); err != nil {
		return err
	}
	for _, row := range envelopeCapability() {
		if _, err := fmt.Fprintf(tw, "%s\t%d\n", row.name, row.v); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print binary identity and JSON envelope capability",
		Long: `Print which robne binary this is, and which JSON envelope versions it can emit.

JSON recommend "version" is per-run (max plugin this invocation, 10 when
business hours is on with container/namespace siblings only, or 11 when
node/GPU/VM business-hours siblings are present). It does not identify the
install. This command does. business_hours in the table is the YAML bump;
business_hours_plugins is the node/GPU/VM sibling bump. Neither is a
--plugins name. There is no --version flag.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeVersion(cmd.OutOrStdout())
		},
	}
}
