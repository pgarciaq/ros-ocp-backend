# PRD99 COST-5691: Custom Timeframes — TDD Test Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Write failing tests first for every feature in the custom timeframes implementation, then make them pass with minimal code.

**Architecture:** Tests are organized by repository and component, following each repo's existing test conventions. Each test group covers one logical unit from the implementation spec (IMPL-PRD99-Custom-Timeframes.md).

**Tech Stack:**
- Koku backend: Python 3.11, Django TestCase/IamTestCase, unittest.mock, model_bakery
- ros-ocp-backend: Go 1.24, `testing` + `testify/assert` + `go-cmp/cmp` + `httptest`
- Kruize: Java 25, JUnit 5 (junit-jupiter-engine)
- koku-ui: TypeScript, Jest/React Testing Library (existing monorepo config)

**Test runner commands:**
- Koku: `pipenv run tox -- koku.api.settings.test.ros_custom_timeframes`
- ros-ocp-backend: `go test -v ./internal/...`
- Kruize: `mvn test -pl .` (from autotune root)
- koku-ui: `npm test --workspace apps/koku-ui-hccm`

---

## Part 1: Koku Backend Tests

### Task 1: Settings API — Serializer Validation

**Spec ref:** IMPL §6.1, §10 (Validation Rules)

**Files:**
- Create: `koku/koku/api/settings/test/ros_custom_timeframes/test_serializers.py`
- Create: `koku/koku/api/settings/ros_custom_timeframes.py` (serializer + view, implementation)

**Run:** `pipenv run tox -- api.settings.test.ros_custom_timeframes.test_serializers`

- [ ] **Step 1: Write test — valid 3-term configuration accepted**

