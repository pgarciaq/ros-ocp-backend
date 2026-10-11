package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnstableContainerCode86 pins the API surface for the container
// crash-loop quality gate: name, definition, container catalog membership,
// and Kruize mapping must all agree on code 86 (48 is VM_CRASH_LOOP).
func TestUnstableContainerCode86(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "UNSTABLE_CONTAINER", CodeNames[int16(86)])

	def, ok := Definitions[int16(86)]
	require.True(t, ok, "Definitions missing code 86")
	assert.Equal(t, "WARNING", def.Severity)
	assert.Contains(t, def.Message, "crash loop")

	resp := BuildCatalog("container")
	codes := make([]int16, 0, len(resp.Data))
	for _, e := range resp.Data {
		codes = append(codes, e.Code)
	}
	assert.Contains(t, codes, int16(86), "container catalog missing code 86")

	mapped := MapToKruizeFormat([]int16{86})
	entry, ok := mapped["86"]
	require.True(t, ok, "Kruize mapping missing code 86")
	assert.Equal(t, "WARNING", entry.Type)
	assert.Equal(t, int16(86), entry.Code)
	assert.NotEmpty(t, entry.Message)
}
