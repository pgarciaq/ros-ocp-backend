package namespace

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestNamespacePlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*NamespacePlugin)(nil)
		_ plugin.CSVIngestor       = (*NamespacePlugin)(nil)
		_ plugin.APIProvider       = (*NamespacePlugin)(nil)
		_ plugin.RetentionProvider = (*NamespacePlugin)(nil)
	)
}

func TestNamespacePlugin_nameEnabledAndTypes(t *testing.T) {
	t.Parallel()

	p := &NamespacePlugin{}
	assert.Equal(t, "namespace", p.Name())
	assert.True(t, p.Enabled())
	assert.Equal(t, []string{"namespace"}, p.SupportedCSVTypes())
	assert.Equal(t, []string{"daily_namespace_digests", "namespace_usage_samples"}, p.RetentionTables())
}