```python
# koku/koku/api/settings/test/ros_custom_timeframes/test_serializers.py
from django.test import TestCase
from api.settings.ros_custom_timeframes import ROSCustomTimeframesSerializer


class TestROSCustomTimeframesSerializer(TestCase):
    """Tests for ROS custom timeframes validation (Koku is the single source of truth)."""

    def test_valid_three_terms(self):
        """A valid 3-term config with business hours disabled passes validation."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 3},
                {"name": "term2", "duration_days": 20},
                {"name": "term3", "duration_days": 60},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 2: Run test — verify it fails (ImportError: serializer doesn't exist yet)**

Run: `pipenv run tox -- api.settings.test.ros_custom_timeframes.test_serializers::TestROSCustomTimeframesSerializer::test_valid_three_terms`
Expected: FAIL with `ImportError` or `ModuleNotFoundError`

- [ ] **Step 3: Write test — valid single-term (term1 only)**

```python
    def test_valid_single_term(self):
        """Only term1 is required; term2 and term3 are optional."""
        data = {
            "terms": [{"name": "term1", "duration_days": 5}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 4: Write test — reject term2 without term1**

```python
    def test_reject_term2_without_term1(self):
        """Cannot define term2 without term1 (sequential rule)."""
        data = {
            "terms": [{"name": "term2", "duration_days": 10}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 5: Write test — reject term3 without term2**

```python
    def test_reject_term3_without_term2(self):
        """Cannot define term3 without term2."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 1},
                {"name": "term3", "duration_days": 30},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 6: Write test — reject terms not ordered shorter to longer**

```python
    def test_reject_terms_not_ascending(self):
        """Terms must be ordered shorter to longer."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 30},
                {"name": "term2", "duration_days": 10},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 7: Write test — reject duplicate durations**

```python
    def test_reject_duplicate_durations(self):
        """All term durations must be distinct."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 7},
                {"name": "term2", "duration_days": 7},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 8: Write test — reject duration below minimum (0)**

```python
    def test_reject_duration_below_minimum(self):
        """Duration must be at least 1 day."""
        data = {
            "terms": [{"name": "term1", "duration_days": 0}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 9: Write test — reject duration above maximum (91+)**

```python
    def test_reject_duration_above_maximum(self):
        """Duration must not exceed 90 days."""
        data = {
            "terms": [{"name": "term1", "duration_days": 91}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 10: Write test — reject non-integer duration**

```python
    def test_reject_non_integer_duration(self):
        """Duration must be a whole number of days."""
        data = {
            "terms": [{"name": "term1", "duration_days": 3.5}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 11: Write test — valid business hours config**

```python
    def test_valid_business_hours(self):
        """Business hours with timezone, weekdays, and time range passes."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "17:00",
                "weekdays": [1, 2, 3, 4, 5],
                "timezone": "America/New_York",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 12: Write test — reject invalid timezone**

```python
    def test_reject_invalid_timezone(self):
        """Timezone must be a valid IANA timezone string."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "17:00",
                "weekdays": [1, 2, 3, 4, 5],
                "timezone": "Mars/Olympus_Mons",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 13: Write test — reject empty weekdays**

```python
    def test_reject_empty_weekdays(self):
        """At least one weekday must be selected."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "17:00",
                "weekdays": [],
                "timezone": "UTC",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 14: Write test — reject invalid weekday number**

```python
    def test_reject_invalid_weekday_number(self):
        """Weekdays must be 1-7 (ISO 8601)."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "17:00",
                "weekdays": [0, 8],
                "timezone": "UTC",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 15: Write test — reject business window under 1 hour**

```python
    def test_reject_business_window_under_one_hour(self):
        """Business window must be at least 1 hour."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "09:30",
                "weekdays": [1],
                "timezone": "UTC",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 16: Write test — midnight-spanning business hours valid**

```python
    def test_midnight_spanning_business_hours(self):
        """Business hours can span midnight (e.g., night shift 22:00-06:00)."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "22:00",
                "end_time": "06:00",
                "weekdays": [1, 2, 3, 4, 5],
                "timezone": "UTC",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 17: Write test — business hours disabled ignores sub-fields**

```python
    def test_business_hours_disabled_ignores_subfields(self):
        """When business_hours.enabled=False, sub-fields are not validated."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 18: Implement the serializer to make all tests pass**

- [ ] **Step 19: Run full serializer test suite — verify all GREEN**

Run: `pipenv run tox -- api.settings.test.ros_custom_timeframes.test_serializers`
Expected: All PASS

- [ ] **Step 20: Commit**

```bash
git add koku/api/settings/test/ros_custom_timeframes/ koku/api/settings/ros_custom_timeframes.py
git commit -m "COST-5691: Add ROS custom timeframes serializer with TDD tests"
```

---

### Task 2: Settings API — Views (GET/PUT)

**Spec ref:** IMPL §6.1

**Files:**
- Create: `koku/koku/api/settings/test/ros_custom_timeframes/test_views.py`
- Modify: `koku/koku/api/settings/view.py`
- Modify: `koku/koku/api/urls.py`

**Run:** `pipenv run tox -- api.settings.test.ros_custom_timeframes.test_views`

- [ ] **Step 1: Write test — GET returns defaults when not configured**

```python
from django_tenants.utils import schema_context
from rest_framework import status
from rest_framework.test import APIClient

from api.iam.test.iam_test_case import IamTestCase


class TestROSCustomTimeframesView(IamTestCase):
    """Tests for the ROS custom timeframes settings endpoint."""

    def setUp(self):
        super().setUp()
        self.client = APIClient()
        self.url = "/api/cost-management/v1/account-settings/ros-custom-timeframes/"

    def test_get_returns_defaults(self):
        """GET returns default terms (1d, 7d, 15d) when no custom config exists."""
        response = self.client.get(self.url, **self.headers)
        self.assertEqual(response.status_code, status.HTTP_200_OK)
        data = response.data["data"][0]
        terms = data["terms"]
        self.assertEqual(len(terms), 3)
        self.assertEqual(terms[0]["duration_days"], 1)
        self.assertEqual(terms[1]["duration_days"], 7)
        self.assertEqual(terms[2]["duration_days"], 15)
        self.assertFalse(data["business_hours"]["enabled"])
```

- [ ] **Step 2: Write test — PUT saves custom terms and GET returns them**

```python
    def test_put_and_get_custom_terms(self):
        """PUT saves custom terms; subsequent GET returns them."""
        payload = {
            "terms": [
                {"name": "term1", "duration_days": 3},
                {"name": "term2", "duration_days": 20},
                {"name": "term3", "duration_days": 60},
            ],
            "business_hours": {"enabled": False},
        }
        put_response = self.client.put(self.url, data=payload, format="json", **self.headers)
        self.assertEqual(put_response.status_code, status.HTTP_204_NO_CONTENT)

        get_response = self.client.get(self.url, **self.headers)
        data = get_response.data["data"][0]
        self.assertEqual(data["terms"][0]["duration_days"], 3)
        self.assertEqual(data["terms"][1]["duration_days"], 20)
        self.assertEqual(data["terms"][2]["duration_days"], 60)
```

- [ ] **Step 3: Write test — PUT with invalid data returns 400**

```python
    def test_put_invalid_data_returns_400(self):
        """PUT with terms out of order returns 400."""
        payload = {
            "terms": [
                {"name": "term1", "duration_days": 30},
                {"name": "term2", "duration_days": 10},
            ],
            "business_hours": {"enabled": False},
        }
        response = self.client.put(self.url, data=payload, format="json", **self.headers)
        self.assertEqual(response.status_code, status.HTTP_400_BAD_REQUEST)
```

- [ ] **Step 4: Write test — PUT with business hours enabled**

```python
    def test_put_business_hours(self):
        """PUT with business hours saves timezone and weekdays."""
        payload = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "17:00",
                "weekdays": [1, 2, 3, 4, 5],
                "timezone": "America/New_York",
            },
        }
        put_response = self.client.put(self.url, data=payload, format="json", **self.headers)
        self.assertEqual(put_response.status_code, status.HTTP_204_NO_CONTENT)

        get_response = self.client.get(self.url, **self.headers)
        bh = get_response.data["data"][0]["business_hours"]
        self.assertTrue(bh["enabled"])
        self.assertEqual(bh["timezone"], "America/New_York")
        self.assertEqual(bh["weekdays"], [1, 2, 3, 4, 5])
```

- [ ] **Step 5: Write test — RBAC non-admin rejected**

```python
    def test_non_admin_rejected(self):
        """Non-admin users get 403 on PUT."""
        self.client.force_authenticate(user=None)
        payload = {
            "terms": [{"name": "term1", "duration_days": 5}],
            "business_hours": {"enabled": False},
        }
        # Use headers without admin access
        response = self.client.put(self.url, data=payload, format="json")
        self.assertIn(response.status_code, [status.HTTP_401_UNAUTHORIZED, status.HTTP_403_FORBIDDEN])
```

- [ ] **Step 6: Implement view, URL registration, and storage to pass all tests**

- [ ] **Step 7: Run test suite — verify all GREEN**

- [ ] **Step 8: Commit**

```bash
git commit -m "COST-5691: Add ROS custom timeframes settings view with TDD tests"
```

---

### Task 3: Kafka Message — Embed Settings and Partition Key

**Spec ref:** IMPL §6.2, §6.3

**Files:**
- Create: `koku/koku/masu/test/external/test_ros_report_shipper_custom_timeframes.py`
- Modify: `koku/koku/masu/external/ros_report_shipper.py`

**Run:** `pipenv run tox -- masu.test.external.test_ros_report_shipper_custom_timeframes`

- [ ] **Step 1: Write test — build_ros_msg includes custom_timeframes in metadata**

```python
import json
from unittest.mock import patch, MagicMock

from django.test import TestCase

from masu.external.ros_report_shipper import ROSReportShipper
from masu.test.util.ocp.test_common import ManifestFactory
from masu.util.ocp import common as utils


class TestROSReportShipperCustomTimeframes(TestCase):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.manifest = ManifestFactory.build(manifest_id=400, cluster_id="test-cluster")
        payload = utils.PayloadInfo(
            request_id="req-1",
            manifest=cls.manifest,
            source_id="1",
            provider_uuid="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
            provider_type="OCP",
            cluster_alias="test-alias",
            account_id="10001",
            org_id="1234567",
            schema_name="org1234567",
            trino_schema="org1234567",
        )
        with patch("masu.external.ros_report_shipper.get_ros_s3_client"):
            cls.shipper = ROSReportShipper(payload, "b64identity", {"account": "10001", "org_id": "1234567"})

    def test_build_ros_msg_includes_custom_timeframes(self):
        """build_ros_msg embeds custom_timeframes from tenant settings in metadata."""
        mock_settings = {
            "terms": [{"name": "term1", "duration_days": 3}],
            "business_hours": {"enabled": False},
        }
        with patch.object(self.shipper, "_get_ros_custom_timeframes", return_value=mock_settings):
            msg = self.shipper.build_ros_msg(["https://url1"], ["key1"])
        parsed = json.loads(msg)
        self.assertIn("custom_timeframes", parsed["metadata"])
        self.assertEqual(parsed["metadata"]["custom_timeframes"]["terms"][0]["duration_days"], 3)
```

- [ ] **Step 2: Write test — send_kafka_message uses org_id as partition key**

```python
    @patch("masu.external.ros_report_shipper.get_producer")
    def test_send_kafka_message_uses_org_id_key(self, mock_get_producer):
        """send_kafka_message passes org_id as the Kafka message key."""
        mock_producer = MagicMock()
        mock_get_producer.return_value = mock_producer

        self.shipper.send_kafka_message(b'{"test": true}')

        mock_producer.produce.assert_called_once()
        call_kwargs = mock_producer.produce.call_args
        self.assertEqual(call_kwargs.kwargs.get("key") or call_kwargs[1].get("key"), b"1234567")
```

- [ ] **Step 3: Write test — build_ros_msg returns null custom_timeframes when no settings**

```python
    def test_build_ros_msg_null_custom_timeframes_when_no_settings(self):
        """When no custom settings configured, custom_timeframes is None/default."""
        with patch.object(self.shipper, "_get_ros_custom_timeframes", return_value=None):
            msg = self.shipper.build_ros_msg(["https://url1"], ["key1"])
        parsed = json.loads(msg)
        self.assertIsNone(parsed["metadata"].get("custom_timeframes"))
```

- [ ] **Step 4: Implement _get_ros_custom_timeframes() and partition key to pass all tests**

- [ ] **Step 5: Run tests — verify GREEN**

- [ ] **Step 6: Commit**

```bash
git commit -m "COST-5691: Embed custom timeframes in Kafka messages, add org_id partition key"
```

---

## Part 2: ros-ocp-backend Tests (Go)

### Task 4: Settings API Client

**Spec ref:** IMPL §5.1, §5.6

**Files:**
- Create: `internal/utils/settings/settings.go`
- Create: `internal/utils/settings/settings_test.go`

**Run:** `go test -v ./internal/utils/settings/`

- [ ] **Step 1: Write test — FetchCustomTimeframes returns parsed config on 200**

```go
// internal/utils/settings/settings_test.go
package settings

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/google/go-cmp/cmp"
)

func TestFetchCustomTimeframes_Success(t *testing.T) {
    responseBody := CustomTimeframesResponse{
        Terms: []TermConfig{
            {Name: "term1", DurationDays: 3},
            {Name: "term2", DurationDays: 20},
        },
        BusinessHours: BusinessHoursConfig{Enabled: false},
    }
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify auth header is forwarded
        if r.Header.Get("x-rh-identity") != "test-identity" {
            t.Error("expected x-rh-identity header")
        }
        w.Header().Set("Content-Type", "application/json")
        resp := map[string]interface{}{
            "data": []interface{}{responseBody},
        }
        json.NewEncoder(w).Encode(resp)
    }))
    defer server.Close()

    result, err := FetchCustomTimeframes(server.URL, "test-identity")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if diff := cmp.Diff(result.Terms[0].DurationDays, 3); diff != "" {
        t.Error(diff)
    }
    if diff := cmp.Diff(result.Terms[1].DurationDays, 20); diff != "" {
        t.Error(diff)
    }
}
```

- [ ] **Step 2: Write test — returns defaults on 404**

```go
func TestFetchCustomTimeframes_404_ReturnsDefaults(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNotFound)
    }))
    defer server.Close()

    result, err := FetchCustomTimeframes(server.URL, "test-identity")
    if err != nil {
        t.Fatalf("404 should not return error, got: %v", err)
    }
    if len(result.Terms) != 3 {
        t.Errorf("expected 3 default terms, got %d", len(result.Terms))
    }
    if result.Terms[0].DurationDays != 1 {
        t.Errorf("expected default term1=1, got %d", result.Terms[0].DurationDays)
    }
}
```

- [ ] **Step 3: Write test — returns defaults on connection error**

```go
func TestFetchCustomTimeframes_ConnectionError_ReturnsDefaults(t *testing.T) {
    result, err := FetchCustomTimeframes("http://localhost:1", "test-identity")
    if err != nil {
        t.Fatalf("connection error should not propagate, got: %v", err)
    }
    if len(result.Terms) != 3 {
        t.Errorf("expected 3 default terms, got %d", len(result.Terms))
    }
}
```

- [ ] **Step 4: Write test — returns defaults on 500**

```go
func TestFetchCustomTimeframes_500_ReturnsDefaults(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer server.Close()

    result, err := FetchCustomTimeframes(server.URL, "test-identity")
    if err != nil {
        t.Fatalf("500 should not return error, got: %v", err)
    }
    if result.Terms[0].DurationDays != 1 {
        t.Errorf("expected default term1=1, got %d", result.Terms[0].DurationDays)
    }
}
```

- [ ] **Step 5: Write test — on-prem mode omits identity header**

```go
func TestFetchCustomTimeframes_OnPrem_NoAuthHeader(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("x-rh-identity") != "" {
            t.Error("on-prem should not send x-rh-identity header")
        }
        w.Header().Set("Content-Type", "application/json")
        resp := map[string]interface{}{
            "data": []interface{}{DefaultCustomTimeframes()},
        }
        json.NewEncoder(w).Encode(resp)
    }))
    defer server.Close()

    // Empty identity = on-prem mode
    result, err := FetchCustomTimeframes(server.URL, "")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(result.Terms) != 3 {
        t.Errorf("expected 3 terms, got %d", len(result.Terms))
    }
}
```

- [ ] **Step 6: Implement FetchCustomTimeframes to pass all tests**

- [ ] **Step 7: Run tests — verify GREEN**

- [ ] **Step 8: Commit**

```bash
git commit -m "COST-5691: Add Koku Settings API client with fallback to defaults"
```

---

### Task 5: Settings Change Detection

**Spec ref:** IMPL §5.2

**Files:**
- Create: `internal/services/settings_detector.go`
- Create: `internal/services/settings_detector_test.go`

**Run:** `go test -v ./internal/services/ -run TestSettingsDetector`

- [ ] **Step 1: Write test — detect change when embedded settings differ from stored**

```go
// internal/services/settings_detector_test.go
package services

