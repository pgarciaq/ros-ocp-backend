# SNO Cluster Operations

Lab-specific cluster procedures (profiling, deploy, E2E against a live cluster).
Do not copy hypervisor hostnames or kubeadmin passwords into `AGENTS.md`.

## Pre-Flight Checklist (ALWAYS do these first)

Before ANY cluster interaction (profiling, deployment, log inspection):

1. **Verify VPN is connected** — the cluster is on Red Hat internal network
2. **Verify sshuttle is active** — routes 192.168.122.0/24 through the hypervisor
   ```bash
   # Check if sshuttle is running
   pgrep -f sshuttle || echo "NOT RUNNING — start it first"
   # Start it
   sshuttle -r root@HYPERVISOR.bkr.lab.eng.rdu2.dc.redhat.com 192.168.122.0/24
   ```
3. **Test connectivity** — do NOT proceed until this works:
   ```bash
   oc login -s https://api.CLUSTERNAME.karmalabs.corp:6443 -u kubeadmin \
     --password='YOURPASSWORD' --insecure-skip-tls-verify=true    # notsecret
   oc whoami  # must return "kubeadmin"
   ```

**If connectivity fails:** STOP. Ask the user to connect VPN and start sshuttle.
Do NOT attempt workarounds or retries — it's a network-level block.

## Do not tail logs

Never `kubectl logs --follow`, `oc logs -f`, or `tail -f` unless the user explicitly
asks. Those commands block. Use `--tail=50` or grep recent logs instead.

## Rebuild before cluster E2E

Never run cluster E2E against images that may be stale:

0. Check architecture (`uname -m` and node `architecture`). SNO ≠ aarch64.
1. Build with a **unique** tag (`imagePullPolicy: IfNotPresent` ignores tag reuse).
2. Push, `oc set image`, wait for rollout.
3. Apply pending migrations.
4. Then run tests.

Go unit tests for this repo are `make test`. Chart pytest (`run-pytest.sh`) is not
in this repository — see `docs/testing/validating-native-engine.md` until that
harness is replaced.

## ROS API Authentication

The ROS API requires `x-rh-identity` header (base64 JSON) with entitlements.
This is required **even with RBAC_ENABLE=false** because the identity middleware
is always active.

### Forged identity (preferred for testing/profiling)

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"admin","email":"admin@example.com","is_org_admin":true}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

curl -s -H "x-rh-identity: $IDENTITY" http://localhost:8080/api/cost-management/v1/recommendations/openshift
```

**Do NOT** attempt Bearer token auth unless specifically needed — it requires:
1. Keycloak admin token from master realm
2. Fetching the `cost-management-ui` client secret (it's confidential, not public)
3. User password in `kubernetes` realm (check ACCESS.md for current credentials)
4. And you STILL need `x-rh-identity` on top of the Bearer token

The forged identity skips all of this.

## Profiling Workflow

### Enabling pprof

```bash
# Set env var on the processor deployment
oc set env deployment/cost-onprem-ros-processor ROS_ENABLE_PPROF=true -n cost-onprem
oc rollout status deployment/cost-onprem-ros-processor -n cost-onprem --timeout=120s

# Port-forward the metrics port (pprof lives on the Prometheus/metrics port, default 9000)
oc port-forward -n cost-onprem deploy/cost-onprem-ros-processor 6060:9000 &
```

### Capturing profiles

```bash
# CPU (30s)
curl -s http://localhost:6060/debug/pprof/profile?seconds=30 -o cpu.prof

# Heap (point-in-time)
curl -s http://localhost:6060/debug/pprof/heap -o heap.prof

# Allocs (cumulative since start)
curl -s http://localhost:6060/debug/pprof/allocs -o allocs.prof

