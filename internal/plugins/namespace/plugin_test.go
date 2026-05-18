package namespace

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestNamespacePlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin      = (*NamespacePlugin)(nil)
		_ plugin.CSVIngestor = (*NamespacePlugin)(nil)
	)
}

func TestNamespacePlugin_nameEnabledAndTypes(t *testing.T) {
	t.Parallel()

	p := &NamespacePlugin{}
	assert.Equal(t, "namespace", p.Name())
	assert.True(t, p.Enabled())
	assert.Equal(t, []string{"namespace"}, p.SupportedCSVTypes())
}
