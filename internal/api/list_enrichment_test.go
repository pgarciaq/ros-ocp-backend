package api

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/stretchr/testify/assert"
)

func TestShouldSkipListEnrichment_CountOnlyRequests(t *testing.T) {
	assert.True(t, shouldSkipListEnrichment(listoptions.ListOptions{Limit: 1}))
	assert.True(t, shouldSkipListEnrichment(listoptions.ListOptions{Limit: 0}))
}

func TestShouldSkipListEnrichment_TablePagesEnrich(t *testing.T) {
	assert.False(t, shouldSkipListEnrichment(listoptions.ListOptions{Limit: 10}))
	assert.False(t, shouldSkipListEnrichment(listoptions.ListOptions{Limit: 2}))
}

func TestShouldSkipListEnrichment_CSVAlwaysEnriches(t *testing.T) {
	assert.False(t, shouldSkipListEnrichment(listoptions.ListOptions{
		Limit:  1,
		Format: listoptions.ResponseFormatCSV,
	}))
}
