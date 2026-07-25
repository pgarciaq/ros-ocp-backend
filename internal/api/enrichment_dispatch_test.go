package api

import (
	"context"
	"os"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test-only plugin (registered from this test package). Must stay off unless
// ROS_API_TEST_ENRICHER=1 so other handler tests do not see synthetic GPU rows.
const rosAPIEnricherTestPluginName = "ros_api_test_enricher"

type rosAPIEnricherTestPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&rosAPIEnricherTestPlugin{})
}

func (p *rosAPIEnricherTestPlugin) Name() string {
	return rosAPIEnricherTestPluginName
}

func (p *rosAPIEnricherTestPlugin) Enabled() bool {
	return os.Getenv("ROS_API_TEST_ENRICHER") == "1"
}

func (p *rosAPIEnricherTestPlugin) EnrichResponse(ctx context.Context, resp interface{}) error {
	in, ok := resp.(*NativeContainerEnrichmentInput)
	if !ok || in == nil {
		return nil
	}
	for i := range in.Results {
		if in.Results[i].GPU == nil {
			in.Results[i].GPU = make(map[string]*model.GPURecommendation)
		}
		in.Results[i].GPU["api_test_term"] = &model.GPURecommendation{
			CurrentGPUModel: "ros-api-enricher-dispatch-test",
		}
	}
	return nil
}

func TestEnrichNativeContainerResults_InvokesAPIEnricherPlugins(t *testing.T) {
	t.Setenv("ROS_API_TEST_ENRICHER", "1")

	results := []model.NativeContainerResult{
		{ClusterUUID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", Project: "ns", Workload: "wl", Container: "c1"},
	}
	EnrichNativeContainerResults(context.Background(), &NativeContainerEnrichmentInput{
		OrgID:   "1234567",
		Results: results,
	})

	require.Contains(t, results[0].GPU, "api_test_term")
	require.NotNil(t, results[0].GPU["api_test_term"])
	require.Equal(t, "ros-api-enricher-dispatch-test", results[0].GPU["api_test_term"].CurrentGPUModel)
}

func TestEnrichContainerCurrency_PatchesMoneyAmountUnits(t *testing.T) {
	savings := money.FormatCentsToAmount(1000, money.DefaultCurrency)
	waste := money.FormatCentsToAmount(500, money.DefaultCurrency)
	cpuSav := money.FormatCentsToAmount(300, money.DefaultCurrency)
	memSav := money.FormatCentsToAmount(200, money.DefaultCurrency)

	results := []model.NativeContainerResult{
		{
			ClusterUUID:           "aaaaaaaa-0000-0000-0000-000000000001",
			EstimatedMonthlySavings: &savings,
			EstimatedMonthlyWaste:   &waste,
			CPUSavings:              &cpuSav,
			MemorySavings:           &memSav,
		},
	}

	enrichContainerCurrency(context.Background(), "org-no-cost-data", results)

	assert.Equal(t, money.DefaultCurrency, results[0].Currency)
	assert.Equal(t, money.DefaultCurrency, results[0].EstimatedMonthlySavings.Units)
	assert.Equal(t, money.DefaultCurrency, results[0].EstimatedMonthlyWaste.Units)
	assert.Equal(t, money.DefaultCurrency, results[0].CPUSavings.Units)
	assert.Equal(t, money.DefaultCurrency, results[0].MemorySavings.Units)
}

func TestEnrichContainerCurrency_NilMoneyAmountsDoNotPanic(t *testing.T) {
	results := []model.NativeContainerResult{
		{ClusterUUID: "aaaaaaaa-0000-0000-0000-000000000001"},
	}
	enrichContainerCurrency(context.Background(), "org-test", results)
	assert.Equal(t, money.DefaultCurrency, results[0].Currency)
}

func TestEnrichContainerCurrency_EmptySlice(t *testing.T) {
	enrichContainerCurrency(context.Background(), "org-test", nil)
}

func TestEnrichNamespaceCurrency_PatchesMoneyAmountUnits(t *testing.T) {
	waste := money.FormatCentsToAmount(800, money.DefaultCurrency)
	savings := money.FormatCentsToAmount(1200, money.DefaultCurrency)

	results := []model.NativeNamespaceResult{
		{
			ClusterUUID:           "aaaaaaaa-0000-0000-0000-000000000001",
			EstimatedMonthlyWaste: &waste,
			Recommendations: map[string]any{
				"estimated_monthly_savings": &savings,
			},
		},
	}

	enrichNamespaceCurrency(context.Background(), "org-test", results)

	assert.Equal(t, money.DefaultCurrency, results[0].EstimatedMonthlyWaste.Units)
	ma := results[0].Recommendations["estimated_monthly_savings"].(*money.MoneyAmount)
	assert.Equal(t, money.DefaultCurrency, ma.Units)
}

func TestEnrichNamespaceCurrency_EmptySlice(t *testing.T) {
	enrichNamespaceCurrency(context.Background(), "org-test", nil)
}
