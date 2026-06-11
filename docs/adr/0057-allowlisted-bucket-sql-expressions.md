# ADR-0057: Use allowlisted bucket SQL expressions (BucketGranularity)

## Status

Accepted

## Context

Dynamic GROUP BY strings from user input would enable SQL injection in boxplot queries.

## Decision

Allowlisted set of bucket expressions; reject anything not in the allowlist. Validation runs at query parse time before SQL interpolation.

## Consequences

SQL injection proof for boxplots. Limited to predefined granularities. Threat model: user-supplied `group_by` or bucket parameters never reach raw SQL—only mapped constants from `BucketGranularity` enter the query template; arbitrary expressions would expose aggregation injection (finding class: untrusted input in SQL fragment).

## References

- [internal/model/boxplot.go](internal/model/boxplot.go)
