// Package pgdigest upserts and reads daily digests in PostgreSQL.
// Container, namespace, node, GPU, PVC, VM (+ GPU devices), namespace quota,
// and cluster quota writers and slim Read* live here. Container and namespace
// writers/readers accept schedule_type (all_hours and business_hours).
// PVC and quota writers use ingest GREATEST/LEAST merge; other entities are
// last-write-wins.
// Container recommend-path SELECT is ForEachSchedule / ReadContainerDigests*.
// Processor BH list/detail uses ForEachScheduleForClusters and
// ForEachScheduleForContainers (page unnest); engine Query* only groups.
// Processor and the robne CLI import it; robne-operator must not (ADR-0305).
package pgdigest
