# Cost-Groups Platform-Namespace Sync

Koku pushes admin-curated Platform cost-group namespaces to ROS, so the
zombie idle leg can exclude admin-added platform projects the compiled
exclusion list does not know about. Transport mirrors tag sync
(`api`/push-primary, `db`-JOIN fallback); see
[tag-sync-auth.md](tag-sync-auth.md) for the TokenReview machinery.

| Item | Value |
|------|-------|
| Endpoint | `POST /api/cost-management/v1/internal/cost-groups/sync` |
| Auth | Same `validateInternalTagsAuth` (SA bearer → TokenReview, `ROS_TAGS_DEV_TOKEN` dev fallback) + `validateInternalOrgTarget` + audit log |
| Body | `{org_id, synced_at, entries: [{namespace, is_prefix, system_default}]}` — `%`-suffixed koku wildcards arrive as `is_prefix` |
| Response | `{"updated": N}` |
| Store | `hcp_platform_namespaces` (migration `000210`, unpartitioned; PK `(org_id, namespace)`) |

## Semantics

- **Replace per org** (`DELETE` + `INSERT` in one tx): admin removals must
  stop excluding. A removed project left behind would silence zombies on
  live namespaces — the unsafe direction.
- **Augment, never replace, at read**: the idle leg excludes a namespace
  when the compiled list OR the synced set says platform; unknown stays
  user activity (narrow-side failure preserved end to end).
- **Failure contract**: sync unreadable (never synced, table error) →
  compiled defaults, logged at warn; evaluation never errors on sync.
  No receiver-side TTL — the koku 6h beat safety net bounds staleness on
  the sender side.

## Ops

- Consumer: `loadSyncedPlatformSet` in `internal/engine/correlate/zombie.go`
  (both lanes: full-window and short).
- Freshness signal: POST `updated` counts in the audit log; a never-synced
  org reads as empty set (compiled-only) — expected, not an alert.
- Koku side (their repo): Celery push mirroring `sync_ros_ocp_tags`
  (per-tenant + 6h beat + SA bearer), hooked in the cost-groups PUT view.
