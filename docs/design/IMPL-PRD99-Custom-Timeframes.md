# PRD99 COST-5691: Custom Timeframes for ROS OCP — Implementation Specification

> **Status:** Ready for implementation
> **PRD:** PRD99 ROSOCP Custom timeframes.docx
> **JIRA:** COST-5691
> **Date:** 2026-03-24
> **Scope:** Kruize (autotune), ros-ocp-backend, koku, koku-ui

---

## Table of Contents

- [1. Overview](#1-overview)
- [2. Architecture](#2-architecture)
- [3. Confirmed Decisions](#3-confirmed-decisions)
- [4. Kruize (autotune)](#4-kruize-autotune)
- [5. ros-ocp-backend](#5-ros-ocp-backend)
- [6. Koku Backend](#6-koku-backend)
- [7. koku-ui Frontend](#7-koku-ui-frontend)
- [8. API Contracts](#8-api-contracts)
- [9. Integration Points](#9-integration-points)
- [10. Validation Rules](#10-validation-rules)
- [11. Known Limitations](#11-known-limitations)
- [12. Deployment Plan](#12-deployment-plan)
- [13. Testing Strategy](#13-testing-strategy)
- [14. PRD Deviations](#14-prd-deviations)

---

## 1. Overview

### Goal

Let Resource Optimization for OpenShift customers choose the timeframe used to generate recommendations, and optionally restrict analysis to business hours only.

### Current State

ROS OCP generates two sets of recommendations (cost-focused and performance-focused) based on three fixed periods: **24 hours** (short term), **7 days** (medium term), **15 days** (long term). All 24 hours of each day are considered. This is driven by Kruize autotune's default term configuration.

### Target State

Customers can:
1. **Define custom term durations** (e.g., 3 days, 20 days, 60 days) up to 90 days.
2. **Restrict analysis to business hours** (e.g., Mon-Fri 9:00-17:00 in a specific timezone).

Settings are per-tenant (org_id), apply to all clusters for that tenant, and are managed through a new Settings page in the Cost Management UI.

### Affected Repositories

| Repository | Branch | Changes |
|---|---|---|
| `autotune` (Kruize) | `remote_monitoring` | New API, retention, business hours, term fixes |
| `ros-ocp-backend` | `main` | Settings propagation, experiment updates, API mapping |
| `koku` | `main` | New Settings endpoint, Kafka changes |
| `koku-ui` | `main` | Settings page, dynamic recommendation labels |

---

## 2. Architecture

### Data Flow (Current)

```
Operator → CSV → Koku masu → Kafka (hccm.ros.events) → ros-ocp-backend
                                                              │
                                           ┌──────────────────┤
                                           ▼                  ▼
                                   createExperiment    updateResults
                                       (Kruize)          (Kruize)
                                                              │
                                                              ▼
                                       Kafka (rosocp.kruize.recommendations)
                                                              │
                                                              ▼
                                                    recommendation-poller
                                                              │
                                                              ▼
                                                   listRecommendations
                                                       (Kruize)
                                                              │
                                                              ▼
                                                 ros-ocp-backend DB
                                                              │
                                                              ▼
                                                    ROSOCP REST API
                                                              │
                                                              ▼
                                                         koku-ui
```

### Data Flow (New — settings propagation)

```
User → koku-ui → Koku Settings API (write custom timeframes)
                        │
                        ▼
            Embedded in hccm.ros.events Kafka messages (change detection)
                        │
                        ▼
              ros-ocp-backend detects settings change
                        │
                ┌───────┴───────┐
                ▼               ▼
    Query Koku Settings    Call Kruize
    API (authoritative)    updateExperiment
                               │
                               ▼
                    Kruize regenerates
                    recommendations with
                    new term config on
                    next poll cycle
```

### Settings Flow for New Experiments

```
Kafka message arrives at ros-ocp-backend
        │
        ▼
  Before createExperiment:
  Query Koku Settings API for org_id's custom timeframes
        │
        ├── Settings API returns custom config → inject into createExperiment
        └── Settings API returns 404 or error → use defaults (1d, 7d, 15d)
```

---

## 3. Confirmed Decisions

All decisions below were confirmed through iterative Q&A with the product owner.

### Architecture & Scope

| # | Decision | Rationale |
|---|---|---|
| 1 | Per-org settings stored in Koku, propagated to Kruize via ros-ocp-backend | Koku is the settings authority; Kruize is the execution engine |
| 2 | New Kruize `updateExperiment` API preserves existing results and changes term config | Avoids re-ingestion of historical data |
| 3 | Show recommendations based on available data using Kruize's existing threshold mechanism | Users see partial results rather than waiting for full data coverage |
| 4 | Kruize work targets the `remote_monitoring` branch | Production branch for remote monitoring use case |
| 5 | Business hours include weekday picker (not just time range) | Supports Mon-Fri work week scenarios |
| 6 | Kruize uses Java `ZonedDateTime` for UTC→local conversion when applying business hours | Correctly handles DST transitions per-interval |
| 7 | Settings are per-tenant only (no per-namespace/workload granularity) | Simplifies Phase 1; per-workload settings are future work |
| 8 | On settings change: Kruize updates via `updateExperiment`; ros-ocp-backend re-polls; Koku does not reflect "recalculating" state | Avoids complex state machine in the API |
| 9 | Feature works in both SaaS and on-prem deployments | Same code path, different infra |
| 10 | RBAC: Cost Management Administrators only | Consistent with existing settings permissions |
| 11 | No cost model dependency | Custom timeframes are independent of cost model configuration |

### Data & Retention

| # | Decision | Rationale |
|---|---|---|
| 12 | Global data retention raised from 16 to 91 days | Supports up to 90-day term windows |
| 13 | Business hours threshold scaling: linear reduction proportional to coverage | 8h business day = 1/3 of 24h → data thresholds scale by same factor |
| 14 | Existing experiments updated via `updateExperiment` — no re-ingestion needed | Historical data in Kruize remains intact |
| 15 | DST handling: known limitation documented; UTC timestamps throughout pipeline; Kruize handles per-interval conversion | Full timezone support is future work |
| 16 | Default term durations: 1 day, 7 days, 15 days (current Kruize defaults) | No change for customers who don't configure custom timeframes |
| 17 | Settings applied at next iteration boundary (complete current cycle first) | Avoids partial/inconsistent recommendations |
| 18 | Terms ordered shorter to longer | Logical consistency; validated at API level |

### API & Naming

| # | Decision | Rationale |
|---|---|---|
| 19 | Keep `short_term`/`medium_term`/`long_term` keys in ROSOCP API responses | Avoids breaking change for UI and API consumers |
| 20 | ros-ocp-backend maps term1→short_term, term2→medium_term, term3→long_term internally | Internal mapping layer; Kruize uses short/medium/long internally |
| 21 | Sequential term definition: must define term1 before term2, term2 before term3 | Terms 2 and 3 are optional; prevents invalid partial configs |
| 22 | Experiment updates iterate one-by-one (bulk updates future feature) | Simpler implementation; acceptable given typical experiment counts |

### Integration

| # | Decision | Rationale |
|---|---|---|
| 23 | Auth for ros-ocp-backend → Koku Settings API: use `B64_identity` from Kafka message (SaaS); unauthenticated internal HTTP (on-prem) | Follows existing housekeeper pattern for on-prem |
| 24 | Koku Settings API is the single source of truth for validation constraints | Prevents divergence between Koku and Kruize validation |
| 25 | Deploy order: koku → Kruize → ros-ocp-backend → koku-ui; graceful fallback to defaults | ros-ocp-backend handles missing Settings API (404) by using defaults |

---

## 4. Kruize (autotune)

**Repository:** `~/dev/koku/autotune/`
**Branch:** `remote_monitoring`

### 4.1 New `updateExperiment` API

**Purpose:** Allow ros-ocp-backend to update an existing experiment's term configuration without deleting results or requiring re-ingestion.

**Endpoint:** `POST /updateExperiment`

**Request Body:**
```json
[
  {
    "experiment_name": "1234567|1|uuid|namespace|deployment|name",
    "recommendation_settings": {
      "threshold": "0.1"
    },
    "term_settings": {
      "short_term": { "duration_in_days": 3, "threshold_in_percent": 0.53 },
      "medium_term": { "duration_in_days": 20, "threshold_in_percent": 0.53 },
      "long_term": { "duration_in_days": 60, "threshold_in_percent": 0.70 }
    },
    "business_hours": {
      "enabled": true,
      "start_time": "09:00",
      "end_time": "17:00",
      "weekdays": [1, 2, 3, 4, 5],
      "timezone": "America/New_York"
    }
  }
]
```

**Response:** `201 Created` on success, `400 Bad Request` for invalid config.

**Behavior:**
- Preserves all existing `kruize_results` data for the experiment.
- Updates the experiment's `KruizeObject` term configuration via `setCustomTerms`.
- Next call to `generateRecommendations` uses the new term config.
- If `business_hours.enabled` is `true`, Kruize applies a post-filter to datapoints: convert each interval's UTC timestamp to the specified timezone using `ZonedDateTime`, and include only intervals where the local time falls within `[start_time, end_time)` on one of the specified weekdays.

**Files to modify/create:**
- `src/main/java/com/autotune/analyzer/serviceObjects/UpdateExperimentAPIObject.java` — new request DTO
- `src/main/java/com/autotune/analyzer/experiment/ExperimentInitiator.java` — update experiment method
- `src/main/java/com/autotune/service/UpdateExperimentService.java` — new REST endpoint handler
- `src/main/java/com/autotune/analyzer/kruizeObject/KruizeObject.java` — extend `setCustomTerms` to accept arbitrary durations

### 4.2 Fix Hardcoded Term Durations

**Problem:** `Terms.java` methods `setDurationBasedOnTerm()` and `getMaxDuration()` hardcode duration values (1, 7, 15 days) instead of reading from `Terms.days`.

**Files to modify:**
- `src/main/java/com/autotune/analyzer/recommendations/term/Terms.java`
  - `setDurationBasedOnTerm()`: Read from `this.days` instead of hardcoded switch.
  - `getMaxDuration()`: Compute from `this.days` instead of hardcoded 15.

### 4.3 Raise Data Retention

**Change:** `DELETE_PARTITION_THRESHOLD_IN_DAYS` from `16` to `91`.

**File:** `src/main/java/com/autotune/analyzer/utils/AnalyzerConstants.java`

```java
// Before
public static final int DELETE_PARTITION_THRESHOLD_IN_DAYS = 16;

// After
public static final int DELETE_PARTITION_THRESHOLD_IN_DAYS = 91;
```

**Impact:** `RetentionPartition.deletePartitions()` in `src/main/java/com/autotune/jobs/RetentionPartition.java` will retain 91 days of data instead of 16. Storage increase is proportional (~5.7x). For most deployments, the `extended_data` jsonb column in `kruize_results` is the primary storage consumer. Monitor PostgreSQL disk usage after deployment.

### 4.4 Business Hours Post-Filter

**Purpose:** When business hours are configured, Kruize filters datapoints to only include intervals that fall within the specified business hours window in the customer's timezone.

**Algorithm:**
1. For each datapoint's UTC interval start time, convert to the experiment's configured timezone using `ZonedDateTime.ofInstant(instant, ZoneId.of(timezone))`.
2. Extract local hour and day-of-week.
3. Include the datapoint only if:
   - Day-of-week is in the configured `weekdays` set (ISO 8601: 1=Monday, 7=Sunday).
   - Local hour is within `[start_hour, end_hour)`. For midnight-spanning ranges (start > end), use `hour >= start OR hour < end`.
4. Apply data threshold scaling: multiply the original threshold by `(business_hours_per_day * business_days_per_week) / (24 * 7)`.

**Files to modify:**
- `src/main/java/com/autotune/analyzer/recommendations/engine/GenericRecommendationModel.java` — add filtering logic before percentile calculations
- `src/main/java/com/autotune/analyzer/recommendations/term/Terms.java` — `checkIfMinDataAvailableForTerm()` must account for reduced datapoint count

### 4.5 Custom Terms Per Experiment

**Current behavior:** `KruizeObject.setCustomTerms()` accepts 1-3 terms but restricts names to "short", "medium", "long".

**Required behavior:** Accept custom durations for each term name. The term names ("short_term", "medium_term", "long_term") remain fixed — only the `days` property changes.

**File:** `src/main/java/com/autotune/analyzer/kruizeObject/KruizeObject.java`

---

## 5. ros-ocp-backend

**Repository:** `~/dev/koku/ros-ocp-backend/`
**Branch:** `main`

### 5.1 Query Koku Settings API During Experiment Creation

**When:** Before calling Kruize's `createExperiment` in `report_processor.go`.

**How:**
1. Build the Koku Settings API URL: `{KOKU_API_BASE_URL}/api/cost-management/v1/account-settings/ros-custom-timeframes/`
2. Set the `x-rh-identity` header from `kafkaMsg.B64_identity` (SaaS) or omit it (on-prem).
3. If the API returns a valid response, parse the term durations and business hours.
4. Inject the custom settings into the `createExperiment` payload's `term_settings` and `business_hours` fields.
5. If the API returns 404, connection error, or any error, fall back to default settings (no custom terms, no business hours).

**Files to modify:**
- `internal/types/kruizePayload/createExperiment.go` — extend `createExperiment` struct with `TermSettings` and `BusinessHours` fields; extend `RecommendationSettings` struct
- `internal/utils/kruize/kruize.go` — add HTTP client for Koku Settings API
- `internal/services/report_processor.go` — call Settings API before `Create_kruize_experiments()`
- `internal/config/config.go` — add `KOKU_API_BASE_URL` config

**Current `createExperiment` payload** (`internal/types/kruizePayload/createExperiment.go:35-54`):
```go
payload := []createExperiment{
    {
        // ...
        Trial_settings:          TrialSettings{Measurement_duration: "15min"},
        Recommendation_settings: RecommendationSettings{Threshold: "0.1"},
        // NEW: add TermSettings and BusinessHours here
    },
}
```

### 5.2 Detect Settings Changes and Call `updateExperiment`

**When:** ros-ocp-backend receives `hccm.ros.events` messages that contain embedded settings (new field). Compare embedded settings with last-known settings for the org_id.

**How:**
1. Parse the new `custom_timeframes` field from the Kafka message metadata.
2. Compare with the last-known settings stored in ros-ocp-backend's database (new column or table).
3. If settings changed:
   a. Query Koku Settings API for authoritative values.
   b. For each experiment belonging to the org_id, call Kruize `updateExperiment`.
   c. Set a "force re-poll" flag on affected experiments so the recommendation poller fetches new recommendations on its next cycle instead of waiting for `RECOMMENDATION_POLL_INTERVAL_HOURS`.

**Files to modify:**
- `internal/types/kafkaMsg.go` — add `CustomTimeframes` field to `KafkaMsg.Metadata`
- `internal/services/report_processor.go` — settings change detection logic after parsing Kafka message
- `internal/utils/kruize/kruize.go` — new `UpdateExperiment()` function
- `internal/model/workload.go` — add query method to get all experiments for an org_id
- `internal/services/recommendation_poller.go` — honor "force re-poll" flag, bypassing `RECOMMENDATION_POLL_INTERVAL_HOURS` check

**Current poll interval check** (`internal/services/recommendation_poller.go:392`):
```go
if int(duration.Hours()) >= cfg.RecommendationPollIntervalHours || utils.NeedRecommOnFirstOfMonth(...) {
    // Also check force_repoll flag here
}
```

### 5.3 Map Term Names in API Responses

**Current code** iterates `short_term`, `medium_term`, `long_term` in `internal/api/utils.go:636`:
```go
for _, period := range []string{"short_term", "medium_term", "long_term"} {
```

**Change:** No change to iteration keys. The API response continues to use `short_term`/`medium_term`/`long_term`. The `duration_in_hours` field within each term reflects the actual configured duration (e.g., `720.0` for a 30-day term under `short_term`).

The UI will use `duration_in_hours` for display labels rather than the key name.

### 5.4 Clean Up Stale Recommendations on Term Removal

**When:** Settings change from 3 terms to 2 (or 2 to 1). The removed term's recommendations become stale.

**How:** After `updateExperiment`, if the new configuration has fewer terms, delete recommendation data for the removed term(s) from ros-ocp-backend's database tables (`recommendation_sets`, `historical_recommendation_sets`, `namespace_recommendation_sets`, `historical_namespace_recommendation_sets`).

**Files to modify:**
- `internal/model/recommendation_set.go` — add delete method for stale term data
- `internal/model/namespace_recommendation_set.go` — same for namespace recommendations

### 5.5 Set `org_id` as Kafka Partition Key

**Current code** (`internal/kafka/producer.go`) uses `experiment_name` as the key for `rosocp.kruize.recommendations`. No change needed there.

**No ros-ocp-backend change needed** — the `org_id` partition key change is in koku's `ros_report_shipper.py` (Section 6).

### 5.6 Handle Settings API Unavailability

**Graceful fallback:** If Koku Settings API returns 404, 500, connection timeout, or any error during `createExperiment`, use default term configuration (1d, 7d, 15d, no business hours). Log the failure at WARN level.

---

## 6. Koku Backend

**Repository:** `~/dev/koku/koku/`
**Branch:** `main`

### 6.1 New Settings API Endpoint

**Path:** `GET/PUT /api/cost-management/v1/account-settings/ros-custom-timeframes/`

**Following the existing pattern** in `docs/architecture/api-settings-endpoints.md`:
- GET returns current settings (or defaults if not configured).
- PUT updates settings. Returns `204 No Content`.
- RBAC: `SettingsAccessPermission` (Cost Management Administrators).
- Caching: `@never_cache` decorator.

**GET Response:**
```json
{
  "meta": { "count": 1 },
  "data": [
    {
      "terms": [
        { "name": "term1", "duration_days": 1 },
        { "name": "term2", "duration_days": 7 },
        { "name": "term3", "duration_days": 15 }
      ],
      "business_hours": {
        "enabled": false,
        "start_time": "09:00",
        "end_time": "17:00",
        "weekdays": [1, 2, 3, 4, 5],
        "timezone": "UTC"
      }
    }
  ]
}
```

**PUT Request Body:**
```json
{
  "terms": [
    { "name": "term1", "duration_days": 3 },
    { "name": "term2", "duration_days": 20 },
    { "name": "term3", "duration_days": 60 }
  ],
  "business_hours": {
    "enabled": true,
    "start_time": "09:00",
    "end_time": "17:00",
    "weekdays": [1, 2, 3, 4, 5],
    "timezone": "America/New_York"
  }
}
```

**Storage:** New `UserSettings` entry (or new model) in the tenant schema. The existing account-settings pattern uses `UserSettings` with key-value storage.

**Files to create/modify:**
- `koku/api/settings/ros_custom_timeframes.py` — new serializer and view
- `koku/api/settings/view.py` — register new setting
- `koku/api/urls.py` — add URL pattern
- `koku/api/settings/serializers.py` — validation logic (single source of truth)

### 6.2 Embed Settings in Kafka Messages

**File:** `koku/masu/external/ros_report_shipper.py`

**Current `build_ros_msg()` method** (line 139-148):
```python
ros_json = {
    "request_id": self.request_id,
    "b64_identity": self.b64_identity,
    "metadata": self.metadata | {"cluster_alias": self.cluster_alias},
    "files": presigned_urls,
    "object_keys": upload_keys,
}
```

**Change:** Add `custom_timeframes` to the metadata. Query the tenant's ROS custom timeframes settings before building the message.

```python
ros_json = {
    "request_id": self.request_id,
    "b64_identity": self.b64_identity,
    "metadata": self.metadata | {
        "cluster_alias": self.cluster_alias,
        "custom_timeframes": self._get_ros_custom_timeframes(),  # NEW
    },
    "files": presigned_urls,
    "object_keys": upload_keys,
}
```

### 6.3 Set `org_id` as Kafka Partition Key

**File:** `koku/masu/external/ros_report_shipper.py`

**Current `send_kafka_message()` method** (line 132-137):
```python
def send_kafka_message(self, msg):
    producer = get_producer()
    producer.produce(ROS_TOPIC, value=msg, callback=delivery_callback)
    producer.poll(0)
```

**Change:** Add `key` parameter with `org_id`:
```python
def send_kafka_message(self, msg):
    producer = get_producer()
    org_id = self.metadata.get("org_id", "")
    producer.produce(ROS_TOPIC, value=msg, key=org_id.encode("utf-8"), callback=delivery_callback)
    producer.poll(0)
```

This ensures all messages for the same tenant go to the same Kafka partition, preventing race conditions when ros-ocp-backend processes settings changes.

---

## 7. koku-ui Frontend

**Repository:** `~/dev/koku/koku-ui/`
**Branch:** `main`

### 7.1 Settings Page — Resource Optimization Tab

**Location:** New tab in the existing Settings page.

**PRD requirement:** *"Add a new 'Resource optimization' tab in the Settings page."*

**UI Components:**
1. **Term Duration Section:**
   - Three rows: Term 1, Term 2, Term 3.
   - Each row has a numeric input (days) with min=1, max=90.
   - Term 2 is disabled until Term 1 is configured.
   - Term 3 is disabled until Term 2 is configured.
   - Terms must be ordered shorter to longer (validated on submit).
   - Default values shown as placeholders: 1, 7, 15.
   - "Reset to defaults" button.

2. **Business Hours Section:**
   - Toggle: "Restrict analysis to business hours" (default: off).
   - When enabled, shows:
     - PatternFly 24-hour time picker for start time (default: 09:00).
     - PatternFly 24-hour time picker for end time (default: 17:00).
     - Weekday picker (checkboxes for Mon-Sun, default: Mon-Fri).
     - Timezone selector (IANA timezone list, default: UTC).

3. **Save button** calls `PUT /api/cost-management/v1/account-settings/ros-custom-timeframes/`.

**Files to create/modify:**
- `apps/koku-ui-hccm/src/routes/settings/rosCustomTimeframes/` — new component directory
- `apps/koku-ui-hccm/src/api/settings.ts` — add API methods for the new endpoint
- `apps/koku-ui-hccm/src/routes/settings/` — add tab to Settings page

### 7.2 Dynamic Labels in Recommendation Views

**Current behavior:** The UI displays "Short term", "Medium term", "Long term" as fixed labels.

**New behavior:** Display the actual duration from `duration_in_hours` field (e.g., "3 days", "20 days", "60 days").

**Files to modify:**
- `apps/koku-ui-ros/src/utils/commonTypes.ts` — `Interval` enum remains, add duration display helper
- `apps/koku-ui-ros/src/utils/recomendations.ts` — `getRecommendationTerm` updated to extract `duration_in_hours`
- `apps/koku-ui-ros/src/api/ros/recommendations.ts` — `RecommendationTerms` interface unchanged (still `short_term`/`medium_term`/`long_term`)

### 7.3 On-Prem Considerations

The on-prem shell (`apps/koku-ui-onprem/`) loads HCCM as a federated module. The new Settings tab must be accessible in on-prem mode. No additional work needed if the Settings route is already federated.

---

## 8. API Contracts

### 8.1 Koku Settings API

```
GET  /api/cost-management/v1/account-settings/ros-custom-timeframes/
PUT  /api/cost-management/v1/account-settings/ros-custom-timeframes/
```

See [Section 6.1](#61-new-settings-api-endpoint) for request/response schemas.

### 8.2 Kruize updateExperiment API

```
POST /updateExperiment
```

See [Section 4.1](#41-new-updateexperiment-api) for request schema.

### 8.3 ROSOCP Recommendations API (unchanged path, updated response)

```
GET /api/cost-management/v1/recommendations/openshift/
```

**Response keys unchanged:** `short_term`, `medium_term`, `long_term`.

**New dynamic field:** Each term's `duration_in_hours` reflects the actual configured duration:
```json
{
  "recommendation_terms": {
    "short_term": {
      "duration_in_hours": 72.0,
      "monitoring_start_time": "...",
      "recommendation_engines": { ... }
    },
    "medium_term": {
      "duration_in_hours": 480.0,
      ...
    }
  }
}
```

### 8.4 Kafka Message Schema (hccm.ros.events — updated)

```json
{
  "request_id": "uuid",
  "b64_identity": "base64-encoded-identity",
  "metadata": {
    "account": "10001",
    "org_id": "1234567",
    "source_id": "1",
    "provider_uuid": "uuid",
    "cluster_uuid": "uuid",
    "cluster_alias": "my-cluster",
    "operator_version": "3.4.0",
    "custom_timeframes": {
      "terms": [
        { "name": "term1", "duration_days": 3 },
        { "name": "term2", "duration_days": 20 },
        { "name": "term3", "duration_days": 60 }
      ],
      "business_hours": {
        "enabled": true,
        "start_time": "09:00",
        "end_time": "17:00",
        "weekdays": [1, 2, 3, 4, 5],
        "timezone": "America/New_York"
      }
    }
  },
  "files": ["https://..."],
  "object_keys": ["path/to/file"]
}
```

**Kafka message key:** `org_id` (encoded as UTF-8 bytes).

---

## 9. Integration Points

| # | From | To | Mechanism | Auth | Error Handling |
|---|---|---|---|---|---|
| 1 | koku-ui | Koku Settings API | REST PUT/GET | x-rh-identity + RBAC | Standard HTTP error codes |
| 2 | Koku masu | ros-ocp-backend | Kafka `hccm.ros.events` | N/A (message bus) | org_id as partition key |
| 3 | ros-ocp-backend | Koku Settings API | REST GET | B64_identity (SaaS) / none (on-prem) | 404/error → use defaults |
| 4 | ros-ocp-backend | Kruize createExperiment | REST POST | Internal HTTP | Existing error handling |
| 5 | ros-ocp-backend | Kruize updateExperiment | REST POST | Internal HTTP | Log error, retry on next cycle |
| 6 | ros-ocp-backend | Kruize listRecommendations | REST GET | Internal HTTP | Existing error handling |
| 7 | koku-ui | ros-ocp-backend API | REST GET | x-rh-identity | Existing error handling |

---

## 10. Validation Rules

**Koku Settings API is the single source of truth** for all validation. Kruize trusts values received from ros-ocp-backend.

### Term Durations

| Rule | Constraint |
|---|---|
| Minimum duration | 1 day |
| Maximum duration | 90 days |
| Unit | Whole days only (integer) |
| Count | 1 to 3 terms |
| Ordering | term1 < term2 < term3 (shorter to longer) |
| Sequential | Cannot define term2 without term1, or term3 without term2 |
| No duplicates | All durations must be distinct |

### Business Hours

| Rule | Constraint |
|---|---|
| Start/end time | `HH:MM` format, 24-hour clock, 15-minute granularity |
| Timezone | Valid IANA timezone string (e.g., `America/New_York`) |
| Weekdays | Array of ISO 8601 day numbers (1=Monday through 7=Sunday), at least 1 |
| Minimum window | At least 1 hour of business time per business day |
| Midnight spanning | Allowed (e.g., `start_time: "22:00"`, `end_time: "06:00"`) |

### Cross-Field Validation

| Rule | Constraint |
|---|---|
| Business hours without terms | Allowed (uses default terms with business hours filter) |
| Terms without business hours | Allowed (uses custom terms with full-day analysis) |

---

## 11. Known Limitations

### L1: Settings Propagation Delay (~6 hours in Phase 1)

After a customer changes settings, recommendations based on new settings are available after approximately 6 hours (with force re-poll). The delay consists of:
- Next Kafka message from koku containing the new settings (~variable, depends on upload cycle)
- ros-ocp-backend detects change and calls `updateExperiment` (~seconds)
- Force re-poll triggers `generateRecommendations` on next data upload (~up to 6 hours based on operator upload cycle)

**Phase 2 improvement:** Webhook/push mechanism from koku to ros-ocp-backend for immediate notification.

### L2: DST Edge Cases in Business Hours

When a DST transition occurs mid-window, Kruize handles per-interval conversion using `ZonedDateTime`. Edge cases:
- A 15-minute interval spanning a DST transition is evaluated based on the interval's start time in the local timezone.
- "Spring forward" gaps (e.g., 2:00 AM doesn't exist) are handled by Java's `ZoneId` rules — the interval is classified based on the adjusted time.

Full timezone support (per-cluster timezone, timezone-aware recommendations) is planned as a future feature.

### L3: Per-Tenant Only

Settings apply to all clusters and namespaces for the tenant. Per-cluster or per-namespace customization is not supported in Phase 1.

### L4: One-by-One Experiment Updates

When settings change, ros-ocp-backend iterates over all experiments for the org_id and calls `updateExperiment` for each. For large tenants with thousands of workloads, this may take time. Bulk `updateExperiment` is planned as a future optimization.

### L5: JVM Heap Sizing for 91-Day Retention

With retention raised to 91 days, Kruize's in-memory data structures for `generateRecommendations` will hold ~5.7x more datapoints. Monitor JVM heap usage after deployment. Recommended: set `-Xmx` based on cluster size and experiment count. For large deployments (>1000 experiments), consider 4GB+ heap.

### L6: Stale Recommendations During Settings Transition

After settings change, old recommendations (based on previous settings) remain visible until replaced by new recommendations on the next poll cycle. The UI does not show a "recalculating" indicator. See [Section 14, Deviation 1](#deviation-1-replace-vs-invalidate).

### L7: Weekday Semantics Tied to Timezone

"Monday" in `America/New_York` is a different UTC time range than "Monday" in `Asia/Tokyo`. Kruize converts each datapoint to the configured timezone before checking the weekday.

---

## 12. Deployment Plan

### Deploy Order

```
1. Koku (new Settings API endpoint)
   └── Can be deployed independently; no breaking changes
2. Kruize (updateExperiment API, retention, business hours, term fixes)
   └── Requires Kruize image rebuild from remote_monitoring branch
3. ros-ocp-backend (Settings API integration, updateExperiment calls, force re-poll)
   └── Depends on both Koku Settings API and Kruize updateExperiment being available
4. koku-ui (Settings page, dynamic labels)
   └── Depends on Koku Settings API being available
```

### Rollback Plan

1. **Remove custom settings via API** → experiments revert to defaults on next `updateExperiment` cycle.
2. **Disable at ros-ocp-backend** → feature flag or config toggle to skip Settings API query and always use defaults.
3. **No data loss** — Kruize retains all historical data; ros-ocp-backend DB recommendations are replaced, not deleted.

### On-Prem Deployment

The on-prem Helm chart (`cost-onprem-chart`) must be updated to:
- Include the new Kruize image (built from `remote_monitoring` branch with custom timeframes support).
- Expose the `KOKU_API_BASE_URL` environment variable for ros-ocp-backend.
- No Kafka configuration changes needed (on-prem uses the same Kafka topics).

---

## 13. Testing Strategy

### Kruize

- Unit tests for `updateExperiment` API (valid/invalid payloads, experiment not found).
- Unit tests for business hours filter (UTC→local conversion, DST transitions, midnight spanning, weekday filtering).
- Unit tests for custom term durations (1d, 30d, 90d).
- Integration test: `createExperiment` → `updateResults` → `updateExperiment` (change terms) → `generateRecommendations` → verify recommendations use new terms.
- Performance test: `generateRecommendations` with 91 days of data.

### ros-ocp-backend

- Unit tests for Settings API client (success, 404 fallback, timeout fallback).
- Unit tests for settings change detection from Kafka messages.
- Unit tests for `updateExperiment` call construction.
- Unit tests for stale recommendation cleanup.
- Unit tests for force re-poll flag in recommendation poller.
- Integration test: end-to-end settings change → experiment update → new recommendations.

### Koku Backend

- Unit tests for new Settings serializer (all validation rules from Section 10).
- Unit tests for Kafka message embedding of custom_timeframes.
- Unit tests for `org_id` partition key in `ros_report_shipper.py`.
- API tests: GET/PUT settings with proper RBAC (admin allowed, non-admin forbidden).

### koku-ui

- Component tests for Settings page (term inputs, business hours toggle, timezone selector, weekday picker).
- Component tests for dynamic duration labels in recommendation views.
- E2E test: configure custom timeframes → verify API call → verify settings persisted.

---

## 14. PRD Deviations

Two conscious deviations from the PRD were accepted by the product owner.

### Deviation 1: Replace vs Invalidate

**PRD says:** *"Invalidate the calculated recommendations and recalculate them."*

**Implementation:** Old recommendations remain visible until replaced by new recommendations on the next poll cycle. No "recalculating" state is shown.

**Rationale:** Avoids complex state management across three services. The delay is bounded (~6 hours with force re-poll). Accepted as pragmatic for Phase 1.

### Deviation 2: No Selective Per-Term Invalidation

**PRD says:** *"Only invalidate the data for the recommendations of the changed timeframe."*

**Implementation:** Kruize regenerates all terms' recommendations on `generateRecommendations`, regardless of which term changed.

**Rationale:** Recommendation generation is a lightweight in-memory percentile calculation. Regenerating all 3 terms costs negligible additional time compared to regenerating 1. Unchanged terms produce identical results. Accepted as an implementation simplification with no user-visible impact.

---

## Appendix A: Term Name Mapping

| User-Facing (Settings UI) | Koku Settings API | Kafka Message | ros-ocp-backend Internal | Kruize Internal | ROSOCP API Response |
|---|---|---|---|---|---|
| Term 1 | `term1` | `term1` | `short_term` | `short_term` | `short_term` |
| Term 2 | `term2` | `term2` | `medium_term` | `medium_term` | `medium_term` |
| Term 3 | `term3` | `term3` | `long_term` | `long_term` | `long_term` |

## Appendix B: Existing Code References

| Component | File | What It Does | Relevance |
|---|---|---|---|
| Kruize | `AnalyzerConstants.java` | `DELETE_PARTITION_THRESHOLD_IN_DAYS = 16` | Must change to 91 |
| Kruize | `RecommendationConstants.java` | Percentile constants (60th cost CPU, 98th perf CPU, 100th memory) | Unchanged, but affected by business hours filter |
| Kruize | `Terms.java` | `setDurationBasedOnTerm()`, `getMaxDuration()` hardcode durations | Must fix to read from `Terms.days` |
| Kruize | `KruizeObject.java` | `setCustomTerms()` restricts to short/medium/long names | Must accept custom durations |
| Kruize | `GenericRecommendationModel.java` | `getCPURequestRecommendation()`, `getMemoryRequestRecommendation()` | Business hours filter applied before these |
| ros-ocp-backend | `createExperiment.go` | Hardcoded `Measurement_duration: "15min"`, `Threshold: "0.1"` | Must extend with term_settings, business_hours |
| ros-ocp-backend | `report_processor.go` | `ProcessReport()` calls `Create_kruize_experiments()` | Must query Settings API before creating |
| ros-ocp-backend | `recommendation_poller.go` | `PollForRecommendations()` checks `RecommendationPollIntervalHours` | Must honor force re-poll flag |
| ros-ocp-backend | `api/utils.go` | Iterates `short_term`, `medium_term`, `long_term` | No change needed (keys unchanged) |
| ros-ocp-backend | `kafkaMsg.go` | `KafkaMsg` struct with `B64_identity` field | B64_identity used for Settings API auth |
| Koku | `ros_report_shipper.py` | `send_kafka_message()` — no partition key | Must add `org_id` as key |
| Koku | `ros_report_shipper.py` | `build_ros_msg()` — builds Kafka message | Must add `custom_timeframes` to metadata |
| koku-ui | `recommendations.ts` | `RecommendationTerms` interface | Unchanged (still short/medium/long) |
| koku-ui | `commonTypes.ts` | `Interval` enum: `short_term`, `medium_term`, `long_term` | Unchanged |
| koku-ui | `recomendations.ts` | `getRecommendationTerm()` | Must use `duration_in_hours` for display |

## Appendix C: Kruize Recommendation Algorithm Summary

For reference, the recommendation algorithm that custom timeframes modify:

| Metric | Cost Recommendation | Performance Recommendation |
|---|---|---|
| CPU Request | 60th percentile of usage | 98th percentile of usage |
| CPU Limit | max(request, P98 usage + 15% buffer) | max(request, P100 usage + 15% buffer) |
| Memory Request | P100 usage + spike detection + 15% buffer | P100 usage + spike detection + 15% buffer |
| Memory Limit | P100 usage + spike detection + 15% buffer | P100 usage + spike detection + 15% buffer |

**Spike detection:** If max memory > P100 by >10%, the spike value is used instead.

**Minimum data thresholds** (percentage of term duration that must have datapoints):
- Short term: 53%
- Medium term: 53%
- Long term: 70%

With business hours, these thresholds scale linearly: threshold × (business_hours_per_week / 168).
