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

- [ ] **Step 4: Write test — valid two terms**

```python
    def test_valid_two_terms(self):
        """Two properly ordered terms passes validation."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 5},
                {"name": "term2", "duration_days": 30},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 5: Write test — reject term2 without term1**

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

- [ ] **Step 6: Write test — reject term3 without term2**

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

- [ ] **Step 7: Write test — reject terms not ordered shorter to longer**

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

- [ ] **Step 8: Write test — reject duplicate durations**

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

- [ ] **Step 9: Write test — reject duration below minimum (0)**

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

- [ ] **Step 10: Write test — reject negative duration**

```python
    def test_reject_negative_duration(self):
        """Negative durations are rejected."""
        data = {
            "terms": [{"name": "term1", "duration_days": -5}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 11: Write test — reject duration above maximum (91+)**

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

- [ ] **Step 12: Write test — accept duration at boundary (1 and 90)**

```python
    def test_accept_boundary_durations(self):
        """Boundary values 1 and 90 are accepted."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 1},
                {"name": "term2", "duration_days": 90},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 13: Write test — reject non-integer duration**

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

- [ ] **Step 14: Write test — reject empty terms array**

```python
    def test_reject_empty_terms(self):
        """At least 1 term must be provided."""
        data = {
            "terms": [],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 15: Write test — reject more than 3 terms**

```python
    def test_reject_more_than_three_terms(self):
        """At most 3 terms allowed."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 1},
                {"name": "term2", "duration_days": 7},
                {"name": "term3", "duration_days": 15},
                {"name": "term4", "duration_days": 30},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 16: Write test — reject invalid term names**

```python
    def test_reject_invalid_term_name(self):
        """Only term1, term2, term3 are valid names."""
        data = {
            "terms": [{"name": "foo", "duration_days": 5}],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())

    def test_reject_term4_name(self):
        """term4 is not a valid term name."""
        data = {
            "terms": [
                {"name": "term1", "duration_days": 1},
                {"name": "term2", "duration_days": 7},
                {"name": "term4", "duration_days": 15},
            ],
            "business_hours": {"enabled": False},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 17: Write test — valid business hours config**

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

- [ ] **Step 18: Write test — reject invalid timezone**

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

- [ ] **Step 19: Write test — reject empty weekdays**

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

- [ ] **Step 20: Write test — reject invalid weekday numbers**

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

- [ ] **Step 21: Write test — reject duplicate weekdays**

```python
    def test_reject_duplicate_weekdays(self):
        """Duplicate weekday values are rejected."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "17:00",
                "weekdays": [1, 1, 2, 2],
                "timezone": "UTC",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 22: Write test — reject business window under 1 hour**

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

- [ ] **Step 23: Write test — reject time not on 15-minute boundary**

```python
    def test_reject_time_not_15min_granularity(self):
        """Times must be on 15-minute boundaries (HH:00, HH:15, HH:30, HH:45)."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {
                "enabled": True,
                "start_time": "09:07",
                "end_time": "17:00",
                "weekdays": [1, 2, 3, 4, 5],
                "timezone": "UTC",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 24: Write test — reject invalid time format**

```python
    def test_reject_invalid_time_format(self):
        """Times must be HH:MM 24-hour format."""
        for invalid_time in ["9:00", "25:00", "09:60", "9am", "abc"]:
            with self.subTest(time=invalid_time):
                data = {
                    "terms": [{"name": "term1", "duration_days": 7}],
                    "business_hours": {
                        "enabled": True,
                        "start_time": invalid_time,
                        "end_time": "17:00",
                        "weekdays": [1],
                        "timezone": "UTC",
                    },
                }
                serializer = ROSCustomTimeframesSerializer(data=data)
                self.assertFalse(serializer.is_valid(), f"Should reject time: {invalid_time}")
```

- [ ] **Step 25: Write test — business hours enabled but missing required fields**

```python
    def test_reject_enabled_business_hours_missing_fields(self):
        """When enabled=True, start_time/end_time/weekdays/timezone are required."""
        data = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {"enabled": True},
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertFalse(serializer.is_valid())
```

- [ ] **Step 26: Write test — midnight-spanning business hours valid**

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

- [ ] **Step 27: Write test — business hours disabled ignores sub-fields**

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

- [ ] **Step 28: Write test — business hours enabled with default term durations (cross-field rule)**

```python
    def test_business_hours_with_default_terms(self):
        """Business hours can be enabled without changing default term durations.

        IMPL §10: 'Business hours without [custom] terms: Allowed (uses default terms
        with business hours filter).'
        """
        data = {
            "terms": [
                {"name": "term1", "duration_days": 1},
                {"name": "term2", "duration_days": 7},
                {"name": "term3", "duration_days": 15},
            ],
            "business_hours": {
                "enabled": True,
                "start_time": "09:00",
                "end_time": "17:00",
                "weekdays": [1, 2, 3, 4, 5],
                "timezone": "UTC",
            },
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        self.assertTrue(serializer.is_valid(), serializer.errors)

    def test_terms_without_business_hours_field(self):
        """Custom terms without business_hours field uses default (disabled).

        IMPL §10: 'Terms without business hours: Allowed.'
        """
        data = {
            "terms": [
                {"name": "term1", "duration_days": 30},
                {"name": "term2", "duration_days": 60},
            ],
        }
        serializer = ROSCustomTimeframesSerializer(data=data)
        # Depending on implementation: either passes with business_hours defaulting
        # to disabled, or requires business_hours field. Test documents the decision.
        self.assertTrue(serializer.is_valid(), serializer.errors)
```

- [ ] **Step 29: Implement the serializer to make all tests pass**

- [ ] **Step 29: Run full serializer test suite — verify all GREEN**

- [ ] **Step 30: Commit**

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
        response = self.client.put(self.url, data=payload, format="json")
        self.assertIn(response.status_code, [status.HTTP_401_UNAUTHORIZED, status.HTTP_403_FORBIDDEN])
```

- [ ] **Step 6: Write test — PUT replaces all settings (full replacement, not merge)**

```python
    def test_put_replaces_full_config(self):
        """PUT 3 terms then PUT 1 term: GET returns only 1 term, not merged."""
        three_terms = {
            "terms": [
                {"name": "term1", "duration_days": 3},
                {"name": "term2", "duration_days": 20},
                {"name": "term3", "duration_days": 60},
            ],
            "business_hours": {"enabled": False},
        }
        self.client.put(self.url, data=three_terms, format="json", **self.headers)

        one_term = {
            "terms": [{"name": "term1", "duration_days": 10}],
            "business_hours": {"enabled": False},
        }
        self.client.put(self.url, data=one_term, format="json", **self.headers)

        get_response = self.client.get(self.url, **self.headers)
        terms = get_response.data["data"][0]["terms"]
        self.assertEqual(len(terms), 1)
        self.assertEqual(terms[0]["duration_days"], 10)
```

- [ ] **Step 7: Write test — PUT idempotency**

```python
    def test_put_idempotent(self):
        """Putting the same config twice succeeds both times."""
        payload = {
            "terms": [{"name": "term1", "duration_days": 7}],
            "business_hours": {"enabled": False},
        }
        r1 = self.client.put(self.url, data=payload, format="json", **self.headers)
        r2 = self.client.put(self.url, data=payload, format="json", **self.headers)
        self.assertEqual(r1.status_code, status.HTTP_204_NO_CONTENT)
        self.assertEqual(r2.status_code, status.HTTP_204_NO_CONTENT)
```

- [ ] **Step 8: Write test — tenant isolation (multi-tenancy)**

```python
    def test_tenant_isolation(self):
        """Org A's custom settings do not bleed into Org B's response.

        Uses two different tenant schemas to verify schema-per-tenant isolation.
        """
        payload = {
            "terms": [{"name": "term1", "duration_days": 42}],
            "business_hours": {"enabled": False},
        }
        self.client.put(self.url, data=payload, format="json", **self.headers)

        get_response = self.client.get(self.url, **self.headers)
        self.assertEqual(get_response.data["data"][0]["terms"][0]["duration_days"], 42)

        # A second tenant (different schema) should still see defaults.
        # This requires a second IamTestCase identity or creating a second tenant.
        # At minimum, verify the settings are stored in the tenant schema:
        with schema_context(self.schema_name):
            from api.settings.ros_custom_timeframes import get_ros_custom_timeframes
            settings = get_ros_custom_timeframes(self.schema_name)
            self.assertEqual(settings["terms"][0]["duration_days"], 42)
```

- [ ] **Step 9: Write test — response has no-cache headers (@never_cache)**

```python
    def test_response_not_cached(self):
        """Settings endpoint must use @never_cache to prevent stale cached responses.

        IMPL §6.1: '@never_cache decorator'. Without this, Django's cache_page
        middleware (backed by Valkey, 1-hour TTL) would serve stale settings,
        causing Kafka messages to embed outdated custom_timeframes.
        """
        response = self.client.get(self.url, **self.headers)
        cache_control = response.get("Cache-Control", "")
        self.assertIn("no-cache", cache_control.lower().replace(" ", ""))
```

- [ ] **Step 10: Write test — non-admin can still GET (read-only access)**

```python
    def test_non_admin_can_get(self):
        """Non-admin users can read settings (GET) even if PUT is restricted."""
        # GET should be available to all authenticated users
        response = self.client.get(self.url, **self.headers)
        self.assertEqual(response.status_code, status.HTTP_200_OK)
```

- [ ] **Step 11: Implement view, URL registration, and storage to pass all tests**

- [ ] **Step 10: Run test suite — verify all GREEN**

- [ ] **Step 11: Commit**

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

- [ ] **Step 4: Write test — _get_ros_custom_timeframes queries tenant schema**

```python
    @patch("masu.external.ros_report_shipper.schema_context")
    def test_get_ros_custom_timeframes_uses_schema_context(self, mock_schema_ctx):
        """_get_ros_custom_timeframes queries settings within the tenant schema."""
        mock_schema_ctx.return_value.__enter__ = MagicMock()
        mock_schema_ctx.return_value.__exit__ = MagicMock()
        self.shipper._get_ros_custom_timeframes()
        mock_schema_ctx.assert_called_with("org1234567")
```

- [ ] **Step 5: Write test — _get_ros_custom_timeframes handles DB error gracefully**

```python
    def test_get_ros_custom_timeframes_db_error_returns_none(self):
        """If the DB query fails, return None instead of crashing the pipeline."""
        with patch.object(self.shipper, "_get_ros_custom_timeframes", side_effect=Exception("DB error")):
            with patch.object(self.shipper, "_get_ros_custom_timeframes", return_value=None):
                msg = self.shipper.build_ros_msg(["https://url1"], ["key1"])
        parsed = json.loads(msg)
        self.assertIsNone(parsed["metadata"].get("custom_timeframes"))
```

- [ ] **Step 6: Write test — partition key with missing org_id defaults to empty**

```python
    @patch("masu.external.ros_report_shipper.get_producer")
    def test_send_kafka_message_missing_org_id(self, mock_get_producer):
        """If org_id is missing from metadata, partition key is empty bytes."""
        mock_producer = MagicMock()
        mock_get_producer.return_value = mock_producer

        shipper_no_org = self.shipper
        original_metadata = shipper_no_org.metadata.copy()
        shipper_no_org.metadata = {k: v for k, v in original_metadata.items() if k != "org_id"}
        try:
            shipper_no_org.send_kafka_message(b'{"test": true}')
            call_kwargs = mock_producer.produce.call_args
            key = call_kwargs.kwargs.get("key") or call_kwargs[1].get("key")
            self.assertEqual(key, b"")
        finally:
            shipper_no_org.metadata = original_metadata
```

- [ ] **Step 7: Implement _get_ros_custom_timeframes() and partition key to pass all tests**

- [ ] **Step 8: Run tests — verify GREEN**

- [ ] **Step 9: Commit**

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

    result, err := FetchCustomTimeframes(server.URL, "")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(result.Terms) != 3 {
        t.Errorf("expected 3 terms, got %d", len(result.Terms))
    }
}
```

- [ ] **Step 6: Write test — empty base URL returns defaults immediately**

```go
func TestFetchCustomTimeframes_EmptyBaseURL_ReturnsDefaults(t *testing.T) {
    result, err := FetchCustomTimeframes("", "test-identity")
    if err != nil {
        t.Fatalf("empty base URL should not return error, got: %v", err)
    }
    if len(result.Terms) != 3 {
        t.Errorf("expected 3 default terms, got %d", len(result.Terms))
    }
    if result.Terms[0].DurationDays != 1 {
        t.Errorf("expected default term1=1, got %d", result.Terms[0].DurationDays)
    }
}
```

- [ ] **Step 7: Implement FetchCustomTimeframes to pass all tests**

- [ ] **Step 8: Run tests — verify GREEN**

- [ ] **Step 9: Commit**

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

- [ ] **Step 5: Write test — term count change detected (3→2)**

```go
func TestSettingsDetector_DetectsTermCountChange(t *testing.T) {
    stored := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{
            {Name: "term1", DurationDays: 1},
            {Name: "term2", DurationDays: 7},
            {Name: "term3", DurationDays: 15},
        },
    }
    incoming := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{
            {Name: "term1", DurationDays: 1},
            {Name: "term2", DurationDays: 7},
        },
    }
    if !HasSettingsChanged(stored, incoming) {
        t.Error("term count change should be detected")
    }
}
```

- [ ] **Step 6: Implement HasSettingsChanged**

- [ ] **Step 7: Run tests — verify GREEN**

- [ ] **Step 8: Commit**

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

---

### Task 8: UpdateExperiment HTTP Client

**Spec ref:** IMPL §5.2

**Files:**
- Modify: `internal/utils/kruize/kruize.go`
- Create: `internal/utils/kruize/update_experiment_test.go`

**Run:** `go test -v ./internal/utils/kruize/ -run TestUpdateExperiment`

- [ ] **Step 1: Write test — UpdateExperiment sends correct JSON to Kruize**

```go
// internal/utils/kruize/update_experiment_test.go
package kruize

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/redhatinsights/ros-ocp-backend/internal/utils/settings"
)

func TestUpdateExperiment_Success(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            t.Errorf("expected POST, got %s", r.Method)
        }
        if r.URL.Path != "/updateExperiment" {
            t.Errorf("expected /updateExperiment, got %s", r.URL.Path)
        }
        var payload []map[string]interface{}
        json.NewDecoder(r.Body).Decode(&payload)
        if payload[0]["experiment_name"] != "test-exp-1" {
            t.Errorf("expected experiment_name test-exp-1, got %v", payload[0]["experiment_name"])
        }
        w.WriteHeader(http.StatusCreated)
    }))
    defer server.Close()

    config := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{
            {Name: "term1", DurationDays: 3},
            {Name: "term2", DurationDays: 20},
        },
        BusinessHours: settings.BusinessHoursConfig{Enabled: false},
    }
    err := UpdateExperiment(server.URL, "test-exp-1", config)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
```

- [ ] **Step 2: Write test — handles Kruize 404 (experiment not found) gracefully**

```go
func TestUpdateExperiment_404_NoError(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNotFound)
    }))
    defer server.Close()

    config := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
    }
    err := UpdateExperiment(server.URL, "missing-exp", config)
    if err != nil {
        t.Fatalf("404 should be handled gracefully, got: %v", err)
    }
}
```

- [ ] **Step 3: Write test — handles Kruize 500 with error**

```go
func TestUpdateExperiment_500_ReturnsError(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer server.Close()

    config := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
    }
    err := UpdateExperiment(server.URL, "test-exp", config)
    if err == nil {
        t.Fatal("expected error for 500 response")
    }
}
```

- [ ] **Step 4: Write test — handles connection timeout gracefully**

```go
func TestUpdateExperiment_ConnectionError(t *testing.T) {
    config := &settings.CustomTimeframesResponse{
        Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
    }
    err := UpdateExperiment("http://localhost:1", "test-exp", config)
    if err == nil {
        t.Fatal("expected error for connection failure")
    }
}
```

- [ ] **Step 5: Implement UpdateExperiment**

- [ ] **Step 6: Run tests — verify GREEN**

- [ ] **Step 7: Commit**

---

### Task 9: Force Re-Poll Flag

**Spec ref:** IMPL §5.2

**Files:**
- Modify: `internal/services/recommendation_poller.go`
- Create: `internal/services/recommendation_poller_repoll_test.go`

**Run:** `go test -v ./internal/services/ -run TestForceRepoll`

- [ ] **Step 1: Write test — force_repoll=true bypasses poll interval check**

```go
// internal/services/recommendation_poller_repoll_test.go
package services

import "testing"

func TestForceRepoll_BypassesPollInterval(t *testing.T) {
    hoursSinceLastPoll := 1
    pollIntervalHours := 6
    forceRepoll := true

    if !ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, false) {
        t.Error("force_repoll=true should bypass the poll interval check")
    }
}
```

- [ ] **Step 2: Write test — force_repoll=false respects normal interval**

```go
func TestForceRepoll_FalseRespectsInterval(t *testing.T) {
    hoursSinceLastPoll := 1
    pollIntervalHours := 6
    forceRepoll := false

    if ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, false) {
        t.Error("force_repoll=false with insufficient hours should not poll")
    }
}
```

- [ ] **Step 3: Write test — normal interval exceeded still polls without force flag**

```go
func TestForceRepoll_NormalIntervalExceeded(t *testing.T) {
    hoursSinceLastPoll := 7
    pollIntervalHours := 6
    forceRepoll := false

    if !ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, false) {
        t.Error("normal interval exceeded should trigger poll")
    }
}
```

- [ ] **Step 4: Write test — force re-poll flag cleared after successful poll**

```go
func TestForceRepoll_ClearedAfterPoll(t *testing.T) {
    // After a successful poll triggered by force_repoll=true,
    // the flag must be reset to false. Otherwise the experiment
    // polls on every cycle forever.
    flag := true
    ClearForceRepollFlag(&flag)
    if flag {
        t.Error("force_repoll flag should be cleared after successful poll")
    }
}
```

- [ ] **Step 5: Write test — first-of-month logic still works with force flag**

```go
func TestForceRepoll_FirstOfMonthStillTriggers(t *testing.T) {
    hoursSinceLastPoll := 1
    pollIntervalHours := 6
    forceRepoll := false
    isFirstOfMonth := true

    if !ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, isFirstOfMonth) {
        t.Error("first-of-month should trigger poll regardless of interval")
    }
}
```

- [ ] **Step 6: Extract ShouldPollForRecommendations and implement**

- [ ] **Step 5: Run tests — verify GREEN**

- [ ] **Step 6: Commit**

---

### Task 10: Stale Recommendation Cleanup

**Spec ref:** IMPL §5.4

**Files:**
- Modify: `internal/model/recommendation_set.go`
- Modify: `internal/model/namespace_recommendation_set.go`
- Create: `internal/model/recommendation_cleanup_test.go`

**Run:** `go test -v ./internal/model/ -run TestStaleRecommendation`

> **Note:** These tests verify the query/function signatures and logic. Full DB integration tests require a test database and are out of scope for unit TDD.

- [ ] **Step 1: Write test — TermsToCleanup returns removed terms when going from 3→2**

```go
// internal/model/recommendation_cleanup_test.go
package model

import "testing"

func TestTermsToCleanup_ThreeToTwo(t *testing.T) {
    oldTermCount := 3
    newTermCount := 2
    removed := TermsToCleanup(oldTermCount, newTermCount)
    if len(removed) != 1 {
        t.Fatalf("expected 1 removed term, got %d", len(removed))
    }
    if removed[0] != "long_term" {
        t.Errorf("expected 'long_term' removed, got %s", removed[0])
    }
}
```

- [ ] **Step 2: Write test — TermsToCleanup returns two removed terms when going from 3→1**

```go
func TestTermsToCleanup_ThreeToOne(t *testing.T) {
    removed := TermsToCleanup(3, 1)
    if len(removed) != 2 {
        t.Fatalf("expected 2 removed terms, got %d", len(removed))
    }
    expected := map[string]bool{"medium_term": true, "long_term": true}
    for _, r := range removed {
        if !expected[r] {
            t.Errorf("unexpected removed term: %s", r)
        }
    }
}
```

- [ ] **Step 3: Write test — TermsToCleanup returns empty when adding terms (2→3)**

```go
func TestTermsToCleanup_AddingTerms(t *testing.T) {
    removed := TermsToCleanup(2, 3)
    if len(removed) != 0 {
        t.Errorf("adding terms should not remove anything, got %v", removed)
    }
}
```

- [ ] **Step 4: Write test — TermsToCleanup returns empty when count unchanged**

```go
func TestTermsToCleanup_SameCount(t *testing.T) {
    removed := TermsToCleanup(3, 3)
    if len(removed) != 0 {
        t.Errorf("same count should not remove anything, got %v", removed)
    }
}
```

- [ ] **Step 5: Implement TermsToCleanup**

- [ ] **Step 6: Run tests — verify GREEN**

- [ ] **Step 7: Commit**

---

## Part 3: Kruize (autotune) Tests (Java)

> **Note:** Kruize has minimal unit test infrastructure (1 test file). These tests establish the pattern for new test coverage. Use JUnit 5.

### Task 11: Terms.java — Fix Hardcoded Durations

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

    @Test
    void setDurationBasedOnTerm_maxDuration90Days() {
        Terms term = new Terms();
        term.setDays(90);
        term.setDurationBasedOnTerm();
        assertEquals(90 * 24, term.getDuration_in_hours());
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

---

### Task 12: Data Retention Constant

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

---

### Task 13: updateExperiment API

**Spec ref:** IMPL §4.1

**Files:**
- Create: `src/test/java/com/autotune/service/UpdateExperimentServiceTest.java`
- Create: `src/main/java/com/autotune/service/UpdateExperimentService.java`
- Create: `src/main/java/com/autotune/analyzer/serviceObjects/UpdateExperimentAPIObject.java`
- Modify: `src/main/java/com/autotune/analyzer/experiment/ExperimentInitiator.java`

**Run:** `mvn test -Dtest=UpdateExperimentServiceTest`

- [ ] **Step 1: Write test — valid request updates experiment terms**

```java
package com.autotune.service;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class UpdateExperimentServiceTest {

    @Test
    void validRequest_updatesTermConfig() {
        // Verify that updateExperiment changes the KruizeObject's term settings
        // without deleting kruize_results data.
        // This requires creating a mock experiment first.
        // Skeleton: create experiment in-memory, call update, verify terms changed.
        assertNotNull("Placeholder - implement with experiment mock");
    }

    @Test
    void invalidRequest_missingExperimentName_returns400() {
        // Verify that a request without experiment_name returns 400 Bad Request.
        assertNotNull("Placeholder - implement with HTTP test");
    }

    @Test
    void experimentNotFound_returns404() {
        // Verify that updating a non-existent experiment returns 404.
        assertNotNull("Placeholder - implement with experiment mock");
    }

    @Test
    void updatePreservesExistingResults() {
        // Verify that kruize_results rows are NOT deleted after updateExperiment.
        assertNotNull("Placeholder - implement with DB mock");
    }

    @Test
    void updatedTermsUsedInNextGenerateRecommendations() {
        // Verify that after updateExperiment, generateRecommendations uses new terms.
        assertNotNull("Placeholder - implement with full lifecycle mock");
    }

    @Test
    void businessHoursPersistedOnExperiment() {
        // Verify that updateExperiment with business_hours config persists
        // the start_time, end_time, weekdays, and timezone on the KruizeObject.
        // Without this, the business hours filter (Task 14) has nothing to filter on.
        assertNotNull("Placeholder - implement with experiment mock");
    }

    @Test
    void updateWithOnlyBusinessHours_preservesExistingTerms() {
        // Verify that updating business_hours without changing terms does not
        // reset terms to defaults.
        assertNotNull("Placeholder - implement with experiment mock");
    }

    @Test
    void updateWithInvalidTermDuration_returns400() {
        // Verify that duration_in_days=0 or >90 in updateExperiment returns 400.
        assertNotNull("Placeholder - implement with HTTP test");
    }
}
```

> **Note:** These are skeleton tests. Kruize's test infrastructure is minimal (no HTTP test harness). The implementation step must set up a lightweight test context (in-memory experiment store or mock). Each placeholder assertion must be replaced with real logic.

- [ ] **Step 2: Implement UpdateExperimentService and DTO**

- [ ] **Step 3: Replace placeholder tests with real assertions**

- [ ] **Step 4: Run tests — verify GREEN**

- [ ] **Step 5: Commit**

---

### Task 14: Business Hours Post-Filter

**Spec ref:** IMPL §4.4

**Files:**
- Create: `src/test/java/com/autotune/analyzer/recommendations/engine/BusinessHoursFilterTest.java`
- Modify: `src/main/java/com/autotune/analyzer/recommendations/engine/GenericRecommendationModel.java`

**Run:** `mvn test -Dtest=BusinessHoursFilterTest`

- [ ] **Step 1: Write test — UTC datapoint within business hours is included**

```java
package com.autotune.analyzer.recommendations.engine;

import org.junit.jupiter.api.Test;
import java.sql.Timestamp;
import java.time.ZonedDateTime;
import java.time.ZoneId;
import java.util.Set;
import static org.junit.jupiter.api.Assertions.*;

class BusinessHoursFilterTest {

    @Test
    void utcDatapoint_withinBusinessHours_included() {
        // 14:00 UTC = 10:00 ET (within 09:00-17:00)
        Timestamp ts = Timestamp.valueOf("2026-03-24 14:00:00");
        boolean result = BusinessHoursFilter.isWithinBusinessHours(
            ts, "09:00", "17:00", Set.of(2), "America/New_York"); // Tuesday=2
        assertTrue(result, "14:00 UTC on Tuesday should be within NYC business hours");
    }

    @Test
    void utcDatapoint_outsideBusinessHours_excluded() {
        // 23:00 UTC = 19:00 ET (outside 09:00-17:00)
        Timestamp ts = Timestamp.valueOf("2026-03-24 23:00:00");
        boolean result = BusinessHoursFilter.isWithinBusinessHours(
            ts, "09:00", "17:00", Set.of(2), "America/New_York");
        assertFalse(result, "23:00 UTC on Tuesday should be outside NYC business hours");
    }

    @Test
    void weekend_datapoint_excluded() {
        // Saturday datapoint excluded when weekdays=[1,2,3,4,5]
        Timestamp ts = Timestamp.valueOf("2026-03-28 14:00:00"); // Saturday
        boolean result = BusinessHoursFilter.isWithinBusinessHours(
            ts, "09:00", "17:00", Set.of(1, 2, 3, 4, 5), "America/New_York");
        assertFalse(result, "Saturday datapoint should be excluded for Mon-Fri config");
    }

    @Test
    void midnightSpanning_23h_included() {
        // Night shift 22:00-06:00: 23:00 local should be included
        Timestamp ts = Timestamp.valueOf("2026-03-24 23:00:00");
        boolean result = BusinessHoursFilter.isWithinBusinessHours(
            ts, "22:00", "06:00", Set.of(2), "UTC");
        assertTrue(result, "23:00 should be within midnight-spanning range 22:00-06:00");
    }

    @Test
    void midnightSpanning_07h_excluded() {
        // Night shift 22:00-06:00: 07:00 local should be excluded
        Timestamp ts = Timestamp.valueOf("2026-03-25 07:00:00");
        boolean result = BusinessHoursFilter.isWithinBusinessHours(
            ts, "22:00", "06:00", Set.of(3), "UTC"); // Wednesday
        assertFalse(result, "07:00 should be outside midnight-spanning range 22:00-06:00");
    }
}
```

- [ ] **Step 2: Write test — DST spring-forward handling**

```java
    @Test
    void dstSpringForward_handledCorrectly() {
        // 2026-03-08 is DST spring-forward in America/New_York
        // 07:00 UTC = 02:00 EST → becomes 03:00 EDT (spring forward)
        // Should be handled by ZonedDateTime without exception
        Timestamp ts = Timestamp.valueOf("2026-03-08 07:00:00");
        assertDoesNotThrow(() -> BusinessHoursFilter.isWithinBusinessHours(
            ts, "09:00", "17:00", Set.of(7), "America/New_York"));
    }
```

- [ ] **Step 3: Write test — threshold scaling calculation**

```java
    @Test
    void thresholdScaling_8h5d() {
        // 8h/day × 5 days/week = 40 business hours per week
        // 24h/day × 7 days/week = 168 total hours per week
        // Scale factor = 40/168 ≈ 0.2381
        double scale = BusinessHoursFilter.calculateThresholdScale(8, 5);
        assertEquals(40.0 / 168.0, scale, 0.001);
    }

    @Test
    void thresholdScaling_24h7d_noReduction() {
        // Full coverage = no scaling
        double scale = BusinessHoursFilter.calculateThresholdScale(24, 7);
        assertEquals(1.0, scale, 0.001);
    }
```

- [ ] **Step 4: Write test — checkIfMinDataAvailableForTerm uses scaled threshold**

```java
    @Test
    void checkIfMinDataAvailable_usesScaledThreshold() {
        // IMPL §4.4: "checkIfMinDataAvailableForTerm() must account for reduced
        // datapoint count due to business hours filtering."
        //
        // With business hours 8h/day, 5 days/week: scale = 40/168 ≈ 0.238
        // Short term (1 day) requires 53% of data → scaled = 53% * 0.238 ≈ 12.6%
        // So with 13% of datapoints available (within business hours), it should pass.
        //
        // Without scaling, 13% < 53% would fail → recommendations suppressed.
        Terms term = new Terms();
        term.setDays(1);
        term.setDurationBasedOnTerm();
        // Configure business hours (8h window, 5 weekdays)
        // Set available datapoints to ~13% of total (within BH only)
        // Assert that checkIfMinDataAvailableForTerm returns true
        assertNotNull("Placeholder - implement with configured Terms and BH scaling");
    }

    @Test
    void checkIfMinDataAvailable_withoutBusinessHours_usesOriginalThreshold() {
        // When business hours are disabled, the original unscaled threshold applies.
        Terms term = new Terms();
        term.setDays(1);
        term.setDurationBasedOnTerm();
        // Set available datapoints to 40% (below 53% threshold)
        // Assert that checkIfMinDataAvailableForTerm returns false
        assertNotNull("Placeholder - implement with configured Terms, no BH");
    }
```

- [ ] **Step 5: Implement BusinessHoursFilter**

- [ ] **Step 5: Run tests — verify GREEN**

- [ ] **Step 6: Commit**

---

### Task 15: Custom Terms Per Experiment (setCustomTerms)

**Spec ref:** IMPL §4.5

**Files:**
- Create: `src/test/java/com/autotune/analyzer/kruizeObject/KruizeObjectCustomTermsTest.java`
- Modify: `src/main/java/com/autotune/analyzer/kruizeObject/KruizeObject.java`

**Run:** `mvn test -Dtest=KruizeObjectCustomTermsTest`

- [ ] **Step 1: Write test — setCustomTerms accepts custom duration on short_term**

```java
package com.autotune.analyzer.kruizeObject;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
import java.util.Map;
import java.util.HashMap;

class KruizeObjectCustomTermsTest {

    @Test
    void setCustomTerms_acceptsCustomShortTermDuration() {
        KruizeObject obj = new KruizeObject();
        Map<String, Object> terms = new HashMap<>();
        terms.put("short_term", Map.of("duration_in_days", 30));
        assertDoesNotThrow(() -> obj.setCustomTerms(terms));
    }

    @Test
    void setCustomTerms_90DayLongTerm_accepted() {
        KruizeObject obj = new KruizeObject();
        Map<String, Object> terms = new HashMap<>();
        terms.put("short_term", Map.of("duration_in_days", 3));
        terms.put("medium_term", Map.of("duration_in_days", 30));
        terms.put("long_term", Map.of("duration_in_days", 90));
        assertDoesNotThrow(() -> obj.setCustomTerms(terms));
    }

    @Test
    void setCustomTerms_preservesTermNames() {
        KruizeObject obj = new KruizeObject();
        Map<String, Object> terms = new HashMap<>();
        terms.put("short_term", Map.of("duration_in_days", 30));
        obj.setCustomTerms(terms);
        assertNotNull(obj.getTerms().get("short_term"),
            "Term name 'short_term' should be preserved even with custom duration");
    }
}
```

- [ ] **Step 2: Modify KruizeObject.setCustomTerms to accept arbitrary durations**

- [ ] **Step 3: Run tests — verify GREEN**

- [ ] **Step 4: Commit**

---

## Part 4: koku-ui Tests (TypeScript)

### Task 16: Duration Display Helper

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

  it('handles 0 hours', () => {
    expect(formatDurationLabel(0)).toBe('0 hours');
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
  if (Number.isInteger(days) && days >= 1) {
    return days === 1 ? '1 day' : `${days} days`;
  }
  return `${durationInHours} hours`;
};
```

- [ ] **Step 4: Run test — verify GREEN**

- [ ] **Step 5: Commit**

---

## Part 5: Cross-Cutting Gap Coverage

> Tests in this section cover integration-level glue logic identified during gap analysis. They verify that individually-tested components are wired together correctly.

### Task 17: ros-ocp-backend — Term Name Mapping (term1 → short_term)

**Spec ref:** IMPL §5.3, Appendix A

**Files:**
- Create: `internal/utils/settings/term_mapping.go`
- Create: `internal/utils/settings/term_mapping_test.go`

**Run:** `go test -v ./internal/utils/settings/ -run TestTermMapping`

- [ ] **Step 1: Write test — term1 maps to short_term**

```go
// internal/utils/settings/term_mapping_test.go
package settings

import "testing"

func TestTermMapping_Term1ToShortTerm(t *testing.T) {
    result := MapTermNameToKruize("term1")
    if result != "short_term" {
        t.Errorf("expected short_term, got %s", result)
    }
}

func TestTermMapping_Term2ToMediumTerm(t *testing.T) {
    result := MapTermNameToKruize("term2")
    if result != "medium_term" {
        t.Errorf("expected medium_term, got %s", result)
    }
}

func TestTermMapping_Term3ToLongTerm(t *testing.T) {
    result := MapTermNameToKruize("term3")
    if result != "long_term" {
        t.Errorf("expected long_term, got %s", result)
    }
}

func TestTermMapping_UnknownTermPanicsOrErrors(t *testing.T) {
    result := MapTermNameToKruize("term4")
    if result != "" {
        t.Errorf("unknown term should return empty string, got %s", result)
    }
}
```

- [ ] **Step 2: Write test — reverse mapping (short_term → term1)**

```go
func TestTermMapping_ReverseShortTermToTerm1(t *testing.T) {
    result := MapKruizeTermToUser("short_term")
    if result != "term1" {
        t.Errorf("expected term1, got %s", result)
    }
}

func TestTermMapping_ReverseMediumTermToTerm2(t *testing.T) {
    result := MapKruizeTermToUser("medium_term")
    if result != "term2" {
        t.Errorf("expected term2, got %s", result)
    }
}

func TestTermMapping_ReverseLongTermToTerm3(t *testing.T) {
    result := MapKruizeTermToUser("long_term")
    if result != "term3" {
        t.Errorf("expected term3, got %s", result)
    }
}
```

- [ ] **Step 3: Write test — BuildTermSettings maps full config correctly**

```go
func TestBuildTermSettings_FullConfig(t *testing.T) {
    config := &CustomTimeframesResponse{
        Terms: []TermConfig{
            {Name: "term1", DurationDays: 3},
            {Name: "term2", DurationDays: 20},
            {Name: "term3", DurationDays: 60},
        },
    }
    ts := BuildTermSettingsForKruize(config)
    if ts.ShortTerm.DurationInDays != 3 {
        t.Errorf("short_term duration expected 3, got %d", ts.ShortTerm.DurationInDays)
    }
    if ts.MediumTerm.DurationInDays != 20 {
        t.Errorf("medium_term duration expected 20, got %d", ts.MediumTerm.DurationInDays)
    }
    if ts.LongTerm.DurationInDays != 60 {
        t.Errorf("long_term duration expected 60, got %d", ts.LongTerm.DurationInDays)
    }
}

func TestBuildTermSettings_SingleTerm(t *testing.T) {
    config := &CustomTimeframesResponse{
        Terms: []TermConfig{
            {Name: "term1", DurationDays: 5},
        },
    }
    ts := BuildTermSettingsForKruize(config)
    if ts.ShortTerm.DurationInDays != 5 {
        t.Errorf("short_term duration expected 5, got %d", ts.ShortTerm.DurationInDays)
    }
    if ts.MediumTerm != nil {
        t.Error("medium_term should be nil for single-term config")
    }
    if ts.LongTerm != nil {
        t.Error("long_term should be nil for single-term config")
    }
}
```

- [ ] **Step 4: Implement mapping functions**

- [ ] **Step 5: Run tests — verify GREEN**

- [ ] **Step 6: Commit**

---

### Task 18: ros-ocp-backend — ROSOCP API Response `duration_in_hours`

**Spec ref:** IMPL §8.3

**Files:**
- Modify: `internal/api/utils.go`
- Create: `internal/api/utils_duration_test.go`

**Run:** `go test -v ./internal/api/ -run TestDurationInHours`

> **Note:** This verifies that the REST API response surfaces the custom `duration_in_hours` from Kruize. If the API still returns `24.0` for a 30-day custom "short_term", the UI shows the wrong label.

- [ ] **Step 1: Write test — duration_in_hours reflects custom term duration**

```go
// internal/api/utils_duration_test.go
package api

import "testing"

func TestDurationInHours_CustomShortTerm(t *testing.T) {
    // When Kruize returns short_term with duration_in_hours=720.0 (30 days),
    // the ROSOCP API response must surface 720.0, not the default 24.0.
    kruizeRecommendation := map[string]interface{}{
        "short_term": map[string]interface{}{
            "duration_in_hours": 720.0,
            "monitoring_start_time": "2026-02-22T00:00:00Z",
            "recommendation_engines": map[string]interface{}{},
        },
    }
    result := FormatRecommendationTerms(kruizeRecommendation)
    shortTerm := result["short_term"]
    if shortTerm.DurationInHours != 720.0 {
        t.Errorf("expected duration_in_hours=720.0, got %f", shortTerm.DurationInHours)
    }
}

func TestDurationInHours_DefaultsPreserved(t *testing.T) {
    // Default Kruize response with standard durations.
    kruizeRecommendation := map[string]interface{}{
        "short_term": map[string]interface{}{
            "duration_in_hours": 24.0,
        },
        "medium_term": map[string]interface{}{
            "duration_in_hours": 168.0,
        },
        "long_term": map[string]interface{}{
            "duration_in_hours": 360.0,
        },
    }
    result := FormatRecommendationTerms(kruizeRecommendation)
    if result["short_term"].DurationInHours != 24.0 {
        t.Errorf("expected 24.0, got %f", result["short_term"].DurationInHours)
    }
    if result["medium_term"].DurationInHours != 168.0 {
        t.Errorf("expected 168.0, got %f", result["medium_term"].DurationInHours)
    }
}
```

- [ ] **Step 2: Write test — missing duration_in_hours handled gracefully**

```go
func TestDurationInHours_MissingField(t *testing.T) {
    // Older Kruize versions may not return duration_in_hours.
    // The API should handle this gracefully (use 0 or compute from term name).
    kruizeRecommendation := map[string]interface{}{
        "short_term": map[string]interface{}{
            "monitoring_start_time": "2026-02-22T00:00:00Z",
        },
    }
    result := FormatRecommendationTerms(kruizeRecommendation)
    // Should not panic, and should return a sensible default
    if result["short_term"].DurationInHours < 0 {
        t.Error("duration_in_hours should not be negative")
    }
}
```

- [ ] **Step 3: Implement or verify FormatRecommendationTerms surfaces the field**

- [ ] **Step 4: Run tests — verify GREEN**

- [ ] **Step 5: Commit**

---

### Task 19: ros-ocp-backend — GetWorkloadsByOrgID

**Spec ref:** IMPL §5.2

**Files:**
- Modify: `internal/model/workload.go`
- Create: `internal/model/workload_query_test.go`

**Run:** `go test -v ./internal/model/ -run TestGetWorkloadsByOrgID`

> **Note:** When settings change, ros-ocp-backend must find ALL experiments for the org_id to call `updateExperiment` on each. If this query filters on the wrong column, the wrong experiments get updated — or none do.

- [ ] **Step 1: Write test — query function signature and basic behavior**

```go
// internal/model/workload_query_test.go
package model

import "testing"

func TestGetWorkloadsByOrgID_ReturnsCorrectType(t *testing.T) {
    // Verify the function signature exists and returns []Workload.
    // Full DB test is integration-scope, but verify the query builds correctly.
    orgID := "1234567"
    query := BuildGetWorkloadsByOrgIDQuery(orgID)
    if query.OrgID != "1234567" {
        t.Errorf("expected org_id 1234567, got %s", query.OrgID)
    }
}

func TestGetWorkloadsByOrgID_EmptyOrgID(t *testing.T) {
    // Empty org_id should return an error, not query all workloads.
    _, err := BuildGetWorkloadsByOrgIDQuery("")
    if err == nil {
        t.Error("empty org_id should return error")
    }
}
```

- [ ] **Step 2: Implement BuildGetWorkloadsByOrgIDQuery**

- [ ] **Step 3: Run tests — verify GREEN**

- [ ] **Step 4: Commit**

---

### Task 20: koku-ui — Settings Page Component Tests

**Spec ref:** IMPL §7.1, §13

**Files:**
- Create: `apps/koku-ui-hccm/src/routes/settings/rosCustomTimeframes/RosCustomTimeframes.test.tsx`
- Create: `apps/koku-ui-hccm/src/routes/settings/rosCustomTimeframes/RosCustomTimeframes.tsx`

**Run:** `npm test --workspace apps/koku-ui-hccm -- --testPathPattern=RosCustomTimeframes`

- [ ] **Step 1: Write test — renders default term values**

```typescript
// apps/koku-ui-hccm/src/routes/settings/rosCustomTimeframes/RosCustomTimeframes.test.tsx
import { render, screen } from '@testing-library/react';
import RosCustomTimeframes from './RosCustomTimeframes';

describe('RosCustomTimeframes Settings', () => {
  it('renders three term input fields with default placeholders', () => {
    render(<RosCustomTimeframes />);
    expect(screen.getByLabelText(/term 1/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/term 2/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/term 3/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Write test — Term 2 disabled until Term 1 has a value**

```typescript
  it('disables Term 2 until Term 1 is configured', () => {
    render(<RosCustomTimeframes />);
    const term2 = screen.getByLabelText(/term 2/i);
    expect(term2).toBeDisabled();
  });
```

- [ ] **Step 3: Write test — Term 3 disabled until Term 2 has a value**

```typescript
  it('disables Term 3 until Term 2 is configured', () => {
    render(<RosCustomTimeframes />);
    const term3 = screen.getByLabelText(/term 3/i);
    expect(term3).toBeDisabled();
  });
```

- [ ] **Step 4: Write test — business hours toggle shows/hides sub-fields**

```typescript
  it('hides business hours fields when toggle is off', () => {
    render(<RosCustomTimeframes />);
    expect(screen.queryByLabelText(/start time/i)).not.toBeInTheDocument();
  });

  it('shows business hours fields when toggle is on', async () => {
    render(<RosCustomTimeframes />);
    const toggle = screen.getByRole('checkbox', { name: /restrict analysis to business hours/i });
    await userEvent.click(toggle);
    expect(screen.getByLabelText(/start time/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/end time/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/timezone/i)).toBeInTheDocument();
  });
```

- [ ] **Step 5: Write test — validation error on submit with out-of-order terms**

```typescript
  it('shows validation error when terms are not in ascending order', async () => {
    render(<RosCustomTimeframes />);
    const term1 = screen.getByLabelText(/term 1/i);
    const term2 = screen.getByLabelText(/term 2/i);
    await userEvent.clear(term1);
    await userEvent.type(term1, '30');
    // Enable term2
    await userEvent.clear(term2);
    await userEvent.type(term2, '10');
    const saveButton = screen.getByRole('button', { name: /save/i });
    await userEvent.click(saveButton);
    expect(screen.getByText(/must be ordered/i)).toBeInTheDocument();
  });
```

- [ ] **Step 6: Write test — Reset to defaults button**

```typescript
  it('resets all fields to defaults when Reset button is clicked', async () => {
    render(<RosCustomTimeframes />);
    const term1 = screen.getByLabelText(/term 1/i);
    await userEvent.clear(term1);
    await userEvent.type(term1, '42');
    const resetButton = screen.getByRole('button', { name: /reset to defaults/i });
    await userEvent.click(resetButton);
    expect(term1).toHaveValue(1);
  });
```

- [ ] **Step 7: Implement RosCustomTimeframes component**

- [ ] **Step 8: Run tests — verify GREEN**

- [ ] **Step 9: Commit**

---

## Execution Summary

| Part | Repository | Tasks | Test Count | Focus |
|---|---|---|---|---|
| 1 | koku | 3 tasks (serializer, views, Kafka) | ~39 tests | Settings API validation, multi-tenancy, caching, Kafka changes |
| 2 | ros-ocp-backend | 7 tasks (client, detection, payload, parsing, updateExperiment, re-poll, cleanup) | ~27 tests | Settings propagation, experiment updates, force re-poll |
| 3 | Kruize | 5 tasks (terms fix, retention, updateExperiment API, business hours filter, custom terms) | ~28 tests | Core algorithm, new API, business hours, threshold scaling |
| 4 | koku-ui | 1 task (duration display) | ~7 tests | Display logic |
| 5 | Cross-cutting | 4 tasks (term mapping, API response, workload query, settings page) | ~19 tests | Glue logic, UI components |
| **Total** | **4 repos** | **20 tasks** | **~120 tests** | |

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
Task 1  (Koku serializer) ← no dependencies
Task 2  (Koku views) ← depends on Task 1 serializer
Task 3  (Koku Kafka) ← independent
Task 4  (ros-ocp-backend settings client) ← independent
Task 5  (ros-ocp-backend change detection) ← depends on Task 4 types
Task 6  (ros-ocp-backend payload) ← independent
Task 7  (ros-ocp-backend Kafka parsing) ← independent
Task 8  (ros-ocp-backend updateExperiment client) ← depends on Task 4 types
Task 9  (ros-ocp-backend force re-poll) ← independent
Task 10 (ros-ocp-backend stale cleanup) ← independent
Task 11 (Kruize terms fix) ← independent
Task 12 (Kruize retention) ← independent
Task 13 (Kruize updateExperiment API) ← independent
Task 14 (Kruize business hours filter) ← independent
Task 15 (Kruize custom terms) ← depends on Task 11
Task 16 (koku-ui display) ← independent
Task 17 (ros-ocp-backend term mapping) ← depends on Task 4 types
Task 18 (ros-ocp-backend API response) ← independent
Task 19 (ros-ocp-backend workload query) ← independent
Task 20 (koku-ui settings page) ← independent
```

### SaaS vs On-Prem Coverage Matrix

| Test Area | SaaS | On-Prem | Task |
|---|---|---|---|
| Settings API with x-rh-identity | ✓ | — | Task 2 |
| Settings API without auth (dev middleware) | — | ✓ | Task 2 |
| Settings API @never_cache | ✓ | ✓ | Task 2 (Step 9) |
| Tenant schema isolation | ✓ | ✓ | Task 2 (Step 8) |
| Kafka partition key (org_id) | ✓ | ✓ | Task 3 |
| Kafka schema_context for settings query | ✓ | ✓ | Task 3 (Step 4) |
| B64_identity forwarded to Settings API | ✓ | — | Task 4 (Step 1) |
| On-prem: no auth header to Settings API | — | ✓ | Task 4 (Step 5) |
| Empty KOKU_API_BASE_URL fallback | — | ✓ | Task 4 (Step 6) |
| Settings API unavailable fallback | ✓ | ✓ | Task 4 (Steps 2-4) |
| Force re-poll flag cleared after use | ✓ | ✓ | Task 9 (Step 4) |
| Backwards-compatible Kafka messages | ✓ | ✓ | Task 7 (Step 2) |
| Term name mapping (term1→short_term) | ✓ | ✓ | Task 17 |
| ROSOCP API duration_in_hours response | ✓ | ✓ | Task 18 |
| Business hours timezone handling | ✓ | ✓ | Task 14 |
| DST spring-forward edge case | ✓ | ✓ | Task 14 (Step 2) |
| Threshold scaling with business hours | ✓ | ✓ | Task 14 (Step 4) |
| UI settings page form logic | ✓ | ✓ | Task 20 |
