# Savings estimations

ROS converts right-sizing recommendations into **estimated monthly dollar impact** using cost model rates from Koku Masu `effective_rates`. Amounts are exposed as structured [`MoneyAmount`](../../internal/money/format.go) objects (`{"value": "12.34", "units": "USD"}`) with two decimal places.

## Where savings appear

| Plugin | List/detail field | Fleet rollup |
|--------|-------------------|--------------|
| Container | `estimated_monthly_savings` | `GET .../savings-summary` → `by_plugin.container` |
| Node | `estimated_monthly_savings` (per engine) | `by_plugin.node` |
| PVC | `estimated_monthly_savings` | `by_plugin.pvc` |
| VM | `savings` | `by_plugin.vm` |
| Snapshot | `estimated_monthly_cost` (holding cost) | `by_plugin.snapshot` |
| GPU MIG/idle | `estimated_monthly_gpu_savings` on container `gpu` block | Excluded — see below |
| GPU time-slicing | `estimated_monthly_timeslicing_savings` | Excluded |
| Quota / cluster-quota | `estimated_savings` | Excluded (avoid double-count with container savings) |

Fleet endpoints:

```
GET /api/cost-management/v1/recommendations/openshift/savings-summary
GET /api/cost-management/v1/recommendations/openshift/fleet-summary
```

`savings-summary` honors `?term=` (`short` / `medium` / `long`) and `?engine=` (`cost` / `performance`). PVC uses `term` only; snapshot totals are term-independent.

## Cost data source

Rates come from Koku only — there is no standalone ROS cost-rate API. Update the OCP cost model in Koku; rates refresh on the next ingestion cycle or via [savings recalculation](../../docs/architecture/cost-integration.md#savings-recalculation-after-cost-model-changes) when configured.

Requires `KOKU_MASU_URL` on ros-processor and ros-api, and `ROS_SAVINGS_ESTIMATES_ENABLED=true` (default).

## GPU savings

**MIG and idle GPU savings** are persisted at ingestion in `estimated_gpu_savings_cents` and refreshed when `container` savings are recalculated after a cost model change. API list/detail reads persisted values when present.

**Node GPU time-slicing savings** are computed at API read time on `GET .../gpu/timeslicing` because candidate selection is fleet-level and changes with scheduling.

GPU dollar amounts are **excluded** from `GET .../savings-summary` totals (`by_plugin.gpu` is always `0`). The response includes `gpu_savings_note` explaining why. Query container `gpu` blocks or the time-slicing endpoint for GPU dollar estimates.

## Negative savings

When a recommendation suggests **more** resources than currently requested, savings can be **negative**. That indicates an additional monthly cost to adopt the recommendation (reliability/performance upsizing), not a bug. UIs should label negative values as additional cost, not "Savings: -$X/month".

## Kill-switch

Set `ROS_SAVINGS_ESTIMATES_ENABLED=false` to skip Masu fetches. Container, node, and PVC savings become `$0`; VM `savings` is `null`; GPU dollar fields are omitted. Notification code **25** (`NotifNoCostData`) is appended on container, node, and PVC when cost data is unavailable.

## Currency

`currency` (ISO 4217) is returned on fleet summaries and `meta.currency` on list endpoints that expose monetary fields. Defaults to `USD` when Masu is unreachable.

## Internal recalculation

After Koku applies cost model rates, masu calls:

```
POST /api/cost-management/v1/internal/recalculate-savings
```

Supported `recommendation_types`: `container` (includes GPU MIG/idle persistence), `node`, `pvc`, `quota`, `cluster-quota`. VM and snapshot require a new ingestion cycle. Service-account bearer token required.

## Further reading

- [Cost integration](../architecture/cost-integration.md) — formulas, freshness, error handling
- [UI integration guide](../ui-integration-guide.md) — response shapes and dashboard guidance
- [Plugin reference](../plugin-reference/index.md) — per-plugin savings fields
