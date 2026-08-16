package pgdigest

// Package pgdigest upserts and reads daily digests in PostgreSQL.
// Container, namespace, node, GPU, PVC, VM (+ GPU devices), namespace quota,
// and cluster quota writers and slim Read* live here. Processor and the robne
// CLI import it; robne-operator must not (ADR-0305).