import (
    "testing"

    "github.com/redhatinsights/ros-ocp-backend/internal/utils/settings"
)

func TestSettingsDetector_DetectsChange(t *testing.T) {
    stored := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{
            {Name: "term1", DurationDays: 1},
            {Name: "term2", DurationDays: 7},
            {Name: "term3", DurationDays: 15},
        },
    }
    incoming := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{
            {Name: "term1", DurationDays: 3},
            {Name: "term2", DurationDays: 20},
            {Name: "term3", DurationDays: 60},
        },
    }

    if !HasSettingsChanged(stored, incoming) {
        t.Error("expected settings change to be detected")
    }
}
```

- [ ] **Step 2: Write test — no change when settings identical**

```go
func TestSettingsDetector_NoChangeWhenIdentical(t *testing.T) {
    config := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{
            {Name: "term1", DurationDays: 1},
        },
    }
    if HasSettingsChanged(config, config) {
        t.Error("identical settings should not report change")
    }
}
```

- [ ] **Step 3: Write test — detects business hours change**

```go
func TestSettingsDetector_DetectsBusinessHoursChange(t *testing.T) {
    stored := &settings.CustomTimeframesResponse{
        Terms:         []settings.TermConfig{{Name: "term1", DurationDays: 1}},
        BusinessHours: settings.BusinessHoursConfig{Enabled: false},
    }
    incoming := &settings.CustomTimeframesResponse{
        Terms:         []settings.TermConfig{{Name: "term1", DurationDays: 1}},
        BusinessHours: settings.BusinessHoursConfig{Enabled: true, StartTime: "09:00", EndTime: "17:00", Timezone: "UTC", Weekdays: []int{1, 2, 3, 4, 5}},
    }
    if !HasSettingsChanged(stored, incoming) {
        t.Error("business hours change should be detected")
    }
}
```

- [ ] **Step 4: Write test — nil stored (first message) treated as change**

```go
func TestSettingsDetector_NilStoredIsChange(t *testing.T) {
    incoming := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
    }
    if !HasSettingsChanged(nil, incoming) {
        t.Error("nil stored settings should be treated as a change")
    }
}
```

- [ ] **Step 5: Implement HasSettingsChanged**

- [ ] **Step 6: Run tests — verify GREEN**

- [ ] **Step 7: Commit**

```bash
git commit -m "COST-5691: Add settings change detection for Kafka messages"
```

---

### Task 6: createExperiment Payload Extension

**Spec ref:** IMPL §5.1

**Files:**
- Modify: `internal/types/kruizePayload/createExperiment.go`
- Create: `internal/types/kruizePayload/createExperiment_custom_test.go`

**Run:** `go test -v ./internal/types/kruizePayload/ -run TestCreateExperimentCustom`

- [ ] **Step 1: Write test — payload includes term_settings when custom config provided**

```go
// internal/types/kruizePayload/createExperiment_custom_test.go
package kruizePayload

