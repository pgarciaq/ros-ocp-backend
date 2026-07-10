#!/usr/bin/env python3
"""Generate a comprehensive nise YAML config for ROS-OCP scale benchmarks.

Produces a nise-compatible YAML that exercises ALL recommendation engines in
ros-ocp-backend:

  - Container rightsizing (regular, over-provisioned, under-provisioned)
  - Idle/zombie container detection (near-zero CPU and memory usage)
  - GPU time-slicing recommendations (container-level DCGM metrics)
  - GPU MIG recommendations (container-level with mig_instances)
  - Namespace recommendations (from namespace-level aggregation)
  - Namespace quota recommendations (resource_quota in namespaces)
  - Cluster quota recommendations (cluster_resource_quotas)
  - PVC recommendations (oversized, near-full, orphaned, healthy)
  - Snapshot staleness recommendations (stale, orphaned, never-restored)
  - VM rightsizing (active, idle, abandoned, Windows, Linux)
  - VM GPU recommendations (passthrough, MIG, idle, saturated)
  - Business hours (implicit — ros-ocp-backend classifies by interval timestamp)

Usage:
    python3 gen_benchmark_config.py --containers 10000 --output /data/bench_config.yml
    python3 gen_benchmark_config.py --containers 20000 --start-date 2026-07-01 --end-date 2026-07-31
    python3 gen_benchmark_config.py --containers 50000 --output /data/big_bench.yml

The script scales all entity types proportionally based on --containers:

    Total containers = regular + idle + GPU time-slicing + GPU MIG
    VMs = ~2.5% of total containers (separate OCPVirtualMachineGenerator)
    PVCs = ~10% of namespaces get PVCs (4 types: oversized, near-full, orphaned, healthy)
    Snapshots = ~2 per PVC namespace
    Namespace quotas = ~15% of namespaces get resource_quota
    Cluster quotas = 2 (fixed, covers multiple namespaces)

Prerequisites:
    pip install koku-nise  (or the fork with optimizations)

Then run nise with the generated config:
    nise report ocp --static-report-file <output.yml> \\
        --ocp-cluster-id <CLUSTER_UUID> --ros-ocp-info -w
"""

import argparse
import random
import sys
import textwrap
from dataclasses import dataclass
from pathlib import Path


