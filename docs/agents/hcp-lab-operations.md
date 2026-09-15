# HCP Lab Operations (hpe-apollo-cn99xx-16)

Internal agent guidance for the HyperShift lab: management `hcp-mgmt`
(4.22.12 compact ARM64) + hosted `hc01` (4.22.12, Agent, SingleReplica).
Full build record: `docs/superpowers/plans/2026-09-14-hcp-lab-install.md`.
Live runbook (IPs, passwords, URLs): `~/rh/kcli/hc01-hpe-apollo-cn99xx-16/ACCESS.md`.
Epic tracker: [#384](https://github.com/pgarciaq/ros-ocp-backend/issues/384).

Scope rule: this file holds *mechanism gotchas*. Program status lives in
`docs/plans/hcp-fleet-optimization.md`; cluster facts live in ACCESS.md.
Do not duplicate either here — link to them.

## Deploying the operator (koku-metrics-operator)

- Kustomize `namePrefix: koku-metrics-` renames RBAC at deploy: the live
  ClusterRole is `koku-metrics-manager-role`. Applying `config/rbac/role.yaml`
  verbatim creates a **dead duplicate** — everything looks deployed while new
  rules silently never bind. After any RBAC change, prove it with
  `oc auth can-i --as=system:serviceaccount:koku-metrics-operator:<sa>`.
- `volume-shell` helper pods are bare Pods: any node reboot deletes them
  permanently. Recreate as needed; ubi-minimal needs `microdnf install tar`
  for `oc cp` to work.
- local-path-provisioner on RHCOS needs **both**: `chcon -Rt container_file_t
  /opt/local-path-provisioner` on every node (else helper-pod `mkdir:
  Permission denied`, PVCs Pending forever) **and** privileged SCC for the
  provisioner SA. Symptom of skipping: assisted-service never starts.

## Registry (quay.io)

- Robot: `pgarciaq+robneghaction`, push on `quay.io/pgarciaq/koku-metrics-operator`
  (private). Token lives ONLY in root's podman login on the hypervisor —
  never chat, issues, or repo files. If pushes 401, check repo existence +
  robot Write grant first (the usual cause, not credentials).
- Tags are unique per build (`w0.1-<UTC date>`); never reuse (same
  `IfNotPresent` staleness discipline as backend images).
- Native arm64 `podman build` on Apollo works (Dockerfile is TARGETARCH);
  verified `arm64/linux`, ~53 MB.

## DNS (explicit records everywhere — no wildcards exist)

- kcli dnsmasq carries explicit `dns-host` records only. Box resolves via
  `/etc/hosts` (its resolv.conf is corporate DNS). In-cluster pods resolve via
  CoreDNS → node → dnsmasq. All three layers need the same entries.
- `virsh net-update`: one `<host>` block per IP; duplicate hostnames across
  blocks are rejected — keep a single block per IP, delete-then-add on change.
- Current split: API + `api-int` + assisted trio → keepalived VIP
  (.253); hosted apps (console/oauth/canary) → worker-0 IP (HostNetwork
  router, NOT NodePort — DHCP, re-check on rebuild).

## HyperShift specifics

- `hcp create cluster agent` defaults NodePool `arch: amd64` — pass
  `--arch arm64` explicitly; verify in `--render` output before applying.
- `--render` omits generated secrets: create `hc01-pull-secret`,
  `hc01-ssh-key`, `hc01-etcd-encryption-key` (etcd key = base64 of 32 random
  bytes per `AESCBCKeySecretKey = "key"`) or reconcile fails one secret at a
  time. InfraEnv needs `cpuArchitecture: arm64` (default is x86_64).
- The hosted control-plane namespace here is `hc01-infra-hc01`, NOT the old
  `clusters-*` convention. Hosted Infrastructure carries
  `hypershift.openshift.io/managed=true` — managed alone does NOT mean
  management (see #407 design).
- Discovery ISO from the image-service is a ~99 MB minimal ISO; a ~2.5 K
  download is an HTML error page (route TLS needs `-k`). VMs booted from a
  bad ISO need `destroy` + `start` (reboot may not re-read it); diagnose
  headlessly with `virsh screenshot <vm> /tmp/x.ppm`.

## MachineConfig / NTP

- Ignition `contents.source` data URLs **must be quoted** — unquoted, the
  comma ends the flow-mapping value, payload drops silently, and the pool sits
  `RenderDegraded` for hours with nodes unaffected. Always verify server-side.
- MCO drains hang on SingleReplica HCP PDBs (minAvailable 1, 0 allowed);
 FailedToDrain parks with no retry, PDBs recreate in ~30s (owned by
  HostedControlPlane/CPO — pausing the HostedCluster does NOT stop it).
  Recovery that worked: pause HC → scale CPO to 0 → delete PDBs → verify
  hold → bounce machine-config-controller → reboot → rejoin → scale CPO up →
  unpause. Expect a hosted-API outage during the node reboot. Full story on
  [#403](https://github.com/pgarciaq/ros-ocp-backend/issues/403#issuecomment-5669790250).

## Long-run monitoring from a laptop

- Wrap box-side long runs: `tmux new -d -s x sh -c '<cmd> > log 2>&1'`,
  then poll the log file — bare `tmux new -d` leaves no session when the
  command exits fast, and `pgrep -f` patterns self-match the monitor shell.
- kcli wrapper output is not ground truth (`oc get clusterversion` is); kcli
  99 syntax is `create/delete kube`, `ctlplanes` (not masters), `numcpus`,
  `version=stable` + `tag=4.22` (bare `4.22` is rejected).
