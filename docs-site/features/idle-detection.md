# Idle and zombie workload detection

Classify underutilized OpenShift containers as **active**, **idle**, or **zombie**, surface terminate guidance, and roll up recoverable waste at fleet scope.

Full design: [idle-detection.md](../../docs/features/idle-detection.md).

## Settings API

```bash
# Read merged settings (tenant DB + env locks)
curl -s -H "x-rh-identity: $IDENTITY" \
  "https://<ros-api>/api/cost-management/v1/recommendations/openshift/settings/idle-detection"

# Update utilization thresholds
curl -s -X PUT -H "x-rh-identity: $IDENTITY" -H "Content-Type: application/json" \
  -d '{"idle_detection":{"thresholds":{"cpu_utilization_percent":3,"memory_utilization_percent":4}}}' \
  "https://<ros-api>/api/cost-management/v1/recommendations/openshift/settings/idle-detection"

# Reset tenant overrides
curl -s -X DELETE -H "x-rh-identity: $IDENTITY" \
  "https://<ros-api>/api/cost-management/v1/recommendations/openshift/settings/idle-detection"
```

## List filters

```bash
# Zombies only
curl -s -H "x-rh-identity: $IDENTITY" \
  '.../recommendations/openshift?filter[idle_state]=zombie'

# Idle and zombie workloads
curl -s -H "x-rh-identity: $IDENTITY" \
  '.../recommendations/openshift?filter[idle_state]=zombie,idle'
```

Non-active rows include `idle_since`, `idle_duration_days`, `estimated_monthly_waste`, and `idle_recommendation` (`action`, `confidence`, `reason`). Active rows omit waste and recommendation fields.

## CSV export

`?format=csv` on the container list adds columns: `idle_state`, `idle_since`, `idle_duration_days`, `estimated_monthly_waste`, `estimated_monthly_waste_currency`.

## Fleet waste rollup

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  '.../recommendations/openshift/savings-summary?group_by[idle_state]=*'
```

Returns `data[]` rows with `idle_state`, `estimated_monthly_waste`, and `container_count`.
