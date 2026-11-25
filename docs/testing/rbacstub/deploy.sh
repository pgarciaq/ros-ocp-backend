#!/bin/bash
# Deploys the RBAC error-injection stub and the ros-api twin (#921).
#
# The stub: a ConfigMap-carried python service impersonating the insights-rbac
# v1 access endpoint with controllable failure modes (see rbacstub.py).
#
# The twin: GENERATED from the live cost-onprem-ros-api deployment, so it
# always carries the current image and env, with three deliberate deltas:
#   - RBACHOST points at the stub
#   - ROS_RBAC_CACHE_TTL=300 (unambiguous cache behavior within a test run)
#   - a distinct component label + its own Service
# Re-run this script to refresh the twin to the current ros-api image
# (the drift chore: nothing else needed).
#
# Usage (on the hypervisor or anywhere with oc + cluster-admin):
#   ./deploy.sh
# Then port-forward (hypervisor) and tunnel (laptop):
#   oc -n cost-onprem port-forward svc/cost-onprem-ros-api-rbacstub 18486:8000 &
#   oc -n cost-onprem port-forward svc/cost-onprem-rbacstub 18487:8080 &
#   # laptop: ssh -N -L 18486:localhost:18486 -L 18487:localhost:18487 <hypervisor>
#
# Teardown:
#   oc -n cost-onprem delete deploy/cost-onprem-ros-api-rbacstub \
#       svc/cost-onprem-ros-api-rbacstub deploy/cost-onprem-rbacstub \
#       svc/cost-onprem-rbacstub cm/cost-onprem-rbacstub-script

set -euo pipefail

NS="${NS:-cost-onprem}"
DIR="$(cd "$(dirname "$0")" && pwd)"

# --- 1. the stub -----------------------------------------------------------
oc -n "$NS" create configmap cost-onprem-rbacstub-script \
    --from-file=rbacstub.py="$DIR/rbacstub.py" \
    --dry-run=client -o yaml | oc apply -f -

oc apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cost-onprem-rbacstub
  namespace: $NS
  labels:
    app: cost-onprem-rbacstub
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cost-onprem-rbacstub
  template:
    metadata:
      labels:
        app: cost-onprem-rbacstub
    spec:
      containers:
        - name: stub
          image: registry.access.redhat.com/ubi9/python-311:latest
          command: ["python3", "/stub/rbacstub.py"]
          ports:
            - containerPort: 8080
              name: http
          volumeMounts:
            - name: script
              mountPath: /stub
          readinessProbe:
            httpGet:
              path: /__control/hits
              port: 8080
            initialDelaySeconds: 2
            periodSeconds: 5
      volumes:
        - name: script
          configMap:
            name: cost-onprem-rbacstub-script
---
apiVersion: v1
kind: Service
metadata:
  name: cost-onprem-rbacstub
  namespace: $NS
spec:
  selector:
    app: cost-onprem-rbacstub
  ports:
    - port: 8080
      targetPort: 8080
EOF

# --- 2. the twin, generated from the live deployment ------------------------
# The deployment selector is immutable: delete the previous twin before
# applying (a selector change — like the component-label fix — cannot apply
# over an existing deployment).
oc -n "$NS" delete deploy/cost-onprem-ros-api-rbacstub --ignore-not-found

# NetworkPolicy patches: the twin's distinct component label removes it from
# the label-based ingress rules that admit component=ros-api pods to rbac
# and masu — re-admit it by its rbacstub-twin label (idempotent: skipped
# when already present). Helm-owned policies: re-applied by this script on
# every run, since a helm upgrade may reset them.
for NP in cost-onprem-rbac-api-access cost-onprem-masu-access; do
  if ! oc -n "$NS" get networkpolicy "$NP" -o json | grep -q rbacstub-twin; then
    oc -n "$NS" patch networkpolicy "$NP" --type=json \
      -p '[{"op":"add","path":"/spec/ingress/0/from/-","value":{"podSelector":{"matchLabels":{"rbacstub-twin":"true"}}}}]'
  fi
done

TWIN_BASE="$(mktemp)"
trap 'rm -f "$TWIN_BASE"' EXIT
oc -n "$NS" get deploy cost-onprem-ros-api -o json > "$TWIN_BASE"
python3 - "$TWIN_BASE" <<'PY' | oc apply -f -
import json
import sys

d = json.load(open(sys.argv[1]))
meta = d["metadata"]
meta["name"] = "cost-onprem-ros-api-rbacstub"
for k in ("resourceVersion", "uid", "generation", "creationTimestamp", "managedFields"):
    meta.pop(k, None)
meta.pop("annotations", None)
d.pop("status", None)

# Distinct component label: the main cost-onprem-ros-api Service selects on
# the helm labels — a twin carrying them JOINS ITS ENDPOINTS and splits
# production traffic (found live 2026-11-25: the UI round-robined between
# the real API and the twin). The NetworkPolicies that admit rbac/masu
# access by component label are patched below to also admit the twin.
for labels in (d["spec"]["selector"]["matchLabels"], d["spec"]["template"]["metadata"]["labels"]):
    labels["app.kubernetes.io/component"] = "ros-api-rbacstub"
d["spec"]["template"]["metadata"]["labels"]["rbacstub-twin"] = "true"

env = d["spec"]["template"]["spec"]["containers"][0]["env"]
deltas = {
    "RBACHOST": "cost-onprem-rbacstub.cost-onprem.svc.cluster.local",
    "RBACPORT": "8080",
    "ROS_RBAC_CACHE_TTL": "300",
}
seen = set()
for e in env:
    if e["name"] in deltas:
        e["value"] = deltas[e["name"]]
        seen.add(e["name"])
for name, value in deltas.items():
    if name not in seen:
        env.append({"name": name, "value": value})

json.dump(d, sys.stdout)
PY

oc apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: cost-onprem-ros-api-rbacstub
  namespace: $NS
spec:
  selector:
    rbacstub-twin: "true"
  ports:
    - port: 8000
      targetPort: 8000
EOF

echo
echo "stub + twin applied. Wait for rollout:"
echo "  oc -n $NS rollout status deploy/cost-onprem-rbacstub --timeout=120s"
echo "  oc -n $NS rollout status deploy/cost-onprem-ros-api-rbacstub --timeout=180s"
echo "port-forwards (hypervisor):"
echo "  oc -n $NS port-forward svc/cost-onprem-ros-api-rbacstub 18486:8000 &"
echo "  oc -n $NS port-forward svc/cost-onprem-rbacstub 18487:8080 &"
