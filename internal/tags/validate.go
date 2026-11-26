package tags

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxTagKeyLen            = 256
	maxTagValueLen          = 1024
	maxTagKeysPerSync       = 500
	maxNamespaceTagsPerSync = 50_000
	// #941: koku sends the org-wide union of distinct values per enabled
	// key; the old 500-per-key cap 400'd the whole all-or-nothing org sync
	// when one enabled high-cardinality key crossed it, leaving stale tags
	// indefinitely. 10,000 gives that key 20x headroom.
	maxTagValuesPerKey = 10_000
	// maxTotalTagValuesPerSync is the total-values budget across the whole
	// tag_keys catalog (#941): the worst-case payload stays exactly today's
	// — koku bounds enabled keys at 200 (ENABLED_TAG_LIMIT), so the real
	// ceiling was 200 x 500 = 100,000. The budget is the bound that does
	// not depend on the producer's key count: an operator raising koku's
	// ENABLED_TAG_LIMIT still meets the same total.
	maxTotalTagValuesPerSync = 100_000
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
	totalTagValues := 0
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
		totalTagValues += len(entry.Values)
		for j, value := range entry.Values {
			if err := validateTagValue(value); err != nil {
				return fmt.Errorf("tag_keys[%d].values[%d]: %w", i, j, err)
			}
		}
	}
	if totalTagValues > maxTotalTagValuesPerSync {
		return fmt.Errorf("tag_keys: total values across keys exceeds maximum of %d entries", maxTotalTagValuesPerSync)
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
		for key, values := range nt.Tags {
			if err := validateTagKey(key); err != nil {
				return fmt.Errorf("namespace_tags[%d].tags[%q]: %w", i, key, err)
			}
			for _, value := range values {
				if err := validateTagValue(value); err != nil {
					return fmt.Errorf("namespace_tags[%d].tags[%q]: %w", i, key, err)
				}
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
