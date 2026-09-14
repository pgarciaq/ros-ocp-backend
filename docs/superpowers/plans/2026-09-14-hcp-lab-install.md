# HCP lab install plan (Apollo aarch64 hypervisor)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build one ARM64 OCP management cluster plus one hosted worker cluster on `hpe-apollo-cn99xx-16` as the lab for epic #384. No product code, no ROS operator in this plan.

**Architecture:** kcli VMs on the box's KVM. Compact 3-node OCP 4.22 management cluster; hosted cluster `hc01` on the Agent platform (assisted-service discovery ISO booted by 2 more kcli VMs); NodePort publishing; host-path storage for hosted etcd. Deliberately off-matrix on ARM64 control planes (no on-prem `ARM64|ARM64` row in the 4.22 HCP matrix) — lab only.

**Tech Stack:** kcli 99.0 (on box), OCP 4.22 aarch64, multicluster engine (HyperShift), assisted service, `hcp` CLI, local-path-provisioner.

**Spec:** epic [#384](https://github.com/pgarciaq/ros-ocp-backend/issues/384), `docs/plans/hcp-fleet-optimization.md`, ADRs 0328–0335. This plan builds the environment only.

## Global Constraints

- Hypervisor for every `ssh` step: `root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com` with `-o StrictHostKeyChecking=no`.
- OCP **4.22** for management and hosted (no version skew).
- Names (used verbatim in every task): mgmt cluster `hcp-mgmt`, hosted cluster `hc01`, infra namespace `hc01-infra`, base domain `hcplab.corp`.
- Libvirt net `default` = `192.168.122.0/24`, gateway `192.168.122.1`.
- Hosted control plane: `SingleReplica` (locked 2026-09-14; HA would triple apiserver/etcd — revisit only if a wedge needs it).
- StorageClass for hosted etcd: `local-path`, lab-only, no redundancy (stated, not hidden).
- VM budget (verified 2026-09-14 against 224 threads / 246 GiB free / 7.46 TiB pool):

| VMs | vCPU × RAM × disk each | Total |
|---|---|---|
| 3× `hcp-mgmt` compact nodes | 16 × 48 GiB × 200 GiB | 48 threads / 144 GiB |
| 2× `hc01` workers | 8 × 32 GiB × 120 GiB | 16 threads / 64 GiB |
| **Total** | | **64 threads / 208 GiB — ~160 threads / ~38 GiB headroom for hosted CP pods, MCE, monitoring** |

- Out of scope: Cost Management Metrics Operator install (later, separate work), ROS backend deployment, HA control planes.

---

### Task 1: Pre-flight and credentials

**Produces:** verified hypervisor state, pull secret + SSH keys on the box, chrony serving VMs, resolved aarch64 live ISO URL (consumed by Task 2).

- [ ] **Step 1: Re-verify hypervisor state**

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "systemctl is-active libvirtd; virsh net-list --all; kcli list pool; kcli version"
```

Expected: `active`, default net active, default pool active, kcli 99.0.

qemu write access was never proven (zero VMs so far) — idempotent, do it now.
restorecon is preventive: curl-downloaded ISOs may land with a context qemu can't read.

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "setfacl -m u:qemu:rwx /home/libvirt/images && restorecon -Rv /home/libvirt/images >/dev/null && virsh pool-info default | grep -E 'State|Available'"
```

Expected: pool `State: running` with ~7.4 TiB available.

- [ ] **Step 2: Confirm pull secret + SSH keys, copy what's missing** (run from laptop)

```bash
ls ~/rh/kcli/openshift_pull.json ~/rh/kcli/id_rsa ~/rh/kcli/id_rsa.pub
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "mkdir -p /root/.kcli && ls /root/.kcli/openshift_pull.json /root/.kcli/id_rsa 2>/dev/null || echo MISSING"
# If MISSING, copy (if your laptop paths differ, stop and report them instead of guessing):
scp ~/rh/kcli/openshift_pull.json root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com:/root/.kcli/openshift_pull.json
scp ~/rh/kcli/id_rsa root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com:/root/.kcli/id_rsa
scp ~/rh/kcli/id_rsa.pub root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com:/root/.kcli/id_rsa.pub
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "chmod 600 /root/.kcli/id_rsa"
```

- [ ] **Step 3: Hypervisor chrony serves the VM network**

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "grep -q '^allow 192.168.122' /etc/chrony.conf || echo 'allow 192.168.122.0/24' >> /etc/chrony.conf; systemctl restart chronyd; chronyc sources | head -3"
```

Expected: chronyd running with an upstream source.

- [ ] **Step 4: Resolve the 4.22 aarch64 RHCOS live ISO URL** (kcli defaults to x86_64 — this explicit URL is the ARM64 equivalent)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "curl -s https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.22/latest/ | grep -o 'rhcos-live-iso.aarch64.iso' | head -1"
```

Expected: prints `rhcos-live-iso.aarch64.iso`. Full URL for Task 2:
`https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.22/latest/rhcos-live-iso.aarch64.iso`
If the `4.22/latest` symlink doesn't exist yet, list `https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.22/` and use the newest versioned directory — same filename inside.

- [ ] **Step 5: Install the `oc` CLI on the box if missing** (every task from Task 2 on assumes it)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "which oc || kcli download oc; oc version --client"
```

Expected: prints a client version (aarch64 binary — it runs on the box, so arch matches by construction). `kcli download oc` drops the binary in CWD, not PATH — move it (`mv oc /usr/local/bin/oc`; observed client 4.22.13).

- [ ] **Step 6: Confirm UEFI firmware for aarch64 guests** (2-minute gate — if a VM refuses to boot, this is the first suspect)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "rpm -q edk2-aarch64 2>/dev/null || ls /usr/share/edk2/aarch64 2>/dev/null || echo NO_AAVMF"
```

Expected: package or directory present. If `NO_AAVMF`, install `edk2-aarch64` via dnf before Task 2 — do not proceed to VM creation without it.

- [ ] **Step 7: Install podman and tmux** (podman: required by kcli for ISO ignition embedding, and by Task 3 for the image-arch check; tmux: the long installs must survive laptop VPN blips)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "dnf install -y podman tmux; podman --version; tmux -V"
```

Expected: prints both versions.

### Task 2: Create the `hcp-mgmt` compact cluster

**Consumes:** Task 1 (live ISO URL, pull secret). **Produces:** healthy 3-node `hcp-mgmt` (`/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig` on the box).

Why compact 3-node: HCP needs ≥3 workers and SNO is explicitly unsupported as a management cluster; the docs allow a 3-node shared-infra cluster for low-QPS use, which is exactly this lab.

- [ ] **Step 1: Create the cluster** (45–90 min on ThunderX2 cores — slower than x86, do not interrupt; run under `tmux new -s hcp-install` on the hypervisor so a laptop VPN blip can't SIGHUP the install)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  kcli create kube openshift \
    -P cluster=hcp-mgmt \
    -P domain=hcplab.corp \
    -P version=stable \
    -P tag=4.22 \
    -P ctlplanes=3 \
    -P workers=0 \
    -P numcpus=16 \
    -P memory=49152 \
    -P disk_size=200 \
    -P keys=[/root/.kcli/id_rsa.pub] \
    -P liveiso_url=https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.22/latest/rhcos-live-iso.aarch64.iso \
    -P pull_secret=/root/.kcli/openshift_pull.json \
    hcp-mgmt"
```

(`workers=0` makes the 3 ctlplanes schedulable = compact. Param names verified 2026-09-14 against the installed kcli 99 sources: `kube` not `cluster`, `ctlplanes` not `masters`, `numcpus` not `cpus`. The `domain` override is required — the plan defaults to `karmalabs.corp`. `keys` accepts a path or an inline key. `version`/`tag` semantics source-verified against installed kcli 99: valid versions are only ci/candidate/latest/nightly/stable — a bare `4.22` is rejected with "Incorrect version"; `tag=4.22` selects the minor (`stable-4.22` release stream).)

- [ ] **Step 2: Verify management cluster healthy**

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc get clusterversion; oc get nodes; oc get co | grep -v 'True.*False.*False' || echo ALL_COS_HEALTHY"
```

Expected: version `4.22.x`, 3 Ready nodes (all `master,worker`), every CO `Available=True, Progressing=False, Degraded=False`. If any CO is degraded, stop — do not proceed to MCE on a sick cluster.

- [ ] **Step 3: Pin node NTP to the hypervisor** (compact nodes carry the `master` role)

MANDATORY YAML LESSON (bit us 2026-09-14): the ignition `source` URL **must be quoted**. Unquoted, the comma in `...;base64,<payload>` ends the flow-mapping value and the payload is silently dropped → stored source is the bare prefix → `RenderDegraded: parsing Ignition config spec v3 failed … unterminated parameter sequence`, MCP degraded for hours with nodes unaffected. Always verify server-side (`oc get mc … -o jsonpath`) before expecting rollout.

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  HIP=\$(ip -4 addr show virbr0 | grep -oP '(?<=inet )[\d.]+'); echo \"hypervisor libvirt IP: \$HIP\"
  CONF=\$(printf 'server %s iburst\ndriftfile /var/lib/chrony/drift\nmakestep 1.0 3\nrtcsync\nkeyfile /etc/chrony.keys\nleapsectz right/UTC\nlogdir /var/log/chrony\n' \"\$HIP\" | base64 -w0)
  oc apply -f - <<EOF
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 99-master-chrony
spec:
  config:
    ignition: {version: 3.2.0}
    storage:
      files:
      - contents: {source: data:text/plain;charset=utf-8;base64,\${CONF}}
        mode: 0644
        overwrite: true
        path: /etc/chrony.conf
EOF
  for i in \$(seq 1 30); do s=\$(oc get mcp master -o jsonpath='Updated={.status.conditions[?(@.type==\"Updated\")].status} Updating={.status.conditions[?(@.type==\"Updating\")].status}'); echo \"\$i: \$s\"; echo \"\$s\" | grep -q 'Updated=True Updating=False' && break; sleep 20; done"
```

Expected: MCP `Updated=True`, nodes reboot once (~5–10 min), then all Ready again.

- [ ] **Step 4: Laptop access** (repo runbook pattern — sshuttle + hosts)

API and ingress share one keepalived VIP: `192.168.122.253` (verified on the box 2026-09-14 — box dnsmasq resolves all mgmt names to it). Do NOT use per-node DHCP IPs; they are not the serving endpoint.

```bash
# Append to /etc/hosts on the laptop:
# 192.168.122.253  api.hcp-mgmt.hcplab.corp console-openshift-console.apps.hcp-mgmt.hcplab.corp oauth-openshift.apps.hcp-mgmt.hcplab.corp
sshuttle -r root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com 192.168.122.0/24
```

Verify — list the auth dir first (don't assume the password filename), then log in from the laptop:

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "ls /root/.kcli/clusters/hcp-mgmt/auth/"
# cat the password file listed there, then from the laptop:
oc login https://api.hcp-mgmt.hcplab.corp:6443 -u kubeadmin -p '<password>'
oc whoami  # → kubeadmin
```

Per repo rules: if this fails, stop and fix VPN/sshuttle — no workarounds.

### Task 3: Storage for hosted etcd (`local-path`, lab-only)

**Consumes:** healthy `hcp-mgmt`. **Produces:** default StorageClass `local-path` (consumed by Tasks 5 and 7).

`hcp create cluster agent` requires `--etcd-storage-class`. No spare bare disks exist on the box (all space is in LVM), so the cheap lab answer is a host-path provisioner.

- [ ] **Step 1: Verify an arm64 provisioner image exists, then deploy**

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  podman manifest inspect docker.io/rancher/local-path-provisioner:v0.0.31 2>/dev/null | grep -o '\"architecture\": *\"[^\"]*\"' | sort -u"
```

Expected: list includes `"arm64"` (verified 2026-09-14 on the box: amd64, arm, arm64, riscv64). If a future tag 404s or lacks arm64, pick the newest tag whose manifest lists arm64 and substitute it in both the inspect command and the manifest URL below.

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.31/deploy/local-path-storage.yaml
  oc -n local-path-storage rollout status deploy/local-path-provisioner --timeout=180s
  oc patch storageclass local-path -p '{\"metadata\":{\"annotations\":{\"storageclass.kubernetes.io/is-default-class\":\"true\"}}}'
  oc get sc"
```

Expected: StorageClass `local-path` present and marked default.

- [ ] **Step 2: Grant the provisioner the privileged SCC** (OCP default SCCs can block hostPath provisioners — without this, hosted-etcd PVCs may sit Pending and you'll debug storage instead of HCP)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  SA=\$(oc -n local-path-storage get deploy local-path-provisioner -o jsonpath='{.spec.template.spec.serviceAccountName}'); echo \"SA: \$SA\"
  oc adm policy add-scc-to-user privileged -z \$SA -n local-path-storage
  oc -n local-path-storage rollout status deploy/local-path-provisioner --timeout=120s"
```

Expected: rollout succeeds. If the provisioner pod was already Running without the grant, record that and move on.

- [ ] **Step 3: Label the host path for SELinux** (without this, helper pods fail `mkdir … Permission denied`, PVCs stay Pending forever, and assisted-service never starts — observed 2026-09-14)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  for n in hcp-mgmt-ctlplane-0.hcplab.corp hcp-mgmt-ctlplane-1.hcplab.corp hcp-mgmt-ctlplane-2.hcplab.corp; do oc debug node/$n -- chroot /host sh -c 'mkdir -p /opt/local-path-provisioner && chcon -Rt container_file_t /opt/local-path-provisioner'; done
  oc adm policy add-scc-to-group privileged system:serviceaccounts:local-path-storage"
```

Expected: pending PVCs bind within ~2 min (`oc get pvc -n multicluster-engine` all Bound).

### Task 4: MCE + HyperShift on `hcp-mgmt`

**Consumes:** healthy `hcp-mgmt`, `local-path` SC. **Produces:** MCE Available, `hypershift` namespace with operator + `supported-versions` listing 4.22, `hcp` CLI on the box at `/root/hcp`.

- [ ] **Step 1: Subscribe to the MCE channel that supports OCP 4.22** (resolve, don't guess)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc get packagemanifest multicluster-engine -n openshift-marketplace -o jsonpath='{.status.channels[*].name}'"
```

Pick the newest `stable-2.x` channel (observed 2026-09-14: `stable-2.11`, `stable-2.17` — use 2.17), substitute for `MCE_CHANNEL` below:

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc create namespace multicluster-engine --dry-run=client -o yaml | oc apply -f -
  oc apply -f - <<'EOF'
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata: {name: mce-og, namespace: multicluster-engine}
spec: {targetNamespaces: [multicluster-engine]}
EOF
  oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata: {name: multicluster-engine, namespace: multicluster-engine}
spec:
  channel: MCE_CHANNEL
  installPlanApproval: Automatic
  name: multicluster-engine
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF
  for i in \$(seq 1 20); do p=\$(oc get csv -n multicluster-engine -o jsonpath='{.items[0].status.phase}' 2>/dev/null); echo \"\$i: \$p\"; [ \"\$p\" = Succeeded ] && break; sleep 15; done"
```

- [ ] **Step 2: Create the MultiClusterEngine instance and verify HyperShift**

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc apply -f - <<'EOF'
apiVersion: multicluster.openshift.io/v1
kind: MultiClusterEngine
metadata: {name: multiclusterengine}
spec: {}
EOF
  sleep 60; oc get mce multiclusterengine -o jsonpath='{.status.phase}'; echo
  oc get pods -n hypershift --no-headers | head -5
  oc get cm supported-versions -n hypershift -o jsonpath='{.data}'"
```

Expected: MCE phase `Available`, hypershift-operator pod Running, `supported-versions` lists `4.22`. If `4.22` is absent, stop — the HyperShift build can't mint 4.22 hosted clusters and the version choice must be revisited.

- [ ] **Step 3: Obtain the `hcp` CLI and check version**

Get the per-arch URL from `oc get ConsoleCLIDownload hcp-cli-download -o json` (MCE ≥2.5 creates it; archives are `hcp-<os>-<arch>.tar.gz`, e.g. linux-arm64). The route hostname does NOT resolve (kcli dnsmasq carries explicit records only) — download through a port-forward instead (service port is 80, not 8080):

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc -n multicluster-engine port-forward svc/hcp-cli-download 8080:80 >/tmp/pf.log 2>&1 &
  sleep 5
  curl -s --max-time 180 -o /root/hcp.tar.gz http://localhost:8080/linux/arm64/hcp.tar.gz
  kill %1 2>/dev/null
  tar xzf /root/hcp.tar.gz -C /root && chmod +x /root/hcp && /root/hcp version"
```

Expected: `openshift/hypershift` supporting 4.22 (observed 2026-09-14: client+server `23af95…`, server versions 4.14–4.22).

- [ ] **Step 4: Early ARM64 admission recon** (5 min; de-risks the prime unknown before spending time on workers)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc get validatingwebhookconfiguration | grep -i -E 'hypershift|hosted|nodepool' || echo NO_HYPERSHIFT_WEBHOOKS
  oc get crd nodepools.hypershift.openshift.io -o yaml | grep -i -E 'arm64|aarch64' | head -5 || echo NO_ARM64_MENTION_NODEPOOL
  oc get crd hostedclusters.hypershift.openshift.io -o yaml | grep -i -E 'arm64|aarch64' | head -5 || echo NO_ARM64_MENTION_HC"
```

Expected: informational. Any explicit arch allowlist/denylist found here decides whether Task 7's `--render` output needs adjustment — record findings, do not change course yet. Absence of mentions is neutral (proceed to Task 5).

### Task 5: Assisted service + discovery for the hosted workers

**Consumes:** MCE Available, `local-path` SC. **Produces:** Ready `AgentServiceConfig`, `InfraEnv hc01-infraenv` in `hc01-infra` with a downloadable discovery ISO.

- [ ] **Step 1: Enable central infrastructure management** (`AgentServiceConfig` is cluster-scoped — no namespace in metadata)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc apply -f - <<'EOF'
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata: {name: agent}
spec:
  databaseStorage: {storageClassName: local-path, accessModes: [ReadWriteOnce], resources: {requests: {storage: 20Gi}}}
  filesystemStorage: {storageClassName: local-path, accessModes: [ReadWriteOnce], resources: {requests: {storage: 20Gi}}}
  imageStorage: {storageClassName: local-path, accessModes: [ReadWriteOnce], resources: {requests: {storage: 20Gi}}}
EOF
  for i in \$(seq 1 30); do r=\$(oc get agentserviceconfig agent -o jsonpath='{.status.conditions[?(@.type==\"Ready\")].status}' 2>/dev/null); echo \"\$i: Ready=\$r\"; [ \"\$r\" = True ] && break; sleep 20; done"
```

Expected: `Ready=True`. If storage requests fail against `local-path`, read the condition message — adjust sizes, not the class.

- [ ] **Step 2: Create the `InfraEnv` for `hc01`** (plain DHCP on the default net — no nmstate needed)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc create namespace hc01-infra --dry-run=client -o yaml | oc apply -f -
  oc -n hc01-infra create secret generic pull-secret --from-file=.dockerconfigjson=/root/.kcli/openshift_pull.json --type=kubernetes.io/dockerconfigjson --dry-run=client -o yaml | oc apply -f -
  SSHKEY=\$(cat /root/.kcli/id_rsa.pub)
  oc apply -f - <<EOF
apiVersion: agent-install.openshift.io/v1beta1
kind: InfraEnv
metadata: {name: hc01-infraenv, namespace: hc01-infra}
spec:
  pullSecretRef: {name: pull-secret}
  sshAuthorizedKey: \${SSHKEY}
  additionalNTPSources: ["192.168.122.1"]
  cpuArchitecture: arm64
EOF
  ISO=\$(oc -n hc01-infra get infraenv hc01-infraenv -o jsonpath='{.status.isoDownloadURL}'); echo \"ISO: \$ISO\""
```

Expected: `isoDownloadURL` populated. The ISO is downloaded in Task 6.

### Task 6: Boot 2 worker VMs from the discovery ISO, approve Agents

**Consumes:** `isoDownloadURL` from Task 5. **Produces:** 2 approved Agents in `hc01-infra`.

- [ ] **Step 1: Publish assisted DNS, download the discovery ISO, create the VMs** (8 vCPU / 32 GiB / 120 GiB each, booting ISO first)

In-VM clients resolve ONLY via libvirt dnsmasq (explicit records, no wildcard); the box itself resolves via `/etc/hosts`, not dnsmasq. Both need the assisted hostnames or the ISO download and agent registration fail. Re-running `add-last` for an existing hostname errors — delete-then-add if a record already exists.

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  virsh net-update default add-last dns-host \"<host ip='192.168.122.253'><hostname>assisted-image-service-multicluster-engine.apps.hcp-mgmt.hcplab.corp</hostname><hostname>agent-registration-multicluster-engine.apps.hcp-mgmt.hcplab.corp</hostname><hostname>assisted-service-multicluster-engine.apps.hcp-mgmt.hcplab.corp</hostname></host>\" --live --config
  for h in assisted-image-service-multicluster-engine.apps.hcp-mgmt.hcplab.corp agent-registration-multicluster-engine.apps.hcp-mgmt.hcplab.corp assisted-service-multicluster-engine.apps.hcp-mgmt.hcplab.corp; do grep -q \"\$h\" /etc/hosts || echo \"192.168.122.253 \$h\" >> /etc/hosts; done
  ISO=\$(oc -n hc01-infra get infraenv hc01-infraenv -o jsonpath='{.status.isoDownloadURL}')
  curl -skL --max-time 300 -o /home/libvirt/images/hc01-discovery.iso \"\$ISO\" && ls -lh /home/libvirt/images/hc01-discovery.iso
  file /home/libvirt/images/hc01-discovery.iso
  for i in 0 1; do kcli create vm -P memory=32768 -P numcpus=8 -P disks=[120] -P nets=[default] -P iso=/home/libvirt/images/hc01-discovery.iso hc01-worker-\$i; done
  virsh list --all | grep hc01"
```

Expected: assisted hostnames resolve (box + VMs); the ISO is a real bootable image (`file` says ISO 9660, ~99 MB minimal — NOT a 2.5 K HTML error page; `-k` is required, the route uses the cluster self-signed CA); `hc01-worker-0/1` running. If VMs were created from a bad ISO, `virsh reboot` may not re-read it (and ACPI reboot can be ignored with no guest agent) — use `virsh destroy` + `virsh start`; diagnose headlessly with `virsh screenshot <vm> /tmp/x.ppm`.

- [ ] **Step 2: Wait for Agents, approve them, assign hostnames**

Discovery VMs have no DHCP hostname, so both Agents may register as `localhost` — assisted requires unique hostnames, hence the explicit assignment below (index order is arbitrary; either mapping is fine).

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  for i in \$(seq 1 30); do n=\$(oc -n hc01-infra get agents --no-headers 2>/dev/null | wc -l); echo \"\$i: agents=\$n\"; [ \"\$n\" -ge 2 ] && break; sleep 20; done
  oc -n hc01-infra get agents --no-headers -o custom-columns=NAME:.metadata.name,APPROVED:.spec.approved
  i=0; for a in \$(oc -n hc01-infra get agents -o jsonpath='{.items[*].metadata.name}'); do oc -n hc01-infra patch agent \$a --type merge -p \"{\\\"spec\\\":{\\\"approved\\\":true,\\\"hostname\\\":\\\"hc01-worker-\$i\\\"}}\"; i=\$((i+1)); done
  oc -n hc01-infra get agents --no-headers -o custom-columns=NAME:.metadata.name,APPROVED:.spec.approved,ROLE:.spec.role,HOSTNAME:.spec.hostname"
```

Expected: 2 agents, `approved=true`, unique hostnames `hc01-worker-0`/`hc01-worker-1`. If an agent's auto-detected installation disk isn't the 120 GiB data disk, patch `spec.installation_disk_id` to the right `/dev/disk/by-id/...` taken from that agent's `status.inventory.disks` — read, don't guess. On any non-Ready agent, `oc describe agent` first — never retry blindly.

### Task 7: Create hosted cluster `hc01`

**Consumes:** approved Agents, `/root/hcp`, `local-path` SC. **Produces:** Available `HostedCluster hc01`, 2 Ready nodes, hosted kubeconfig at `/root/hc01-kubeconfig`.

- [ ] **Step 1: Render and inspect** (never apply blind — `--render` first)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  echo "API IP: 192.168.122.253 (keepalived VIP, shared by API+ingress)"
  /root/hcp create cluster agent \
    --name=hc01 \
    --namespace=hc01-infra \
    --agent-namespace=hc01-infra \
    --base-domain=hcplab.corp \
    --api-server-address=api.hc01.hcplab.corp \
    --pull-secret=/root/.kcli/openshift_pull.json \
    --ssh-key=/root/.kcli/id_rsa.pub \
    --etcd-storage-class=local-path \
    --release-image=quay.io/openshift-release-dev/ocp-release:4.22.12-multi \
    --node-pool-replicas=2 \
    --control-plane-availability-policy=SingleReplica \
    --arch arm64 \
    --render > /tmp/hc01-render.yaml
  grep -c 'kind:' /tmp/hc01-render.yaml"
```

Inspect `/tmp/hc01-render.yaml` (HostedCluster + NodePool + secrets). If `4.22.12-multi` has rolled, substitute the newest 4.22 `-multi` tag (verify with `oc adm release info … -o jsonpath='{.metadata.version}'`). Explicitly confirm the NodePool arch reads `arm64` — catches wrong-arch defaults before apply.

- [ ] **Step 2: Publish `hc01` DNS inside the VM network and on the box** (without this, hosted workers/agents cannot resolve the hosted API — kcli dnsmasq carries explicit records only, no wildcard, and the box itself resolves via `/etc/hosts`, not dnsmasq. One `<host>` block per IP: duplicate hostnames across blocks are rejected, so keep a single block per IP. Hosted ingress is HostNetwork on the workers — NOT NodePort — so apps hostnames point at a worker IP, while API names point at the keepalived VIP.)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  W0=\$(virsh net-dhcp-leases default | awk '/hc01-worker-0/ {print \$5}' | cut -d/ -f1); [ -n \"\$W0\" ] || { echo NO_WORKER0_LEASE; exit 1; }; echo \"worker-0 IP: \$W0 (DHCP — re-check on rebuild; was .36 at build)\"
  virsh net-update default add-last dns-host \"<host ip='192.168.122.253'><hostname>api.hc01.hcplab.corp</hostname><hostname>api-int.hc01.hcplab.corp</hostname></host>\" --live --config
  virsh net-update default add-last dns-host \"<host ip='\$W0'><hostname>console-openshift-console.apps.hc01.hcplab.corp</hostname><hostname>oauth-openshift.apps.hc01.hcplab.corp</hostname><hostname>canary-openshift-ingress-canary.apps.hc01.hcplab.corp</hostname></host>\" --live --config
  for h in api.hc01.hcplab.corp api-int.hc01.hcplab.corp; do grep -q \"\$h\" /etc/hosts || echo \"192.168.122.253 \$h\" >> /etc/hosts; done
  for h in console-openshift-console.apps.hc01.hcplab.corp oauth-openshift.apps.hc01.hcplab.corp canary-openshift-ingress-canary.apps.hc01.hcplab.corp; do grep -q \"\$h\" /etc/hosts || echo \"\$W0 \$h\" >> /etc/hosts; done
  getent hosts api.hc01.hcplab.corp canary-openshift-ingress-canary.apps.hc01.hcplab.corp"
```

Expected: 2 records on VIP + 3 on the worker IP, in both dnsmasq and box hosts. Mirror the same split to the laptop `/etc/hosts` (NodePort publishing needs no extra LB for the API; apps ride the workers' HostNetwork router).

- [ ] **Step 3: Create the render-omitted secrets, then apply and watch** (30–60 min on ThunderX2; same tmux advice as Task 2 — don't run the watch over a bare SSH session)

`--render` omits generated secrets and reconcile fails without each one, surfacing a single error at a time. Create all three up front (etcd key = base64 of 32 random bytes, per the `AESCBCKeySecretKey = "key"` contract):

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc -n hc01-infra get secret pull-secret -o jsonpath='{.data.\.dockerconfigjson}' | base64 -d > /tmp/ps.json
  python3 -m json.tool /tmp/ps.json > /dev/null && oc -n hc01-infra create secret generic hc01-pull-secret --from-file=.dockerconfigjson=/tmp/ps.json --type=kubernetes.io/dockerconfigjson && rm -f /tmp/ps.json
  oc -n hc01-infra create secret generic hc01-ssh-key --from-file=id_rsa.pub=/root/.kcli/id_rsa.pub
  KEY_B64=\$(head -c 32 /dev/urandom | base64 -w0)
  oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata: {name: hc01-etcd-encryption-key, namespace: hc01-infra}
type: Opaque
data:
  key: \${KEY_B64}
EOF
  oc -n hc01-infra get secrets --no-headers | grep hc01"
```

Expected: all three secrets present. Then apply and watch:

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc apply -f /tmp/hc01-render.yaml
  oc -n hc01-infra get hostedcluster hc01 -w"   # watch until Progressing=False Available=True
```

Once agents show installation in progress (or after their first reboot), eject the discovery ISO so a reboot can't re-boot discovery and re-register a duplicate agent (confirm the cdrom target with `virsh domblklist` first):

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com \
  "virsh change-media hc01-worker-0 sda --eject; virsh change-media hc01-worker-1 sda --eject; virsh domblklist hc01-worker-0"
```

- [ ] **Step 4: Verify the hosted cluster** (the acceptance gate for this whole plan)

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc -n hc01-infra get hostedcluster hc01 -o jsonpath='{.status.version.history[0].version} {.status.conditions[?(@.type==\"Available\")].status}'; echo
  oc -n hc01-infra get nodepool -o custom-columns=NAME:.metadata.name,REPLICAS:.status.replicas
  oc -n hc01-infra-hc01 get pods --no-headers | grep -v Running | head || echo MGMT_CP_PODS_ALL_RUNNING
  /root/hcp create kubeconfig --name=hc01 --namespace=hc01-infra > /root/hc01-kubeconfig
  KUBECONFIG=/root/hc01-kubeconfig oc get nodes
  KUBECONFIG=/root/hc01-kubeconfig oc get clusterversion
  KUBECONFIG=/root/hc01-kubeconfig oc get infrastructures cluster -o jsonpath='{.status.controlPlaneTopology}'"
```

Expected: HostedCluster Available at 4.22.x, 2 Ready nodes in the hosted kubeconfig, `controlPlaneTopology=External` (the exact signal W0/ADR-0328 will key on later), hosted control-plane pods Running in `hc01-infra-hc01` on the management cluster. Laptop also needs the hosted split in `/etc/hosts`: api/api-int → .253, console/oauth → worker-0 IP (see Task 8 runbook).

Hosted kubeadmin password (for console login + runbook — verify the secret name first):

```bash
ssh -o StrictHostKeyChecking=no root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com "
  export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig
  oc -n hc01-infra get secrets | grep -i kubeadmin || echo NO_KUBEADMIN_SECRET
  oc -n hc01-infra get secret hc01-kubeadmin-password -o jsonpath='{.data.password}' | base64 -d; echo"
```

Expected: password printed and captured for the runbook.

### Task 8: Record evidence + write the runbook

**Consumes:** Task 7 acceptance. **Produces:** lab evidence appended to the HCP plan, fresh runbook file. No operator install here.

- [ ] **Step 1: Append lab evidence** to `docs/plans/hcp-fleet-optimization.md` "Lab evidence summary" (append — do not rewrite history): hosted `controlPlaneTopology=External`, management HCP namespace labels, hosted version, SingleReplica note.
- [ ] **Step 2: Write fresh runbook** `~/rh/kcli/hc01-hpe-apollo-cn99xx-16/ACCESS.md` (new file, current facts only): versions, API/console URLs, kubeadmin passwords (mgmt + hosted), kubeconfig paths (`/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig`, `/root/hc01-kubeconfig`), node/VM IPs, StorageClass, MCE/hcp versions, kcli commands, what was off-matrix. Note: hosted ingress uses its own CA — laptop browser/API clients need `--insecure-skip-tls-verify` or a trust-bundle step.

## Rollback (reverse order — hosted first, management last)

```bash
HYP=root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com
# 1. Hosted cluster (keeps the management cluster):
ssh -o StrictHostKeyChecking=no $HYP "export KUBECONFIG=/root/.kcli/clusters/hcp-mgmt/auth/kubeconfig; oc delete -f /tmp/hc01-render.yaml --ignore-not-found; oc -n hc01-infra delete hostedcluster hc01 --ignore-not-found"
# 2. Worker VMs:
ssh -o StrictHostKeyChecking=no $HYP "kcli delete vm -y hc01-worker-0 hc01-worker-1"
# 3. Management cluster (destroys everything):
ssh -o StrictHostKeyChecking=no $HYP "kcli delete kube -y hcp-mgmt"
# 4. DNS cleanup (only if Task 7 Step 2 was applied): virsh net-edit default,
#    remove the hc01 dns-host block, then virsh net-destroy default && virsh net-start default.
```

## Risks (stated, not buried)

| Risk | Likelihood | Mitigation in plan |
|---|---|---|
| HyperShift validation rejects ARM64-hosted CP (off-matrix) | Medium | Task 4 Step 4 recon first; Task 7 renders before applying; error text decides the next move (likely `--render` YAML tweak, not redesign) |
| SingleReplica etcd on host-path dies with the node | Low (lab) | Accepted in constraints; hosted cluster rebuilds from Task 7 YAML in <1h |
| ThunderX2 slowness trips default timeouts | Medium | Generous waits in every polling loop; 45–90 min budgeted per cluster install |
| MCE/assisted-service lack arm64 images | Low | Verified pre-apply at each step (manifest inspect / CSV phase gates) |
| Compact-cluster resource pressure (MCE + monitoring + hosted CP on 3×48 GiB) | Medium | Budget leaves ~38 GiB headroom; if mgmt COs go memory-pressured, grow workers before debugging HCP |
