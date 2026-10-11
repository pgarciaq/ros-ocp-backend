package tags_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func craftJWT(t *testing.T, aud []string) string {
	t.Helper()
	payload := `{}`
	if aud != nil {
		b, err := json.Marshal(aud)
		require.NoError(t, err)
		payload = `{"aud":` + string(b) + `}`
	}
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return "h." + enc + ".s"
}

func TestValidateBearerToken_ForwardsTokenAudiences(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS", "koku")

	var gotAudiences []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Spec struct {
				Token     string   `json:"token"`
				Audiences []string `json:"audiences"`
			} `json:"spec"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		gotAudiences = req.Spec.Audiences
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":{"authenticated":true,"user":{"username":"system:serviceaccount:cost-onprem:koku"}}}`)
	}))
	defer srv.Close()
	t.Setenv("KUBERNETES_TOKEN_REVIEW_URL", srv.URL)
	reviewerFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(reviewerFile, []byte("reviewer"), 0600))
	t.Setenv("KUBERNETES_SA_TOKEN_PATH", reviewerFile)

	// Audience-bound subject token (like Koku's projected ros-api token):
	// the review request must echo its audiences or the API server rejects
	// with "token audiences is invalid for the target audiences".
	err := tags.ValidateBearerToken(context.Background(), craftJWT(t, []string{"ros-api"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"ros-api"}, gotAudiences)

	// Audience-less token: no audiences key, legacy path preserved.
	err = tags.ValidateBearerToken(context.Background(), craftJWT(t, nil))
	require.NoError(t, err)
	assert.Empty(t, gotAudiences)
}
