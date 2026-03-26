package types

import (
	"encoding/json"
	"testing"
)

func TestKafkaMsg_UnmarshalsCustomTimeframes(t *testing.T) {
	raw := `{
        "request_id": "req-1",
        "b64_identity": "aWRlbnRpdHk=",
        "metadata": {
            "org_id": "1234567",
            "source_id": "1",
            "cluster_uuid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
            "cluster_alias": "test",
            "custom_timeframes": {
                "terms": [
                    {"name": "term1", "duration_days": 3},
                    {"name": "term2", "duration_days": 20}
                ],
                "business_hours": {"enabled": false}
            }
        },
        "files": ["https://example.com/file1"]
    }`
	var msg KafkaMsg
	err := json.Unmarshal([]byte(raw), &msg)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if msg.Metadata.CustomTimeframes == nil {
		t.Fatal("expected custom_timeframes to be parsed")
	}
	if len(msg.Metadata.CustomTimeframes.Terms) != 2 {
		t.Errorf("expected 2 terms, got %d", len(msg.Metadata.CustomTimeframes.Terms))
	}
	if msg.Metadata.CustomTimeframes.Terms[0].DurationDays != 3 {
		t.Errorf("expected term1 duration 3, got %d", msg.Metadata.CustomTimeframes.Terms[0].DurationDays)
	}
}

func TestKafkaMsg_BackwardsCompatible_NoCustomTimeframes(t *testing.T) {
	raw := `{
        "request_id": "req-1",
        "b64_identity": "aWRlbnRpdHk=",
        "metadata": {
            "org_id": "1234567",
            "source_id": "1",
            "cluster_uuid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
            "cluster_alias": "test"
        },
        "files": ["https://example.com/file1"]
    }`
	var msg KafkaMsg
	err := json.Unmarshal([]byte(raw), &msg)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if msg.Metadata.CustomTimeframes != nil {
		t.Error("expected nil custom_timeframes for old-format message")
	}
	if msg.Metadata.Org_id != "1234567" {
		t.Errorf("expected org_id 1234567, got %s", msg.Metadata.Org_id)
	}
}