import (
    "encoding/json"
    "testing"
)

func TestCreateExperimentCustom_IncludesTermSettings(t *testing.T) {
    termSettings := &TermSettings{
        ShortTerm:  &TermDuration{DurationInDays: 3, ThresholdInPercent: 0.53},
        MediumTerm: &TermDuration{DurationInDays: 20, ThresholdInPercent: 0.53},
        LongTerm:   &TermDuration{DurationInDays: 60, ThresholdInPercent: 0.70},
    }
    containers := []map[string]string{
        {"container_name": "app", "container_image_name": "image:latest"},
    }
    data := map[string]string{
        "k8s_object_type": "deployment",
        "k8s_object_name": "my-app",
        "namespace":       "default",
    }

    payload, err := GetCreateExperimentPayloadWithSettings("test-exp", "cluster-1", containers, data, termSettings, nil)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    var parsed []map[string]interface{}
    json.Unmarshal(payload, &parsed)

    ts, ok := parsed[0]["term_settings"]
    if !ok {
        t.Fatal("expected term_settings in payload")
    }
    tsMap := ts.(map[string]interface{})
    shortTerm := tsMap["short_term"].(map[string]interface{})
    if shortTerm["duration_in_days"].(float64) != 3 {
        t.Errorf("expected short_term duration 3, got %v", shortTerm["duration_in_days"])
    }
}
```

- [ ] **Step 2: Write test — payload omits term_settings when nil (default behavior preserved)**

```go
func TestCreateExperimentCustom_OmitsTermSettingsWhenNil(t *testing.T) {
    containers := []map[string]string{
        {"container_name": "app", "container_image_name": "image:latest"},
    }
    data := map[string]string{
        "k8s_object_type": "deployment",
        "k8s_object_name": "my-app",
        "namespace":       "default",
    }

    payload, err := GetCreateExperimentPayloadWithSettings("test-exp", "cluster-1", containers, data, nil, nil)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    var parsed []map[string]interface{}
    json.Unmarshal(payload, &parsed)

    if _, ok := parsed[0]["term_settings"]; ok {
        t.Error("term_settings should be omitted when nil")
    }
}
```

- [ ] **Step 3: Write test — payload includes business_hours when provided**

```go
func TestCreateExperimentCustom_IncludesBusinessHours(t *testing.T) {
    bh := &BusinessHoursSettings{
        Enabled:   true,
        StartTime: "09:00",
        EndTime:   "17:00",
        Weekdays:  []int{1, 2, 3, 4, 5},
        Timezone:  "America/New_York",
    }
    containers := []map[string]string{
        {"container_name": "app", "container_image_name": "image:latest"},
    }
    data := map[string]string{
        "k8s_object_type": "deployment",
        "k8s_object_name": "my-app",
        "namespace":       "default",
    }

    payload, err := GetCreateExperimentPayloadWithSettings("test-exp", "cluster-1", containers, data, nil, bh)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    var parsed []map[string]interface{}
    json.Unmarshal(payload, &parsed)

    bhParsed, ok := parsed[0]["business_hours"]
    if !ok {
        t.Fatal("expected business_hours in payload")
    }
    bhMap := bhParsed.(map[string]interface{})
    if bhMap["timezone"] != "America/New_York" {
        t.Errorf("expected timezone America/New_York, got %v", bhMap["timezone"])
    }
}
```

- [ ] **Step 4: Implement the new structs and GetCreateExperimentPayloadWithSettings**

- [ ] **Step 5: Run tests — verify GREEN**

- [ ] **Step 6: Commit**

```bash
git commit -m "COST-5691: Extend createExperiment payload with custom term settings"
```

---

### Task 7: Kafka Message Parsing — CustomTimeframes Field

**Spec ref:** IMPL §5.2, §8.4

**Files:**
- Modify: `internal/types/kafkaMsg.go`
- Create: `internal/types/kafkaMsg_custom_test.go`

**Run:** `go test -v ./internal/types/ -run TestKafkaMsg`

- [ ] **Step 1: Write test — KafkaMsg unmarshals custom_timeframes from metadata**

```go
// internal/types/kafkaMsg_custom_test.go
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
```

- [ ] **Step 2: Write test — KafkaMsg without custom_timeframes still parses (backwards compatible)**

```go
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
```

- [ ] **Step 3: Implement CustomTimeframes struct and add to KafkaMsg.Metadata**

- [ ] **Step 4: Run tests — verify GREEN**

- [ ] **Step 5: Commit**

```bash
git commit -m "COST-5691: Add custom_timeframes to Kafka message struct"
```

---

## Part 3: Kruize (autotune) Tests (Java)

> **Note:** Kruize has minimal unit test infrastructure (1 test file). These tests establish the pattern for new test coverage. Use JUnit 5.

### Task 8: Terms.java — Fix Hardcoded Durations

**Spec ref:** IMPL §4.2

**Files:**
- Create: `src/test/java/com/autotune/analyzer/recommendations/term/TermsTest.java`
- Modify: `src/main/java/com/autotune/analyzer/recommendations/term/Terms.java`

**Run:** `mvn test -Dtest=TermsTest`

- [ ] **Step 1: Write test — setDurationBasedOnTerm uses days field, not hardcoded value**

```java
package com.autotune.analyzer.recommendations.term;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class TermsTest {

    @Test
    void setDurationBasedOnTerm_usesCustomDays() {
        Terms term = new Terms();
        term.setDays(30);
        term.setDurationBasedOnTerm();
        assertEquals(30 * 24, term.getDuration_in_hours(),
            "Duration should be days * 24 hours, not a hardcoded value");
    }

    @Test
    void setDurationBasedOnTerm_defaultShortTerm() {
        Terms term = new Terms();
        term.setDays(1);
        term.setDurationBasedOnTerm();
        assertEquals(24, term.getDuration_in_hours());
    }

    @Test
    void setDurationBasedOnTerm_defaultLongTerm() {
        Terms term = new Terms();
        term.setDays(15);
        term.setDurationBasedOnTerm();
        assertEquals(15 * 24, term.getDuration_in_hours());
    }
}
```

- [ ] **Step 2: Write test — getMaxDuration returns max of configured terms**

```java
    @Test
    void getMaxDuration_reflectsCustomDays() {
        Terms term = new Terms();
        term.setDays(90);
        term.setDurationBasedOnTerm();
        assertEquals(90, term.getMaxDuration(),
            "getMaxDuration should return days from the term, not hardcoded 15");
    }
