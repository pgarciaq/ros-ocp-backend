# ADR-0057: Use allowlisted bucket SQL expressions (BucketGranularity)

## Status

Accepted

## Context

Dynamic GROUP BY strings from user input would enable SQL injection in boxplot queries.

## Decision

Allowlisted set of bucket expressions; reject anything not in the allowlist.

## Consequences

SQL injection proof for boxplots. Limited to predefined granularities.

## References

- [internal/model/boxplot.go](internal/model/boxplot.go)
