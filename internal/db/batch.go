package db

// MaxPgxBatchQueue caps pgx.Batch queue depth to avoid unbounded RAM on large
// clusters. 2000 balances round-trip reduction (fewer batches per flush) against
// memory (~2000 × 44 params × 16 B ≈ 1.4 MiB per batch). For a 10K-container
// file flushed once at EOF, each flush needs ceil(10000/2000) = 5 round-trips.
const MaxPgxBatchQueue = 2000
