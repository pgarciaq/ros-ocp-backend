package tags

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const defaultSACACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

const (
	defaultSATokenPath    = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultTokenReviewURL = "https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews"
)

var log = logging.GetLogger()

type tokenReviewRequest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Token string `json:"token"`
	} `json:"spec"`
}

type tokenReviewResponse struct {
	Status struct {
		Authenticated bool `json:"authenticated"`
		User          struct {
			Username string `json:"username"`
		} `json:"user"`
		Error string `json:"error"`
	} `json:"status"`
}

// ValidateBearerToken authorizes internal tag sync requests.
// When ROS_TAGS_DEV_TOKEN is set, matching tokens are accepted (dev-only).
// Otherwise the token is validated via the Kubernetes TokenReview API.
func ValidateBearerToken(ctx context.Context, bearerToken string) error {
	_, err := AuthenticateInternalCaller(ctx, bearerToken)
	return err
}

// AuthenticateInternalCaller validates the bearer token and returns the caller
// service account name (short form) for audit logging.
func AuthenticateInternalCaller(ctx context.Context, bearerToken string) (string, error) {
	bearerToken = strings.TrimSpace(bearerToken)
	if bearerToken == "" {
		return "", fmt.Errorf("missing bearer token")
	}

	cfg := config.GetConfig()
	if devToken := strings.TrimSpace(cfg.TagsDevToken); devToken != "" && bearerToken == devToken {
		log.Warn("tag sync auth: accepting ROS_TAGS_DEV_TOKEN (dev-only fallback)")
		return "dev-token", nil
	}

	username, err := validateSATokenViaTokenReview(ctx, bearerToken, cfg.TagsAllowedServiceAccounts)
	if err != nil {
		return "", err
	}
	return serviceAccountName(username), nil
}

func validateSATokenViaTokenReview(ctx context.Context, token string, allowedAccounts string) (string, error) {
	cfg := config.GetConfig()
	tokenPath := strings.TrimSpace(cfg.KubernetesSATokenPath)
	if tokenPath == "" {
		tokenPath = defaultSATokenPath
	}
	reviewerToken, err := readFileTrim(tokenPath)
	if err != nil || reviewerToken == "" {
		reviewerToken, err = readFileTrim(defaultSATokenPath)
	}
	if err != nil || reviewerToken == "" {
		return "", fmt.Errorf("service account reviewer token unavailable: %w", err)
	}

	reviewURL := strings.TrimSpace(cfg.KubernetesTokenReviewURL)
	if reviewURL == "" {
		reviewURL = defaultTokenReviewURL
	}

	body, err := json.Marshal(tokenReviewRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
		Spec: struct {
			Token string `json:"token"`
		}{Token: token},
	})
	if err != nil {
		return "", fmt.Errorf("marshal token review request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reviewURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create token review request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+reviewerToken)
	req.Header.Set("Content-Type", "application/json")

	client := kubeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token review request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token review response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("token review returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var review tokenReviewResponse
	if err := json.Unmarshal(respBody, &review); err != nil {
		return "", fmt.Errorf("decode token review response: %w", err)
	}
	if !review.Status.Authenticated {
		if review.Status.Error != "" {
			return "", fmt.Errorf("token not authenticated: %s", review.Status.Error)
		}
		return "", fmt.Errorf("token not authenticated")
	}

	username := review.Status.User.Username
	if !strings.HasPrefix(username, "system:serviceaccount:") {
		return "", fmt.Errorf("token user is not a service account: %q", username)
	}

	allowed := parseAllowedServiceAccounts(allowedAccounts)
	if len(allowed) == 0 {
		if config.IsDevelopment() {
			return username, nil
		}
		return "", fmt.Errorf("service account allowlist is not configured (ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS)")
	}

	saName := serviceAccountName(username)
	for _, candidate := range allowed {
		if candidate == saName || candidate == username {
			return username, nil
		}
	}
	return "", fmt.Errorf("service account %q is not allowed", saName)
}

func parseAllowedServiceAccounts(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func serviceAccountName(username string) string {
	// system:serviceaccount:<namespace>:<name>
	parts := strings.Split(username, ":")
	if len(parts) == 0 {
		return username
	}
	return parts[len(parts)-1]
}

func kubeHTTPClient() *http.Client {
	caCert, err := os.ReadFile(defaultSACACertPath)
	if err != nil {
		return &http.Client{Timeout: 10 * time.Second}
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caCert)
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}
}

func readFileTrim(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// BearerTokenFromHeader extracts the token from an Authorization: Bearer header.
func BearerTokenFromHeader(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
