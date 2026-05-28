package quota

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/stretchr/testify/assert"
)

func TestQuotaPlugin_Metadata(t *testing.T) {
	p := &QuotaPlugin{}
	assert.Equal(t, "quota", p.Name())
	assert.Equal(t, plugin.PhaseProduce, p.Phase())
	assert.Equal(t, 35, p.Priority())
	assert.Equal(t, []string{"container", "namespace"}, p.HookAfterCSVTypes())
	assert.Equal(t, []string{"quota_recommendation_sets"}, p.RetentionTables())
}
