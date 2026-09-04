# AGENTS.md — ros-ocp-backend

Go API + processor for OpenShift rightsizing recommendations (ROS).
This is **not** koku, **not** koku-ui-ros, and **not** koku-metrics-operator.

`CLAUDE.md` is `@AGENTS.md` — do not maintain a second body.

If a Cursor stub and this file disagree, the **stricter** rule wins.
OpenCode and Codex do **not** auto-load `.cursor/rules` — follow [Read when](#read-when).

`docs/agents/` is internal agent guidance. It is not a docs-site pair.

## Must follow

**Auth.** ROS API tests use `x-rh-identity` (base64 JSON), not Keycloak Bearer tokens.

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"admin","email":"admin@example.com","is_org_admin":true}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)
curl -H "x-rh-identity: $IDENTITY" http://localhost:8080/api/...
```

Do not debug `unauthorized_client` or hunt client secrets for API calls.

**Source of truth — open the file; do not copy catalogs into this hub.**

| Need | Read |
|------|------|
| Which production plugins exist | `internal/plugins/plugins.go` (blank imports; example plugin is excluded on purpose) |
| HTTP routes | `internal/api` server wiring |
| Plugin design | `docs/architecture/plugin-architecture.md` |
| List/pagination helpers | `internal/api` listoptions |
| Business hours product contract | `docs-site/features/business-hours.md` and `docs/adr/` (ADR-0036). One line: dual **digests**, all-hours **persisted recs**, Peak hours is GET-time **detail** nest. Do not restate the matrix here. |
| Public docs contracts | `docs-site/` (not `README.md`, not registry error strings) |

**GitHub issues.** Never overwrite the issue body. Put lock/implementation notes in comments. Do not file grab-bag issues.

**Git.** Push to the fork (`github.com/pgarciaq/ros-ocp-backend`) whatever the remote is named — `origin` here is upstream and read-only. Never commit secrets.

**Images.** Unique tags (`feature-$(date -u +%Y%m%d%H%M)`). `imagePullPolicy: IfNotPresent` will keep a stale image if you reuse a tag. Check build-host and node arch (`uname -m`, `oc get nodes … architecture`) before `--platform`. SNO does not imply aarch64.

**CGO.** Processor/`rosocp` builds with `CGO_ENABLED=1` (`confluent-kafka-go`). `make robne` is the blessed `CGO_ENABLED=0` binary.

**Docs trees.** `docs-site/` = public facts; `docs/` = internal. Do not clobber one with the other. Class A pages need `> **Last verified:** YYYY-MM-DD`. Run `make docs-lint docs-drift`.

**Cluster.** If `oc login` / `oc whoami` fails, stop and ask the user for VPN/sshuttle — do not retry around it. Never `kubectl logs --follow`, `oc logs -f`, or `tail -f` unless the user asked. Rebuild, unique-tag, push, and wait for rollout **before** cluster E2E. Go tests for this repo are `make test`. Chart pytest (`run-pytest.sh`) is not in this repository.

**Metrics, not log timestamps.** Processor: `rosocp_pipeline_phase_duration_seconds`, `rosocp_recommendation_duration_seconds`, `rosocp_db_query_duration_seconds` on the metrics port.

## Verify before acting

* **Second source.** Cross-check every source-of-truth claim against one independent location.
* **Caller check.** No dead/buggy-helper claim without a usage grep.
* **Convention survey.** Before touching shared config (workflows, lint, Makefile), read how sibling files do it — e.g. workflow triggers are `main` + `pgarciaq-rosocp-superpowers-*`.
* **Mechanics over memory.** Verify tool/build behavior with a command (`go.mod ≠ linked`; `ListAPIOptions` is the pagination choke point).
* **Severity bar.** P2 needs demonstrated user impact; `security` needs confidentiality/integrity impact, not auth-path proximity.

## Commands

Versions: see `go.mod` and the Dockerfile. Do not pin copies here.

```bash
make test     # all tests (RACE=1 for the race detector)
make lint
make build    # rosocp, CGO_ENABLED=1
make robne    # CGO_ENABLED=0 CLI
make docs-lint docs-drift docs-sync-check
```

## Layout

```
cmd/                 # robne CLI
internal/api/        # HTTP handlers
internal/engine/     # recommendation math
internal/ingestion/  # CSV + digest upserts
internal/plugins/    # production plugin packages
internal/services/   # processor orchestration
librobne/            # CGO-free recommendation library
migrations/          # SQL migrations
docs-site/           # public MkDocs
docs/                # internal docs (includes docs/agents/)
```

## Read when

When the task matches, **read the file before acting**.

| When you… | Read |
|-----------|------|
| Ingest / `INSERT … ON CONFLICT` | [docs/agents/db-upsert-safety.md](docs/agents/db-upsert-safety.md) |
| Edit `docs/` or `docs-site/` | [docs/agents/docs-site-sync.md](docs/agents/docs-site-sync.md) |
| Live cluster / `oc` / image deploy / pprof | [docs/agents/sno-cluster-operations.md](docs/agents/sno-cluster-operations.md) |
| List SQL / indexes | [.cursor/skills/query-performance-review/SKILL.md](.cursor/skills/query-performance-review/SKILL.md) |
| Cluster-scale benchmarks | [.cursor/skills/scale-benchmark/SKILL.md](.cursor/skills/scale-benchmark/SKILL.md) |
| Humans / DCO / Last verified / phase bump | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Cluster E2E pytest (until the chart harness is replaced) | [docs/testing/validating-native-engine.md](docs/testing/validating-native-engine.md) |
| BH product contract | [docs-site/features/business-hours.md](docs-site/features/business-hours.md) |
| Design rationale | [docs/adr/](docs/adr/) |
