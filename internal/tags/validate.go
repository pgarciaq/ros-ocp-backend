package tags

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxTagKeyLen              = 256
	maxTagValueLen            = 1024
	maxTagKeysPerSync         = 500
	maxNamespaceTagsPerSync   = 50_000
	maxTagValuesPerKey        = 500
)

// ValidateSyncRequest checks the inbound tag sync payload before database work.
func ValidateSyncRequest(req SyncRequest) error {
	orgID := strings.TrimSpace(req.OrgID)
	if orgID == "" {
		return fmt.Errorf("org_id is required")
	}
	if len(orgID) > 64 {
		return fmt.Errorf("org_id exceeds maximum length of 64 characters")
	}

	if _, err := parseSyncedAt(req.SyncedAt); err != nil {
		return err
	}

	if len(req.TagKeys) > maxTagKeysPerSync {
		return fmt.Errorf("tag_keys exceeds maximum of %d entries", maxTagKeysPerSync)
	}
	for i, entry := range req.TagKeys {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		if err := validateTagKey(key); err != nil {
			return fmt.Errorf("tag_keys[%d]: %w", i, err)
		}
		if len(entry.Values) > maxTagValuesPerKey {
			return fmt.Errorf("tag_keys[%d]: values exceeds maximum of %d entries", i, maxTagValuesPerKey)
		}
		for j, value := range entry.Values {
			if err := validateTagValue(value); err != nil {
				return fmt.Errorf("tag_keys[%d].values[%d]: %w", i, j, err)
			}
		}
	}

	if len(req.NamespaceTags) > maxNamespaceTagsPerSync {
		return fmt.Errorf("namespace_tags exceeds maximum of %d entries", maxNamespaceTagsPerSync)
	}
	for i, nt := range req.NamespaceTags {
		if strings.TrimSpace(nt.Namespace) == "" && strings.TrimSpace(nt.ClusterUUID) == "" {
			continue
		}
		if strings.TrimSpace(nt.ClusterUUID) == "" {
			return fmt.Errorf("namespace_tags[%d]: cluster_uuid is required when namespace is set", i)
		}
		if strings.TrimSpace(nt.Namespace) == "" {
			return fmt.Errorf("namespace_tags[%d]: namespace is required when cluster_uuid is set", i)
		}
		if len(nt.ClusterUUID) > 64 {
			return fmt.Errorf("namespace_tags[%d]: cluster_uuid exceeds maximum length", i)
		}
		if len(nt.Namespace) > 253 {
			return fmt.Errorf("namespace_tags[%d]: namespace exceeds maximum length", i)
		}
		if len(nt.Tags) > 200 {
			return fmt.Errorf("namespace_tags[%d]: tags exceeds maximum of 200 keys", i)
		}
		for key, value := range nt.Tags {
			if err := validateTagKey(key); err != nil {
				return fmt.Errorf("namespace_tags[%d].tags[%q]: %w", i, key, err)
			}
			if err := validateTagValue(value); err != nil {
				return fmt.Errorf("namespace_tags[%d].tags[%q]: %w", i, key, err)
			}
		}
	}
	return nil
}

func validateTagKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("tag key is required")
	}
	if utf8.RuneCountInString(key) > maxTagKeyLen {
		return fmt.Errorf("tag key exceeds maximum length of %d characters", maxTagKeyLen)
	}
	return nil
}

func validateTagValue(value string) error {
	if utf8.RuneCountInString(value) > maxTagValueLen {
		return fmt.Errorf("tag value exceeds maximum length of %d characters", maxTagValueLen)
	}
	return nil
}
