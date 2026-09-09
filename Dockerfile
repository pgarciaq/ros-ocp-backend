FROM registry.access.redhat.com/ubi10/go-toolset:1.26 AS builder
WORKDIR /go/src/app
COPY . .
USER 0
# Processor/API/housekeeper share this binary and import confluent-kafka-go
# (librdkafka). CGO_ENABLED=1 is required. Downstream FIPS builds also use
# CGO=1 (golang-fips → OpenSSL). Do not set CGO_ENABLED=0 here; that flag is
# only valid for the robne CLI (Makefile `robne` target).
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o rosocp rosocp.go && \
    echo "$(go version)" > go_version_details

FROM registry.access.redhat.com/ubi10/ubi-minimal:latest
WORKDIR /
# install (not reinstall): ubi10-minimal does not ship tzdata, and reinstall
# fails on an absent package; install is a no-op where already present (#562).
RUN microdnf -y update \
    --disableplugin=subscription-manager && \
    microdnf -y install tzdata \
    --disableplugin=subscription-manager && \
    microdnf clean all
COPY --from=builder /go/src/app/rosocp ./rosocp
COPY --from=builder /go/src/app/go_version_details ./go_version_details
COPY migrations ./migrations
COPY openapi.json ./openapi.json
COPY resource_optimization_openshift.json ./resource_optimization_openshift.json
USER 1001
