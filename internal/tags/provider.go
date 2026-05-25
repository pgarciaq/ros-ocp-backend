package tags

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// TagProvider supplies enabled tag keys, values, and container-key filtering for list APIs.
type TagProvider interface {
	// GetEnabledTagKeys returns enabled OCP tag keys for the org.
	GetEnabledTagKeys(ctx context.Context, orgID string) ([]string, error)
	// GetTagValues returns current values for a specific tag key in the org.
	GetTagValues(ctx context.Context, orgID string, key string) ([]string, error)
	// FilterByTag returns org_container_keys container_name scope rows matching the tag filter.
	// Each entry is namespace/workload/container_name (unique per org).
	FilterByTag(ctx context.Context, orgID string, key string, values []string) ([]string, error)
	// TagCatalog returns enabled keys with observed values (for status/metadata endpoints).
	TagCatalog(ctx context.Context, orgID string) ([]TagKeyCatalog, error)
}

var (
	providerOnce sync.Once
	providerInst TagProvider
)

// GetProvider returns the configured TagProvider singleton.
func GetProvider() TagProvider {
	providerOnce.Do(func() {
		providerInst = NewProviderFromConfig(database.GetPool())
	})
	return providerInst
}

// ResetProviderForTest clears the singleton so the next GetProvider() rebuilds from env.
func ResetProviderForTest() {
	providerInst = nil
	providerOnce = sync.Once{}
}

// NewProviderFromConfig selects DB or API implementation from ROS_TAGS_SOURCE.
func NewProviderFromConfig(pool *pgxpool.Pool) TagProvider {
	if config.TagsSource() == "api" {
		return NewAPITagProvider(pool)
	}
	return NewDBTagProvider(pool)
}

// TenantSchema returns the Koku tenant schema name for a bare org_id.
func TenantSchema(orgID string) (string, error) {
	orgID = trimOrgID(orgID)
	if orgID == "" {
		return "", fmt.Errorf("org_id is required")
	}
	for _, r := range orgID {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("invalid org_id %q", orgID)
		}
	}
	return "org" + orgID, nil
}

func trimOrgID(orgID string) string {
	orgID = strings.TrimSpace(orgID)
	if len(orgID) > 3 && orgID[:3] == "org" {
		return orgID[3:]
	}
	return orgID
}
