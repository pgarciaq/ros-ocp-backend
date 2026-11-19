package scripts_test

// #897: the #891 capacity guard had no test. It is the only thing between a
// unit slip in the request bands and a fixture whose every node reads >100%
// requested — a slip that shipped twice, first in the regular band and then in
// the idle/GPU bands. Nothing in `make test-short` or CI would have noticed if
// an edit made the guard unreachable, wrong, or skipped.
//
// The guard is exercised through the generator's own CLI, against mutated
// copies of the script, so each case fails for the reason it names: a node axis
// that no longer fits, or the cores-vs-millicores slip of #891 itself. The
// constants are located by their exact source text — if one is renamed the test
// fails with "update the test", not by silently generating an unmutated config
// that passes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const generatorName = "gen_benchmark_config.py"

// generatorSource returns the committed generator's text. Cases mutate a copy
// of it rather than the working tree, so a run can never rewrite the script
// under test.
func generatorSource(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	src, err := os.ReadFile(filepath.Join(root, "scripts", generatorName)) //nolint:gosec // G304: generator path is fixed, not operator input
	require.NoError(t, err)
	return string(src)
}

func writeGeneratorCopy(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), generatorName)
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	return path
}

// runGenerator runs the generator at a fixed seed and returns its combined
// output, exit code, and the path it was asked to write.
func runGenerator(t *testing.T, script string) (string, int, string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatalf("python3 is required to test scripts/%s: %v", generatorName, err)
	}

	outPath := filepath.Join(t.TempDir(), "bench_config.yml")
	cmd := exec.Command("python3", script, //nolint:gosec // G204: test harness runs the repo's generator on a mutated copy
		"--containers", "1200", // the demo fixture's size: 9 nodes at ~125 containers each
		"--seed", "42", // deterministic bands, so the numbers in each assertion are stable
		"--output", outPath)
	out, err := cmd.CombinedOutput()

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "generator did not exit cleanly: %s", out)
		code = exitErr.ExitCode()
	}
	return string(out), code, outPath
}

// TestGenBenchmarkConfig_CommittedBandsFitTheirNodes is the green control. If
// this fails, the guard is right and the committed bands are wrong — which is
// the bug class #891 fixed, not a broken test.
func TestGenBenchmarkConfig_CommittedBandsFitTheirNodes(t *testing.T) {
	script := writeGeneratorCopy(t, generatorSource(t))

	out, code, outPath := runGenerator(t, script)
	require.Equal(t, 0, code, "the committed bands must not trip the capacity guard: %s", out)

	body, err := os.ReadFile(outPath) //nolint:gosec // G304: path returned by t.TempDir(), not operator input
	require.NoError(t, err)
	require.Contains(t, string(body), "bench-node-008",
		"1200 containers span 9 nodes (8 per 1000, floor 3): the fixture the #891 numbers were measured on")
	require.NotContains(t, string(body), "oversubscribes",
		"a passing run must not print the oversubscription banner")
}

// TestGenBenchmarkConfig_CapacityGuardTripsPerAxis covers each budget axis
// separately. Mutating the limit rather than the bands keeps the arithmetic
// visible: the generator's own numbers (126-150 pods, 32-46 cores, 134-416 GiB
// per node at 1200 containers) are what the limit is compared against.
func TestGenBenchmarkConfig_CapacityGuardTripsPerAxis(t *testing.T) {
	cases := []struct {
		name    string
		from    string
		to      string
		want    string
		notWant []string
	}{
		{
			name:    "pods",
			from:    "NODE_POD_CAPACITY = 250",
			to:      "NODE_POD_CAPACITY = 100",
			want:    "pods (node_capacity_pods 100)",
			notWant: []string{"cores requested", "GiB requested"},
		},
		{
			name:    "cpu",
			from:    "NODE_CPU_CORES = 64",
			to:      "NODE_CPU_CORES = 8",
			want:    "cores requested (8 present)",
			notWant: []string{"pods (node_capacity_pods", "GiB requested"},
		},
		{
			name:    "memory",
			from:    "NODE_MEMORY_GIG = 512",
			to:      "NODE_MEMORY_GIG = 100",
			want:    "GiB requested (100 present)",
			notWant: []string{"pods (node_capacity_pods", "cores requested"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := generatorSource(t)
			require.Contains(t, src, tc.from,
				"the test mutates this limit by its exact source text; update the test when the generator renames it")

			script := writeGeneratorCopy(t, strings.Replace(src, tc.from, tc.to, 1))
			out, code, outPath := runGenerator(t, script)

			require.Equal(t, 1, code, "an over-committed %s axis must exit 1: %s", tc.name, out)
			require.Contains(t, out, "oversubscribes", "the banner must say why the run stopped")
			require.Contains(t, out, tc.want, "stderr must name the %s axis and the limit it exceeded", tc.name)
			require.Contains(t, out, "bench-node-0", "stderr must name the offending node")
			for _, other := range tc.notWant {
				require.NotContains(t, out, other,
					"only the %s axis was over budget; blaming %q would mean the guard cannot tell the axes apart", tc.name, other)
			}
			require.NoFileExists(t, outPath, "the generator must fail before emitting YAML")
		})
	}
}

// TestGenBenchmarkConfig_PerPodGuardCatchesTheMillicoreSlip reverts the unit fix
// of #891 in the regular band — the exact historical change — and asserts the
// per-pod guard catches it before node arithmetic can hide it. A pod asking for
// a whole node's cores is not a density problem, it is a unit problem.
func TestGenBenchmarkConfig_PerPodGuardCatchesTheMillicoreSlip(t *testing.T) {
	const drawMillicores = "cpu_req = round(cpu_req_mc / 1000, 3)"
	src := generatorSource(t)
	require.Contains(t, src, drawMillicores,
		"the test reverts this line to reproduce #891; update the test when the generator moves it")

	script := writeGeneratorCopy(t, strings.Replace(src,
		drawMillicores,
		"cpu_req = round(cpu_req_mc, 3)  # 50-500 mC written straight into a cores field",
		1))
	out, code, outPath := runGenerator(t, script)

	require.Equal(t, 1, code, "the per-pod guard must exit 1 on a cores/millicores slip: %s", out)
	require.Contains(t, out, "A per-pod request above a whole node", "stderr must name the pod-level cause")
	require.Contains(t, out, "the node has 64", "stderr must state what the node actually has")
	require.Contains(t, out, "cores vs millicores", "stderr must name the unit mismatch, not the density")
	require.NoFileExists(t, outPath, "the generator must fail before emitting YAML")
}
