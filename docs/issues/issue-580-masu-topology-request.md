# Issue #580 companion: masu-side topology enrichment request

## Status
Requested (koku-side change pending). Supersedes #595, which was filed in
error for this content and closed with a pointer here. Parent work: #580
(processor persists `clusters.cluster_topology` from enriched messages;
shipped, awaiting enriched traffic).

## Summary
`ROSReportShipper.build_ros_msg` (`koku/masu/external/ros_report_shipper.py`)
ships per-CSV S3 URLs plus identity metadata, but not the manifest's
topology facts — even though the shipper holds the full manifest in memory
(`payload_info.manifest.cr_status`). Downstream, ros-ocp-backend #580 cannot
classify cluster topology on the processor path, so every Kafka-ingested
cluster stays `cluster_topology='unknown'`.

## Proposed contract (additive, backward compatible)
Add to the ROS message `metadata`:

```json
"topology": {
  "controlPlaneTopology": "<Infrastructure.status.controlPlaneTopology>",
  "hostedClusterCount": "<count of visible HostedClusters>",
  "hostedControlPlaneNamespaces": ["<ns-name>", "..."]
}
```

Sourced from the already-held `manifest.cr_status` (same vocabulary the
operator reports; absent/pre-#406 manifests omit the key). No existing
fields change; consumers ignore unknown keys. Naming mirrors
`librobne/topology.TopologyFacts` JSON tags on the consumer side, which
already parses this shape (`librobne/csv/manifest.go`, `internal/types`
`KafkaMsg.metadata.topology`).

## Acceptance (koku side)
- [ ] ROS Kafka messages for manifests carrying `cr_status.topology` include the `topology` object above
- [ ] Manifests without topology facts produce messages byte-identical in shape to today (key absent, nothing else moves)
- [ ] Unit test on `build_ros_msg` with/without facts

## Non-goals
Changing collection, payload contents, or upload behavior. Consumer-side
parsing lives in ros-ocp-backend #580 (shipped).
