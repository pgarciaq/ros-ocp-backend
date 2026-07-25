# ADR-0327: API-time currency conversion over stored-currency duplication

## Status

Accepted

## Phase

Multi-currency savings (#364)

## Context

ROS stores savings in the cost model's native currency (the currency of the
`effective_rates` response from Koku). Users may configure a different preferred
display currency in Koku's `UserSettings`. When an org has clusters with different
cost model currencies (e.g., US clusters in USD, EU clusters in EUR), fleet-level
aggregation endpoints silently sum incompatible amounts.

Two approaches were considered:

### Option A: Store a `savings_currency` column alongside cents

Persist the currency at ingestion time. At API time, convert from the stored
currency to the user's preferred currency using exchange rates.

**Pros:** Exact historical currency known per row; straightforward queries.
**Cons:** Adds a column to every digest/recommendation table (migration), increases
storage, and the stored currency is already derivable from the cluster's cost model.

### Option B: Convert at API response time using the cluster's cost model currency

Do not add a `savings_currency` column. At API time, resolve the stored currency
from `effective_rates` (already cached), resolve the user's preferred currency,
fetch the exchange rate, and convert.

**Pros:** No migration, no storage increase, no schema change. Currency is resolved
dynamically, so changing a cost model's currency is immediately reflected.
**Cons:** Conversion adds HTTP calls (mitigated by caching). If Koku is unreachable,
amounts are returned in the stored currency (graceful degradation, not failure).

## Decision

**Option B — API-time conversion without a stored currency column.**

The stored currency is derivable from the cluster's cost model (already fetched for
rate enrichment). Adding a column across 10+ tables provides no new information and
complicates migrations. The API-time approach also handles cost model currency changes
without requiring data backfill.

## Conversion architecture

```
Request arrives (x-rh-identity → org_id)
        │
        ├─ resolveUserCurrency(org_id) → user_currency (cached 1h per org_id)
        │   └─ GET {KOKU_MASU_URL}/user_currency/?org_id=<org_id>
        │
        ├─ fetchClusterCurrency(org_id, cluster) → stored_currency (from effective_rates cache)
        │
        ├─ fetchExchangeRate(org_id, stored, user) → rate (cached 1h per org_id+pair)
        │   └─ GET {KOKU_MASU_URL}/exchange_rate/?schema=<schema>&from=<from>&to=<to>
        │
        └─ convertAndPatchAmount(amount, rate, display_currency)
            └─ cents * rate, round half-up (math.Floor(x + 0.5))
```

### Fallback

When exchange rates are unavailable (Koku unreachable, pair missing):
- `fetchExchangeRate` returns `1.0`
- `displayCurrency` falls back to `storedCurrency`
- Amounts are returned unconverted — no error, no data loss
- The response `meta.currency` accurately reflects what currency is shown

### Caching

| Cache | Key | TTL | Max entries |
|-------|-----|-----|-------------|
| User currency | `org_id` | 1 hour | 1000 |
| Exchange rate | `org_id:from:to` | 1 hour | 2000 |

TTLs are configurable via environment variables. The 1-hour default balances
freshness (exchange rates update daily) against HTTP call overhead.

## Consequences

- **No migration required** — all changes are in the API layer.
- **Koku dependency** — two new internal Masu endpoints must be deployed first.
  Graceful fallback ensures ROS functions without them (amounts in stored currency).
- **Fleet aggregation** — endpoints that aggregate across clusters with different
  stored currencies now convert to a single display currency before summing.
- **Rounding** — float64 exchange rates with round-half-up at the cent boundary.
  Matches Koku's rounding behavior. Sub-cent precision loss is acceptable for
  display-only conversion (not for financial settlement).
- **Cache invalidation** — when a user changes their preferred currency, it takes
  up to 1 hour for the change to propagate. Acceptable for a display preference.