```

- [ ] **Step 3: Fix Terms.java to read from this.days**

- [ ] **Step 4: Run tests — verify GREEN**

- [ ] **Step 5: Commit**

```bash
git commit -m "COST-5691: Fix Terms.java to use configured days instead of hardcoded values"
```

---

### Task 9: Data Retention Constant

**Spec ref:** IMPL §4.3

**Files:**
- Create: `src/test/java/com/autotune/analyzer/utils/AnalyzerConstantsTest.java`
- Modify: `src/main/java/com/autotune/analyzer/utils/AnalyzerConstants.java`

**Run:** `mvn test -Dtest=AnalyzerConstantsTest`

- [ ] **Step 1: Write test — retention threshold is 91 days**

```java
package com.autotune.analyzer.utils;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class AnalyzerConstantsTest {

    @Test
    void retentionThreshold_supports90DayTerms() {
        assertTrue(AnalyzerConstants.DurationAmount.DELETE_PARTITION_THRESHOLD_IN_DAYS >= 91,
            "Retention must be >= 91 to support 90-day term windows");
    }
}
```

- [ ] **Step 2: Change constant from 16 to 91**

- [ ] **Step 3: Run test — verify GREEN**

- [ ] **Step 4: Commit**

```bash
git commit -m "COST-5691: Raise data retention to 91 days for custom timeframes"
```

---

## Part 4: koku-ui Tests (TypeScript)

> **Note:** koku-ui tests use Jest + React Testing Library. These tests verify the duration display logic.

### Task 10: Duration Display Helper

**Spec ref:** IMPL §7.2

**Files:**
- Create: `apps/koku-ui-ros/src/utils/durationDisplay.ts`
- Create: `apps/koku-ui-ros/src/utils/durationDisplay.test.ts`

**Run:** `npm test --workspace apps/koku-ui-ros -- --testPathPattern=durationDisplay`

- [ ] **Step 1: Write test — formats hours to day labels**

```typescript
// apps/koku-ui-ros/src/utils/durationDisplay.test.ts
import { formatDurationLabel } from './durationDisplay';

