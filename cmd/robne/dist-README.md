# robne — Resource Optimization for OpenShift (CLI)

Offline/batch OpenShift rightsizing recommendations computed from ROS CSV
payloads (NISE-generated or koku-metrics-operator packages). Built by Red Hat.

Full documentation: https://pgarciaq.github.io/ros-ocp-backend/features/robne-cli/

## Quickstart

    # What to apply (list)
    robne recommend --input ./csvs/ --plugins container --no-user-config \
        --format json > recs.json

    # Why this container is recommended at its number (detail)
    robne explain --input ./csvs/ --no-user-config \
        --namespace app --workload api --container api --term short --engine cost

Logs go to stderr and the payload (JSON, CSV, or table) to stdout, so every
`--format json` output can be redirected and parsed as one document; pipe
`2> robne.log` when you want the logs beside it.

## This package

    robne                    the binary
    robne.yaml.sample        the full engine-knob reference config
    rate-card.json.sample    dollar rates example
    man1/                    manpages (e.g. man ./man1/robne.1)
    man-markdown/            the same pages as markdown (for systems without man)

## Version format

    0.<phase>.<YYYYMMDD>-<HHMM>-<sha7>

e.g. `0.17.20261120-0821-03198f1`: the phase branch the commit lives on, the
commit's UTC timestamp, and the commit's first seven hex digits — the same
short-SHA convention as the server image tags, so a robne release joins 1:1
with the server image built from the same commit. One commit = one immutable
release.

    robne version    # binary identity and JSON envelope capability
    robne --help     # command overview
    man ./man1/robne.1