# Goroutines
curl -s http://localhost:6060/debug/pprof/goroutine -o goroutine.prof
```

### Generating API load (while capturing CPU profile)

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"admin","email":"admin@example.com","is_org_admin":true}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

# Port-forward the API
oc port-forward -n cost-onprem deploy/cost-onprem-ros-api 8080:8080 &

# Concurrent load
for stream in $(seq 1 5); do
  (for i in $(seq 1 60); do
    curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8080/api/cost-management/v1/recommendations/openshift" > /dev/null
    curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8080/api/cost-management/v1/recommendations/openshift/namespaces" > /dev/null
  done) &
done
wait
```

### CRITICAL: Disable pprof after profiling

```bash
oc set env deployment/cost-onprem-ros-processor ROS_ENABLE_PPROF=false -n cost-onprem
# Or remove the env var entirely:
oc set env deployment/cost-onprem-ros-processor ROS_ENABLE_PPROF- -n cost-onprem
```

## Pipeline Metrics (for time distribution analysis)

The processor exposes Prometheus histograms for every pipeline phase:

```bash
# Get pipeline phase breakdown
oc exec -n cost-onprem deploy/cost-onprem-ros-processor -- \
  curl -s http://localhost:9000/metrics | grep "rosocp_pipeline_phase_duration_seconds_sum"

# Get recommendation type breakdown
oc exec -n cost-onprem deploy/cost-onprem-ros-processor -- \
  curl -s http://localhost:9000/metrics | grep "rosocp_recommendation_duration_seconds_sum"

# Get DB query operation breakdown
oc exec -n cost-onprem deploy/cost-onprem-ros-processor -- \
  curl -s http://localhost:9000/metrics | grep "rosocp_db_query_duration_seconds_sum"
```

Pipeline phases: `download`, `parse_digest`, `write_digests`, `recommend`,
`write_recommendations`, `post_process`, `metadata_refresh`.

## Common Pitfalls (learned from experience)

| Pitfall | Wasted Time | Prevention |
|---------|-------------|------------|
| No VPN/sshuttle | 15 min | Pre-flight checklist above |
| Using Bearer token instead of x-rh-identity | 25 min | Always use forged identity |
| Keycloak client is confidential (not public) | 10 min | Skip Keycloak, use forged header |
| Wrong kubeadmin password from stale context | 5 min | Always read from ACCESS.md |
| Forgetting to disable pprof after session | security risk | Script cleanup as last step |
| Reusing an image tag | stale binary | Unique tags; see Image Build below |
| `kubectl logs --follow` | blocked session | `--tail=50` or grep |

## Image Build & Deploy

```bash
# Check architecture FIRST
uname -m  # must be x86_64 for this cluster
oc get nodes -o custom-columns=ARCH:.status.nodeInfo.architecture  # amd64

# Build with unique tag
TAG="feature-$(date -u +%Y%m%d%H%M)"
podman build -t ros-ocp-backend:$TAG -f Dockerfile .

# Push
REGISTRY="default-route-openshift-image-registry.apps.CLUSTERNAME.karmalabs.corp:443/cost-onprem"
TOKEN=$(oc create token image-pusher -n cost-onprem --duration=1h)
podman login $REGISTRY -u image-pusher -p "$TOKEN" --tls-verify=false
podman tag ros-ocp-backend:$TAG $REGISTRY/ros-ocp-backend:$TAG
podman push --tls-verify=false $REGISTRY/ros-ocp-backend:$TAG

# Deploy (processor and API share the same image)
INTERNAL="image-registry.openshift-image-registry.svc:5000/cost-onprem"
oc set image deployment/cost-onprem-ros-processor ros-processor=$INTERNAL/ros-ocp-backend:$TAG -n cost-onprem
oc set image deployment/cost-onprem-ros-api ros-api=$INTERNAL/ros-ocp-backend:$TAG -n cost-onprem
oc rollout status deployment/cost-onprem-ros-processor -n cost-onprem --timeout=120s
oc rollout status deployment/cost-onprem-ros-api -n cost-onprem --timeout=120s
```

## Access Details

Full cluster access information is in your local `ACCESS.md` file
(typically under `~/rh/kcli/<cluster-dir>/ACCESS.md`).

Always read this file for current credentials — passwords may be rotated.
