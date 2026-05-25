package tags_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

func TestValidateBearerToken_DevTokenFallback(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-only-token")

	err := tags.ValidateBearerToken(context.Background(), "dev-only-token")
	require.NoError(t, err)
}

func TestValidateBearerToken_RejectsMissingToken(t *testing.T) {
	config.ResetTagsForTest()

	err := tags.ValidateBearerToken(context.Background(), "")
	require.Error(t, err)
}

func TestBearerTokenFromHeader(t *testing.T) {
	assert.Equal(t, "abc", tags.BearerTokenFromHeader("Bearer abc"))
	assert.Equal(t, "", tags.BearerTokenFromHeader("Basic abc"))
	assert.Equal(t, "", tags.BearerTokenFromHeader(""))
}