@dataclass
class BenchmarkConfig:
    total_containers: int
    start_date: str
    end_date: str
    output: str
    seed: int

    # Derived proportions (percentages of total_containers)
    idle_pct: float = 0.03        # 3% idle/zombie containers
    gpu_ts_pct: float = 0.02      # 2% GPU time-slicing containers
    gpu_mig_pct: float = 0.005    # 0.5% GPU MIG containers
    vm_pct: float = 0.025         # 2.5% VMs (separate generator)
    vm_gpu_pct: float = 0.3       # 30% of VMs get GPUs

    nodes_per_1000: int = 1       # 1 node per 1000 containers
    ns_per_1000: int = 15         # 15 namespaces per 1000 containers
    pvc_ns_pct: float = 0.10      # 10% of namespaces get PVCs
    quota_ns_pct: float = 0.15    # 15% of namespaces get namespace quotas

    @property
    def num_nodes(self) -> int:
        return max(3, self.total_containers * self.nodes_per_1000 // 1000)

    @property
    def num_namespaces(self) -> int:
        return max(10, self.total_containers * self.ns_per_1000 // 1000)

    @property
    def num_idle(self) -> int:
        return max(3, int(self.total_containers * self.idle_pct))

    @property
    def num_gpu_ts(self) -> int:
        return max(3, int(self.total_containers * self.gpu_ts_pct))

    @property
    def num_gpu_mig(self) -> int:
        return max(2, int(self.total_containers * self.gpu_mig_pct))

    @property
    def num_regular(self) -> int:
        return self.total_containers - self.num_idle - self.num_gpu_ts - self.num_gpu_mig

    @property
    def num_vms(self) -> int:
        return max(5, int(self.total_containers * self.vm_pct))

    @property
    def num_vm_gpus(self) -> int:
        return max(2, int(self.num_vms * self.vm_gpu_pct))

    @property
    def num_pvc_namespaces(self) -> int:
        return max(2, int(self.num_namespaces * self.pvc_ns_pct))

    @property
    def num_quota_namespaces(self) -> int:
        return max(2, int(self.num_namespaces * self.quota_ns_pct))


GPU_TS_MODELS = [
    ("Tesla T4", 15360),
    ("NVIDIA L4", 23034),
    ("NVIDIA L40S", 48640),
]

GPU_MIG_MODELS = [
    ("NVIDIA A100-SXM4-80GB", 81559, ["1g.10gb", "2g.20gb", "3g.40gb", "7g.80gb"]),
    ("NVIDIA H100-SXM5-80GB", 81559, ["1g.18gb", "2g.24gb", "3g.40gb", "7g.80gb"]),
    ("NVIDIA A100-PCIE-40GB", 40960, ["1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb"]),
]

VM_GPU_MODELS = [
    ("NVIDIA A100-SXM4-80GB", "3g.40gb"),
    ("NVIDIA A100-SXM4-40GB", "3g.20gb"),
    ("NVIDIA T4", None),
    ("NVIDIA H100-SXM5-80GB", "3g.40gb"),
]

VM_GPU_UTILS = ["idle", "low", "medium", "high", "saturated"]


def _indent(text: str, level: int) -> str:
    return textwrap.indent(text, "  " * level)


class YAMLBuilder:
    """Builds the multi-generator nise YAML config."""

    def __init__(self, cfg: BenchmarkConfig):
        self.cfg = cfg
        random.seed(cfg.seed)

    def build(self) -> str:
        lines = [
            "---",
            f"# Auto-generated benchmark config: {self.cfg.total_containers} containers",
            f"# + {self.cfg.num_vms} VMs ({self.cfg.num_vm_gpus} with GPUs)",
            f"# + {self.cfg.num_idle} idle/zombie containers",
            f"# + {self.cfg.num_gpu_ts} GPU time-slicing containers",
            f"# + {self.cfg.num_gpu_mig} GPU MIG containers",
            f"# + ~{self.cfg.num_pvc_namespaces * 4} PVCs across {self.cfg.num_pvc_namespaces} namespaces",
            f"# + ~{self.cfg.num_pvc_namespaces * 2} snapshots",
            f"# + {self.cfg.num_quota_namespaces} namespaces with resource quotas",
            f"# + 2 cluster resource quotas",
            f"# Date range: {self.cfg.start_date} to {self.cfg.end_date}",
            f"# Seed: {self.cfg.seed}",
            "#",
            "# Run with: nise report ocp --static-report-file <this-file> \\",
            "#           --ocp-cluster-id <CLUSTER_UUID> --ros-ocp-info -w",
            "generators:",
        ]

        # Build node → namespace → pod hierarchy
        nodes = self._build_nodes()
        namespaces = self._build_namespaces()
        pod_assignments = self._assign_pods_to_namespaces(namespaces)
        ns_to_node = self._assign_namespaces_to_nodes(namespaces, nodes)

        # Designate special namespaces
        pvc_namespaces = set(random.sample(namespaces, self.cfg.num_pvc_namespaces))
        quota_namespaces = set(random.sample(namespaces, self.cfg.num_quota_namespaces))

        # Group namespaces by node for generating OCPGenerator blocks
        node_ns_map: dict[str, list[str]] = {}
        for ns, node in ns_to_node.items():
            node_ns_map.setdefault(node, []).append(ns)

        # Build snapshots list (tied to PVC namespaces)
        snapshot_entries = self._build_snapshots(pvc_namespaces)

        # Build cluster resource quotas
        crq_ns_list = random.sample(namespaces, min(6, len(namespaces)))
        crq_entries = self._build_cluster_quotas(crq_ns_list)

        # Generate one OCPGenerator per node (keeps YAML manageable)
        for node_name in nodes:
            node_namespaces = node_ns_map.get(node_name, [])
            if not node_namespaces:
                continue

            lines.append(f"  - OCPGenerator:")
            lines.append(f"      start_date: {self.cfg.start_date}")
            lines.append(f"      end_date: {self.cfg.end_date}")

            # Cluster quotas only on the first node's generator
            if node_name == nodes[0] and crq_entries:
                lines.append(f"      cluster_resource_quotas:")
                lines.extend(crq_entries)

            # Snapshots only on the first node's generator
            if node_name == nodes[0] and snapshot_entries:
                lines.append(f"      snapshots:")
                lines.extend(snapshot_entries)

            lines.append(f"      nodes:")
            lines.append(f"        - node:")
            lines.append(f"          node_name: {node_name}")
            lines.append(f"          cpu_cores: 64")
            lines.append(f"          memory_gig: 256")
            lines.append(f"          resource_id: {node_name}")

            # GPU nodes get the GPU-present label
            has_gpu_pods = any(
                p.get("gpu_type") for ns in node_namespaces for p in pod_assignments.get(ns, [])
            )
            if has_gpu_pods:
                lines.append(
                    f"          node_labels: label_node_name:{node_name}|nvidia_com_gpu_present:True"
                )

            lines.append(f"          namespaces:")

            for ns in node_namespaces:
                lines.append(f"            {ns}:")

                # Namespace labels
                lines.append(f"              namespace_labels: label_ns:{ns}|label_bench:true")

                # Namespace quotas
                if ns in quota_namespaces:
                    lines.extend(self._build_namespace_quota(ns))

                pods = pod_assignments.get(ns, [])
                if pods:
                    lines.append(f"              pods:")
                    for pod in pods:
                        lines.extend(self._build_pod(pod))

                # PVCs
                if ns in pvc_namespaces:
                    pod_name = pods[0]["name"] if pods else f"{ns}-pod-000"
                    lines.extend(self._build_pvcs(ns, pod_name))

        # VM generator (separate block)
        vm_lines = self._build_vm_generator()
        if vm_lines:
            lines.extend(vm_lines)

        return "\n".join(lines) + "\n"

    def _build_nodes(self) -> list[str]:
        return [f"bench-node-{i:03d}" for i in range(self.cfg.num_nodes)]

    def _build_namespaces(self) -> list[str]:
        return [f"bench-ns-{i:04d}" for i in range(self.cfg.num_namespaces)]

    def _assign_pods_to_namespaces(self, namespaces: list[str]) -> dict[str, list[dict]]:
        """Distribute all pod types across namespaces."""
        assignments: dict[str, list[dict]] = {ns: [] for ns in namespaces}

        pod_counter = 0

        # Regular containers
        for i in range(self.cfg.num_regular):
            ns = namespaces[i % len(namespaces)]
            assignments[ns].append({
                "name": f"{ns}-pod-{pod_counter:05d}",
                "type": "regular",
                "gpu_type": None,
            })
            pod_counter += 1

        # Idle/zombie containers (spread across ~20% of namespaces)
        idle_namespaces = random.sample(namespaces, max(2, len(namespaces) // 5))
        for i in range(self.cfg.num_idle):
            ns = idle_namespaces[i % len(idle_namespaces)]
            assignments[ns].append({
                "name": f"{ns}-idle-{pod_counter:05d}",
                "type": "idle",
                "gpu_type": None,
            })
            pod_counter += 1

        # GPU time-slicing containers
        gpu_ts_namespaces = random.sample(namespaces, max(2, len(namespaces) // 10))
        for i in range(self.cfg.num_gpu_ts):
            ns = gpu_ts_namespaces[i % len(gpu_ts_namespaces)]
            assignments[ns].append({
                "name": f"{ns}-gputd-{pod_counter:05d}",
                "type": "gpu_ts",
                "gpu_type": "ts",
            })
            pod_counter += 1

        # GPU MIG containers
        gpu_mig_namespaces = random.sample(namespaces, max(1, len(namespaces) // 20))
        for i in range(self.cfg.num_gpu_mig):
            ns = gpu_mig_namespaces[i % len(gpu_mig_namespaces)]
            assignments[ns].append({
                "name": f"{ns}-gpumig-{pod_counter:05d}",
                "type": "gpu_mig",
                "gpu_type": "mig",
            })
            pod_counter += 1

        return assignments

    def _assign_namespaces_to_nodes(
        self, namespaces: list[str], nodes: list[str]
    ) -> dict[str, str]:
        return {ns: nodes[i % len(nodes)] for i, ns in enumerate(namespaces)}

    def _build_pod(self, pod: dict) -> list[str]:
        lines = []
        name = pod["name"]
        pod_type = pod["type"]

        if pod_type == "idle":
            cpu_req = random.randint(4, 16)
            cpu_lim = cpu_req * 2
            mem_req = round(random.uniform(2.0, 8.0), 1)
            mem_lim = mem_req * 2
            cpu_usage = round(random.uniform(0.001, 0.01), 4)
            mem_usage = round(random.uniform(0.01, 0.05), 3)
            label_extra = "|label_idle:true"
        elif pod_type in ("gpu_ts", "gpu_mig"):
            cpu_req = random.randint(4, 8)
            cpu_lim = cpu_req * 2
            mem_req = round(random.uniform(8.0, 32.0), 1)
            mem_lim = round(mem_req * 1.5, 1)
            cpu_usage = round(random.uniform(1.0, float(cpu_req) * 0.8), 2)
            mem_usage = round(random.uniform(4.0, float(mem_req) * 0.9), 2)
            label_extra = f"|label_accelerator:gpu|label_workload:{'training' if random.random() < 0.5 else 'inference'}"
        else:
            cpu_req = random.randint(50, 500)
            cpu_lim = random.randint(max(cpu_req, 500), 2000)
            mem_req = round(random.uniform(0.1, 2.0), 1)
            mem_lim = round(random.uniform(max(mem_req, 2.0), 8.0), 1)
            cpu_usage = round(random.uniform(0.1, float(cpu_req) * 0.8), 2)
            mem_usage = round(random.uniform(0.05, float(mem_req) * 0.9), 2)
            label_extra = ""

        lines.append(f"                - pod:")
        lines.append(f"                  pod_name: {name}")
        lines.append(f"                  cpu_request: {cpu_req}")
        lines.append(f"                  cpu_limit: {cpu_lim}")
        lines.append(f"                  mem_request_gig: {mem_req}")
        lines.append(f"                  mem_limit_gig: {mem_lim}")
        lines.append(f"                  cpu_usage:")
        lines.append(f"                    full_period: {cpu_usage}")
        lines.append(f"                  mem_usage_gig:")
        lines.append(f"                    full_period: {mem_usage}")
        lines.append(f"                  labels: label_app:{name.rsplit('-', 1)[0]}{label_extra}")
        lines.append(f"                  workload: deployment")

        # GPU blocks
        if pod_type == "gpu_ts":
            model, mem_mib = random.choice(GPU_TS_MODELS)
            lines.append(f"                  gpus:")
            lines.append(f"                    - gpu:")
            lines.append(f'                      gpu_model: "{model}"')
            lines.append(f"                      gpu_memory_capacity_mib: {mem_mib}")
            lines.append(f"                      sm_active_avg: {round(random.uniform(0.05, 0.6), 2)}")
            lines.append(f"                      tensor_pipe_active_avg: {round(random.uniform(0.02, 0.3), 2)}")
            lines.append(f"                      dram_active_avg: {round(random.uniform(0.03, 0.4), 2)}")
            lines.append(f"                      fb_usage_avg: {round(random.uniform(500, mem_mib * 0.8), 1)}")

        elif pod_type == "gpu_mig":
            model, mem_mib, profiles = random.choice(GPU_MIG_MODELS)
            profile = random.choice(profiles)
            num_mig = random.choice([1, 2])
            lines.append(f"                  gpus:")
            lines.append(f"                    - gpu:")
            lines.append(f'                      gpu_model: "{model}"')
            lines.append(f"                      gpu_memory_capacity_mib: {mem_mib}")
            lines.append(f"                      mig_instances:")
            for _ in range(num_mig):
                lines.append(f'                        - mig_instance:')
                lines.append(f'                          mig_profile: "{profile}"')
                lines.append(f'                          mig_strategy: "{random.choice(["single", "mixed"])}"')

        return lines

    def _build_namespace_quota(self, ns: str) -> list[str]:
        cpu_used = round(random.uniform(1.0, 8.0), 1)
        mem_used = random.randint(2, 16)
        return [
            f"              resource_quota:",
            f"                cpu_request_used: {cpu_used}",
            f"                cpu_limit_used: {round(cpu_used * 2, 1)}",
            f"                memory_request_used_gig: {mem_used}",
            f"                memory_limit_used_gig: {mem_used * 2}",
        ]

    def _build_pvcs(self, ns: str, pod_name: str) -> list[str]:
        lines = [f"              volumes:"]

        # Oversized PVC (~5% utilization)
        lines.extend([
            f"                - volume:",
            f"                  volume_name: {ns}-vol-oversized",
            f"                  storage_class: gp3-csi",
            f"                  volume_request_gig: 100",
            f"                  labels: label_scenario:oversized",
            f"                  volume_claims:",
            f"                    - volume_claim:",
            f"                      volume_claim_name: {ns}-pvc-oversized",
            f"                      pod_name: {pod_name}",
            f"                      labels: label_scenario:oversized",
            f"                      capacity_gig: 100",
            f"                      volume_claim_usage_gig:",
            f"                        full_period: 5",
        ])

        # Near-full PVC (~85% utilization)
        lines.extend([
            f"                - volume:",
            f"                  volume_name: {ns}-vol-nearfull",
            f"                  storage_class: gp3-csi",
            f"                  volume_request_gig: 10",
            f"                  labels: label_scenario:near_full",
            f"                  volume_claims:",
            f"                    - volume_claim:",
            f"                      volume_claim_name: {ns}-pvc-nearfull",
            f"                      pod_name: {pod_name}",
            f"                      labels: label_scenario:near_full",
            f"                      capacity_gig: 10",
            f"                      volume_claim_usage_gig:",
            f"                        full_period: 8.5",
        ])

        # Orphaned PVC (zero usage)
        lines.extend([
            f"                - volume:",
            f"                  volume_name: {ns}-vol-orphaned",
            f"                  storage_class: gp2",
            f"                  volume_request_gig: 20",
            f"                  labels: label_scenario:orphaned",
            f"                  volume_claims:",
            f"                    - volume_claim:",
            f"                      volume_claim_name: {ns}-pvc-orphaned",
            f"                      pod_name: {pod_name}",
            f"                      labels: label_scenario:orphaned",
            f"                      capacity_gig: 20",
            f"                      volume_claim_usage_gig:",
            f"                        full_period: 0",
        ])

        # Healthy PVC (~60% utilization)
        lines.extend([
            f"                - volume:",
            f"                  volume_name: {ns}-vol-healthy",
            f"                  storage_class: gp3-csi",
            f"                  volume_request_gig: 50",
            f"                  labels: label_scenario:healthy",
            f"                  volume_claims:",
            f"                    - volume_claim:",
            f"                      volume_claim_name: {ns}-pvc-healthy",
            f"                      pod_name: {pod_name}",
            f"                      labels: label_scenario:healthy",
            f"                      capacity_gig: 50",
            f"                      volume_claim_usage_gig:",
            f"                        full_period: 30",
        ])

        return lines

    def _build_snapshots(self, pvc_namespaces: set[str]) -> list[str]:
        lines = []
        for ns in sorted(pvc_namespaces):
            # Stale snapshot (>90 days old, never restored)
            lines.extend([
                f"        - snapshot_name: {ns}-snap-stale",
                f"          namespace: {ns}",
                f"          source_pvc_name: {ns}-pvc-oversized",
                f"          creation_days_ago: {random.randint(91, 180)}",
                f"          source_pvc_exists: true",
                f"          restored_pvc_count: 0",
                f"          restore_size_bytes: {random.randint(10, 100) * 1073741824}",
            ])
            # Orphaned snapshot (source PVC deleted)
            lines.extend([
                f"        - snapshot_name: {ns}-snap-orphaned",
                f"          namespace: {ns}",
                f"          source_pvc_name: {ns}-pvc-deleted",
                f"          creation_days_ago: {random.randint(30, 90)}",
                f"          source_pvc_exists: false",
                f"          restored_pvc_count: 0",
                f"          restore_size_bytes: {random.randint(5, 50) * 1073741824}",
            ])
        return lines

    def _build_cluster_quotas(self, ns_list: list[str]) -> list[str]:
        ns_str_1 = ",".join(ns_list[:3])
        ns_str_2 = ",".join(ns_list[3:6]) if len(ns_list) > 3 else ns_list[0]
        return [
            f"        - name: bench-crq-team",
            f"          cpu_request_hard: 200",
            f"          cpu_limit_hard: 400",
            f"          memory_request_hard_gig: 500",
            f"          memory_limit_hard_gig: 1000",
            f"          cpu_request_used: 180",
            f"          cpu_limit_used: 360",
            f"          memory_request_used_gig: 450",
            f"          memory_limit_used_gig: 900",
            f"          storage_request_hard_gig: 5000",
            f"          storage_request_used_gig: 4200",
            f"          pods_hard: 2000",
            f"          pods_used: 1750",
            f"          object_count_hard: 0",
            f"          object_count_used: 0",
            f"          namespaces: {ns_str_1}",
            f"        - name: bench-crq-platform",
            f"          cpu_request_hard: 300",
            f"          cpu_limit_hard: 600",
            f"          memory_request_hard_gig: 640",
            f"          memory_limit_hard_gig: 1280",
            f"          cpu_request_used: 270",
            f"          memory_request_used_gig: 580",
            f"          storage_request_hard_gig: 10000",
            f"          storage_request_used_gig: 8000",
            f"          pods_hard: 5000",
            f"          pods_used: 4100",
            f"          object_count_hard: 0",
            f"          object_count_used: 0",
            f"          namespaces: {ns_str_2}",
        ]

    def _build_vm_generator(self) -> list[str]:
        if self.cfg.num_vms < 1:
            return []

        lines = [
            f"  # VMs: {self.cfg.num_vms} total ({self.cfg.num_vm_gpus} with GPUs)",
            f"  - OCPVirtualMachineGenerator:",
            f"      start_date: {self.cfg.start_date}",
            f"      end_date: {self.cfg.end_date}",
            f"      vms:",
        ]

        num_idle = max(1, self.cfg.num_vms // 10)
        num_abandoned = max(1, self.cfg.num_vms // 20)
        num_windows = max(1, self.cfg.num_vms // 5)
        num_gpu = self.cfg.num_vm_gpus
        num_regular = self.cfg.num_vms - num_idle - num_abandoned - num_gpu

        vm_counter = 0

        # Regular Linux VMs
        for i in range(max(0, num_regular - num_windows)):
            ns = f"bench-ns-{i % self.cfg.num_namespaces:04d}"
            lines.extend([
                f"        - vm_name: bench-vm-linux-{vm_counter:04d}",
                f"          namespace: {ns}",
                f"          node_name: bench-node-{vm_counter % self.cfg.num_nodes:03d}",
                f"          guest_os: linux",
                f"          guest_agent: true",
                f"          vcpu: {random.choice([2, 4, 8])}",
                f"          memory_gib: {random.choice([4, 8, 16, 32])}",
                f"          disk_gib: {random.choice([50, 100, 200, 500])}",
            ])
            vm_counter += 1

        # Windows VMs
        for i in range(num_windows):
            ns = f"bench-ns-{(vm_counter + i) % self.cfg.num_namespaces:04d}"
            lines.extend([
                f"        - vm_name: bench-vm-win-{vm_counter:04d}",
                f"          namespace: {ns}",
                f"          node_name: bench-node-{vm_counter % self.cfg.num_nodes:03d}",
                f"          guest_os: windows",
                f"          guest_agent: {random.choice(['true', 'false'])}",
                f"          vcpu: {random.choice([4, 8, 16])}",
                f"          memory_gib: {random.choice([8, 16, 32, 64])}",
                f"          disk_gib: {random.choice([100, 200, 500])}",
            ])
            vm_counter += 1

        # Idle VMs
        for i in range(num_idle):
            lines.extend([
                f"        - vm_name: bench-vm-idle-{vm_counter:04d}",
                f"          namespace: bench-ns-{vm_counter % self.cfg.num_namespaces:04d}",
                f"          node_name: bench-node-{vm_counter % self.cfg.num_nodes:03d}",
                f"          guest_os: linux",
                f"          guest_agent: true",
                f"          vcpu: 4",
                f"          memory_gib: 8",
                f"          disk_gib: 40",
                f"          idle: true",
            ])
            vm_counter += 1

        # Abandoned VMs
        for i in range(num_abandoned):
            lines.extend([
                f"        - vm_name: bench-vm-abandoned-{vm_counter:04d}",
                f"          namespace: bench-ns-{vm_counter % self.cfg.num_namespaces:04d}",
                f"          node_name: bench-node-{vm_counter % self.cfg.num_nodes:03d}",
                f"          guest_os: linux",
                f"          guest_agent: true",
                f"          vcpu: 4",
                f"          memory_gib: 8",
                f"          disk_gib: 50",
                f"          abandoned: true",
            ])
            vm_counter += 1

        # GPU VMs (mix of MIG and passthrough)
        for i in range(num_gpu):
            model, mig_profile = random.choice(VM_GPU_MODELS)
            util = random.choice(VM_GPU_UTILS)
            lines.extend([
                f"        - vm_name: bench-vm-gpu-{vm_counter:04d}",
                f"          namespace: bench-ns-{vm_counter % self.cfg.num_namespaces:04d}",
                f"          node_name: bench-node-{vm_counter % self.cfg.num_nodes:03d}",
                f"          guest_os: linux",
                f"          guest_agent: true",
                f"          vcpu: {random.choice([8, 16, 32])}",
                f"          memory_gib: {random.choice([32, 64, 128])}",
                f"          disk_gib: {random.choice([200, 500, 1000])}",
                f"          gpu_count: {random.choice([1, 2])}",
                f'          gpu_model: "{model}"',
                f"          gpu_utilization: {util}",
            ])
            if mig_profile:
                lines.append(f'          gpu_mig_profile: "{mig_profile}"')
            vm_counter += 1

        return lines


def main():
    parser = argparse.ArgumentParser(
        description="Generate comprehensive nise YAML config for ROS-OCP scale benchmarks",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=textwrap.dedent("""
        Examples:
          # 10K containers with VMs, GPUs, PVCs, quotas, snapshots
          python3 gen_benchmark_config.py --containers 10000

          # 20K mixed workloads for a 30-day range
          python3 gen_benchmark_config.py --containers 20000 \\
            --start-date 2026-07-01 --end-date 2026-07-31

          # 50K stress test
          python3 gen_benchmark_config.py --containers 50000 \\
            --output /data/bench_50k.yml
        """),
    )
    parser.add_argument(
        "--containers", type=int, required=True,
        help="Total number of containers (regular + idle + GPU)"
    )
    parser.add_argument(
        "--start-date", default="last_month",
        help="Start date (YYYY-MM-DD or 'last_month', default: last_month)"
    )
    parser.add_argument(
        "--end-date", default="today",
        help="End date (YYYY-MM-DD or 'today', default: today)"
    )
    parser.add_argument(
        "--output", "-o", default="bench_config.yml",
        help="Output YAML file path (default: bench_config.yml)"
    )
    parser.add_argument(
        "--seed", type=int, default=42,
        help="Random seed for reproducibility (default: 42)"
    )

    args = parser.parse_args()

    if args.containers < 100:
        print("ERROR: minimum 100 containers for a meaningful benchmark", file=sys.stderr)
        sys.exit(1)

    cfg = BenchmarkConfig(
        total_containers=args.containers,
        start_date=args.start_date,
        end_date=args.end_date,
        output=args.output,
        seed=args.seed,
    )

    print(f"Generating benchmark config:")
    print(f"  Total containers: {cfg.total_containers}")
    print(f"    Regular:        {cfg.num_regular}")
    print(f"    Idle/zombie:    {cfg.num_idle}")
    print(f"    GPU (TS):       {cfg.num_gpu_ts}")
    print(f"    GPU (MIG):      {cfg.num_gpu_mig}")
    print(f"  Nodes:            {cfg.num_nodes}")
    print(f"  Namespaces:       {cfg.num_namespaces}")
    print(f"  VMs:              {cfg.num_vms} ({cfg.num_vm_gpus} with GPUs)")
    print(f"  PVC namespaces:   {cfg.num_pvc_namespaces} ({cfg.num_pvc_namespaces * 4} PVCs)")
    print(f"  Snapshot count:   ~{cfg.num_pvc_namespaces * 2}")
    print(f"  Quota namespaces: {cfg.num_quota_namespaces}")
    print(f"  Cluster quotas:   2")
    print(f"  Date range:       {cfg.start_date} → {cfg.end_date}")
    print(f"  Output:           {cfg.output}")

    builder = YAMLBuilder(cfg)
    yaml_content = builder.build()

    output_path = Path(cfg.output)
    output_path.write_text(yaml_content)

    size_kb = output_path.stat().st_size / 1024
    print(f"\nConfig written: {output_path} ({size_kb:.0f} KB)")
    print(f"\nNext steps:")
    print(f"  nise report ocp \\")
    print(f"    --static-report-file {cfg.output} \\")
    print(f"    --ocp-cluster-id <CLUSTER_UUID> \\")
    print(f"    --ros-ocp-info -w")


if __name__ == "__main__":
    main()
