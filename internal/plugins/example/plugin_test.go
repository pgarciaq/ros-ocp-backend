package example

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestExamplePlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*ExamplePlugin)(nil)
		_ plugin.CSVIngestor       = (*ExamplePlugin)(nil)
		_ plugin.IngestHook        = (*ExamplePlugin)(nil)
		_ plugin.APIProvider       = (*ExamplePlugin)(nil)
		_ plugin.APIEnricher       = (*ExamplePlugin)(nil)
		_ plugin.RetentionProvider = (*ExamplePlugin)(nil)
		_ plugin.MigrationProvider = (*ExamplePlugin)(nil)
	)
}

func TestExamplePlugin_nameAndEnabled(t *testing.T) {
	t.Parallel()

	p := &ExamplePlugin{}
	assert.Equal(t, "_example", p.Name())
	assert.False(t, p.Enabled())
}
