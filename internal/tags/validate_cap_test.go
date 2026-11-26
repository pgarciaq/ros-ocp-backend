package tags

import (
	"fmt"
	"strings"
	"testing"
)

// #941: the per-key value cap and the total-values budget. Koku sends the
// org-wide union of distinct values per enabled key; the old 500-per-key cap
// 400'd the whole all-or-nothing org sync when one enabled key crossed it,
// leaving stale tags indefinitely. The cap is now 10,000 per key with a
// 100,000 total-values budget — the worst-case payload is exactly today's
// (koku bounds enabled keys at 200, so 200 × 500 was the real ceiling), with
// 20× per-key headroom for one high-cardinality key.

func values(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("value-%05d", i)
	}
	return out
}

func TestValidateSyncRequest_TagValueCap(t *testing.T) {
	base := func(keyValues []string) SyncRequest {
		return SyncRequest{
			OrgID:    "1234567",
			SyncedAt: "2026-11-26T00:00:00Z",
			TagKeys:  []TagKeyCatalog{{Key: "app", Values: keyValues}},
		}
	}

	t.Run("a key with more than 500 values syncs (#941's close criterion)", func(t *testing.T) {
		// 501..10,000 values: accepted. The old cap rejected 501 with a 400.
		if err := ValidateSyncRequest(base(values(501))); err != nil {
			t.Errorf("501 values must sync after #941: %v", err)
		}
		if err := ValidateSyncRequest(base(values(10_000))); err != nil {
			t.Errorf("10,000 values (the new per-key cap) must sync: %v", err)
		}
	})

	t.Run("the per-key cap still rejects beyond 10,000 with a named message", func(t *testing.T) {
		err := ValidateSyncRequest(base(values(10_001)))
		if err == nil {
			t.Fatal("10,001 values must be rejected")
		}
		if !strings.Contains(err.Error(), "10000") {
			t.Errorf("the rejection must name the cap, got: %v", err)
		}
	})
}

func TestValidateSyncRequest_TotalValueBudget(t *testing.T) {
	// The budget is the bound that does not depend on the producer's key
	// count: 200 keys x 500 values (today's real worst case, koku bounds
	// enabled keys at 200) passes; 101,000 total values across any
	// distribution is rejected — the worst-case payload stays exactly
	// today's no matter how koku's ENABLED_TAG_LIMIT is configured.
	keys := make([]TagKeyCatalog, 200)
	for i := range keys {
		keys[i] = TagKeyCatalog{Key: fmt.Sprintf("key-%03d", i), Values: values(500)}
	}
	req := SyncRequest{
		OrgID:    "1234567",
		SyncedAt: "2026-11-26T00:00:00Z",
		TagKeys:  keys,
	}
	if err := ValidateSyncRequest(req); err != nil {
		t.Errorf("200 keys x 500 values (100,000 total — today's worst case) must sync: %v", err)
	}

	// Over the budget without any single key exceeding the per-key cap:
	// 11 keys x 9,100 values = 100,100 total.
	overKeys := make([]TagKeyCatalog, 11)
	for i := range overKeys {
		overKeys[i] = TagKeyCatalog{Key: fmt.Sprintf("key-%02d", i), Values: values(9_100)}
	}
	over := SyncRequest{
		OrgID:    "1234567",
		SyncedAt: "2026-11-26T00:00:00Z",
		TagKeys:  overKeys,
	}
	err := ValidateSyncRequest(over)
	if err == nil {
		t.Fatal("100,001 total values must be rejected by the budget")
	}
	if !strings.Contains(err.Error(), "100000") {
		t.Errorf("the rejection must name the total budget, got: %v", err)
	}
}