describe('formatDurationLabel', () => {
  it('formats 24 hours as "1 day"', () => {
    expect(formatDurationLabel(24.0)).toBe('1 day');
  });

  it('formats 72 hours as "3 days"', () => {
    expect(formatDurationLabel(72.0)).toBe('3 days');
  });

  it('formats 480 hours as "20 days"', () => {
    expect(formatDurationLabel(480.0)).toBe('20 days');
  });

  it('formats 2160 hours as "90 days"', () => {
    expect(formatDurationLabel(2160.0)).toBe('90 days');
  });

  it('falls back to hours for non-whole days', () => {
    expect(formatDurationLabel(36.0)).toBe('36 hours');
  });

  it('handles null/undefined gracefully', () => {
    expect(formatDurationLabel(undefined)).toBe('Unknown');
    expect(formatDurationLabel(null)).toBe('Unknown');
  });
});
```

- [ ] **Step 2: Run test — verify FAIL**

- [ ] **Step 3: Implement formatDurationLabel**

```typescript
// apps/koku-ui-ros/src/utils/durationDisplay.ts
export const formatDurationLabel = (durationInHours: number | null | undefined): string => {
  if (durationInHours == null) {
    return 'Unknown';
  }
  const days = durationInHours / 24;
  if (Number.isInteger(days)) {
    return days === 1 ? '1 day' : `${days} days`;
  }
  return `${durationInHours} hours`;
};
```

- [ ] **Step 4: Run test — verify GREEN**

- [ ] **Step 5: Commit**

```bash
git commit -m "COST-5691: Add duration display helper for custom timeframes"
```

---

## Execution Summary

| Part | Repository | Tasks | Test Count | Focus |
|---|---|---|---|---|
| 1 | koku | 3 tasks (serializer, views, Kafka) | ~22 tests | Settings API validation, Kafka message changes |
| 2 | ros-ocp-backend | 4 tasks (client, detection, payload, parsing) | ~15 tests | Settings propagation, experiment payload |
| 3 | Kruize | 2 tasks (terms fix, retention) | ~5 tests | Core algorithm fixes |
| 4 | koku-ui | 1 task (duration display) | ~6 tests | Display logic |
| **Total** | **4 repos** | **10 tasks** | **~48 tests** | |

### TDD Cycle Per Task

```
1. Write RED tests (expect failures: ImportError, undefined, assertion fails)
2. Run tests — confirm they FAIL for the right reason
3. Write MINIMAL implementation to make tests GREEN
4. Run tests — confirm all PASS
5. Refactor if needed (keep tests GREEN)
6. Commit
```

### Test Dependency Order

Tests are designed to be independent, but implementation should follow the deployment order:

```
Task 1 (Koku serializer) ← no dependencies
Task 2 (Koku views) ← depends on Task 1 serializer
Task 3 (Koku Kafka) ← independent
Task 4 (ros-ocp-backend settings client) ← independent
Task 5 (ros-ocp-backend change detection) ← depends on Task 4 types
Task 6 (ros-ocp-backend payload) ← independent
Task 7 (ros-ocp-backend Kafka parsing) ← independent
Task 8 (Kruize terms fix) ← independent
Task 9 (Kruize retention) ← independent
Task 10 (koku-ui display) ← independent
```
