FROM registry.access.redhat.com/ubi10/go-toolset:1.25 AS builder
WORKDIR /go/src/app
COPY . .
USER 0
# CGO_ENABLED=0: upstream-only; downstream FIPS builds intentionally use CGO_ENABLED=1.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o rosocp rosocp.go && \
    echo "$(go version)" > go_version_details

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
WORKDIR /
RUN microdnf -y update \
    --disableplugin=subscription-manager && \
    microdnf -y reinstall tzdata \
    --disableplugin=subscription-manager && \
    microdnf clean all
COPY --from=builder /go/src/app/rosocp ./rosocp
COPY --from=builder /go/src/app/go_version_details ./go_version_details
COPY migrations ./migrations
COPY openapi.json ./openapi.json
COPY resource_optimization_openshift.json ./resource_optimization_openshift.json
USER 1001
