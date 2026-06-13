package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestContainerPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin            = (*ContainerPlugin)(nil)
		_ plugin.CSVIngestor       = (*ContainerPlugin)(nil)
		_ plugin.RetentionProvider = (*ContainerPlugin)(nil)
	)
}

func TestContainerPlugin_nameEnabledAndTypes(t *testing.T) {
	t.Parallel()

	p := &ContainerPlugin{}
	assert.Equal(t, "container", p.Name())
	assert.True(t, p.Enabled())
	assert.Equal(t, []string{"container"}, p.SupportedCSVTypes())
	assert.Equal(t, []string{"daily_container_digests"}, p.RetentionTables())
}
