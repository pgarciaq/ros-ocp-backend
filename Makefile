include scripts/.env

UNAME_S := $(shell uname -s)

identity={"identity": {"org_id": "3340851", "type": "System", "auth_type": "cert-auth", "system": {"cn": "1b36b20f-7fa0-4454-a6d2-008294e06378", "cert_type": "system"}, "internal": {"org_id": "3340851", "auth_time": 6300}}}
ifeq ($(UNAME_S),Darwin)
    b64_identity=$(shell echo '${identity}' | base64)
else
    b64_identity=$(shell echo '${identity}' | base64 -w 0 -)
endif

ros_ocp_msg='{"request_id": "uuid1234", "b64_identity": "test", "metadata": {"org_id": "3340851", "source_id": "111", "cluster_uuid": "1b36b20f-7fa0-4454-a6d2-008294e06378", "cluster_alias": "name222"}, "files": ["http://localhost:8888/ros-ocp-usage.csv"]}'
ros_ocp_msg_24Hrs='{"request_id": "uuid1234", "b64_identity": "test", "metadata": {"org_id": "3340851", "source_id": "111", "cluster_uuid": "1b36b20f-7fa0-4454-a6d2-008294e06378", "cluster_alias": "name222"}, "files": ["http://localhost:8888/ros-ocp-usage-24Hrs.csv"]}'
ros_ocp_namespace_msg='{"request_id": "uuid1234", "b64_identity": "test", "metadata": {"org_id": "3340851", "source_id": "111", "cluster_uuid": "1b36b20f-7fa0-4454-a6d2-008294e06378", "cluster_alias": "name222"}, "files": ["http://localhost:8888/ros_ocp_namespace.csv"]}'
file=./scripts/samples/cost-mgmt.tar.gz
CSVfile=./scripts/samples/ros-ocp-usage.csv
CSVfile_name_tuple := $(subst /, ,$(CSVfile:%=%))
CSVfile_name := $(word 4,$(CSVfile_name_tuple))
INGRESS_PORT ?= 3000

ifdef env
	short_env=$(shell echo '${env}' | cut -d'-' -f2)
	server=$(shell oc get clowdenvironments env-ephemeral-${short_env} -o=jsonpath='{.status.hostname}')
	username=$(shell oc get secret env-ephemeral-${short_env}-keycloak -n ephemeral-${short_env} -o=jsonpath='{.data.defaultUsername}' | base64 -d)
	password=$(shell oc get secret env-ephemeral-${short_env}-keycloak -n ephemeral-${short_env} -o=jsonpath='{.data.defaultPassword}' | base64 -d)
	auth_header=$(shell echo -n '${username}:${password}' | base64)
	minio_accessKey=$(shell oc get secret env-ephemeral-${short_env}-minio -o=jsonpath='{.data.accessKey}' | base64 -d)
	minio_secretKey=$(shell oc get secret env-ephemeral-${short_env}-minio -o=jsonpath='{.data.secretKey}' | base64 -d)
endif

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
ifeq (,$(wildcard $(LOCALBIN)))
	@echo "🤖 Ensuring $(LOCALBIN) is available"
	mkdir -p $(LOCALBIN)
	@echo "✅ Done"
endif

.PHONY: golangci-lint
GOLANGCILINT := $(LOCALBIN)/golangci-lint
GOLANGCI_URL := https://golangci-lint.run/install.sh
start_date := 1970-01-01
GOLANGCI_VERSION := latest

golangci-lint: $(LOCALBIN)
ifeq (,$(wildcard $(GOLANGCILINT)))
	@ echo "📥 Downloading golangci-lint"
	curl -sSfL $(GOLANGCI_URL) | sh -s -- -b $(LOCALBIN) $(GOLANGCI_VERSION)
	@ echo "✅ Done"
endif


.PHONY: install-golang-migrate-cli-tool
install-golang-migrate-cli-tool: $(LOCALBIN)
	curl -L https://github.com/golang-migrate/migrate/releases/download/v4.15.2/migrate.linux-amd64.tar.gz | tar xvz -C $(LOCALBIN) migrate


.PHONY: db-migrate
db-migrate:
	go run rosocp.go db migrate up

.PHONY: run-processor
run-processor:
	PROMETHEUS_PORT=5005 go run rosocp.go start processor

.PHONY: run-recommender
run-recommendation-poller:
	PROMETHEUS_PORT=5006 go run rosocp.go start recommendation-poller

.PHONY: run-api-server
run-api-server:
	PROMETHEUS_PORT=5007 go run rosocp.go start api

.PHONY: build
build:
	go build -o bin/rosocp rosocp.go

.PHONY: lint
lint: golangci-lint
	$(GOLANGCILINT) run --timeout=3m ./...

.PHONY: test
test:
	go test -v -race -count=1 ./...

MCCILINT := $(LOCALBIN)/mc
.PHONY: archive-to-minio
archive-to-minio:
ifdef env
	-oc expose svc env-${env}-minio -n ${env}
ifeq (,$(wildcard $(MCCILINT)))
	@ echo "📥 Downloading minio client"
    ifeq ($(UNAME_S),Darwin)
		curl https://dl.min.io/client/mc/release/darwin-amd64/mc --create-dirs -o $(MCCILINT)
    else
		curl https://dl.min.io/client/mc/release/linux-amd64/mc --create-dirs -o $(MCCILINT)
    endif
	chmod +x $(MCCILINT)
	@ echo "✅ Done"
endif
	bin/mc alias set myminio http://env-${env}-minio-${env}.apps.crc-eph.r9lp.p1.openshiftapps.com ${minio_accessKey} ${minio_secretKey}
	bin/mc cp ${CSVfile} myminio/insights-upload-perma/
	sleep 5
	$(eval SHAREURL=$(shell bin/mc share download --json myminio/insights-upload-perma/${CSVfile_name} | jq -r '.share'))
	$(eval KAFKAPOD=$(shell oc get pods -o custom-columns=POD:.metadata.name --no-headers -n ${env} | grep kafka))
	$(eval ros_ocp_msg_ephemeral = '{\"request_id\": \"uuid1234\", \"b64_identity\": \"test\", \"metadata\": {\"org_id\": \"3340851\", \"source_id\": \"111\", \"cluster_uuid\": \"222\", \"cluster_alias\": \"name222\"}, \"files\": [\"$(SHAREURL)\"]}')
	oc exec ${KAFKAPOD} -n ${env} -- /bin/bash -c "echo ${ros_ocp_msg_ephemeral} | /opt/kafka/bin/kafka-console-producer.sh --topic hccm.ros.events   --broker-list localhost:9092"
else
	@ echo "Env not defined"
endif

upload-msg-to-rosocp:
	echo ${ros_ocp_msg} | docker compose -f scripts/docker-compose.yml exec -T kafka kafka-console-producer --topic hccm.ros.events  --broker-list localhost:29092

upload-ns-msg-to-rosocp:
	echo ${ros_ocp_namespace_msg} | docker compose -f scripts/docker-compose.yml exec -T kafka kafka-console-producer --topic hccm.ros.events  --broker-list localhost:29092


upload-msg-to-rosocp-24Hrs:
	echo ${ros_ocp_msg_24Hrs} | docker compose -f scripts/docker-compose.yml exec -T kafka kafka-console-producer --topic hccm.ros.events  --broker-list localhost:29092


get-recommendations:
ifdef env
	$(eval APIPOD=$(shell oc get pods -o custom-columns=POD:.metadata.name --no-headers -n ${env} | grep ros-ocp-backend-api))
	oc exec ${APIPOD} -c ros-ocp-backend-api -n ${env} -- /bin/bash -c 'curl -v -H "X-Rh-Identity: ${b64_identity}" -H "x-rh-request_id: testtesttest" http://localhost:8000/api/cost-management/v1/recommendations/openshift?start_date=${start_date}' | python -m json.tool
else
	curl -v -H "x-rh-identity: ${b64_identity}" \
		 -H "x-rh-request_id: testtesttest" \
		 http://localhost:8000/api/cost-management/v1/recommendations/openshift?start_date=${start_date} | python -m json.tool
endif

get-ns-recommendations:
ifdef env
	$(eval APIPOD=$(shell oc get pods -o custom-columns=POD:.metadata.name --no-headers -n ${env} | grep ros-ocp-backend-api))
	oc exec ${APIPOD} -c ros-ocp-backend-api -n ${env} -- /bin/bash -c 'curl -v -H "X-Rh-Identity: ${b64_identity}" -H "x-rh-request_id: testtesttest" http://localhost:8000/api/cost-management/v1/openshift/namespace/recommendations?start_date=${start_date}' | python -m json.tool
else
	curl -v -H "x-rh-identity: ${b64_identity}" \
		 -H "x-rh-request_id: testtesttest" \
		 "http://localhost:8000/api/cost-management/v1/openshift/namespace/recommendations?start_date=${start_date}" | python -m json.tool
endif

get_unleash_features:
	curl -H "Authorization: rosocp:dev.token" http://localhost:3063/api/client/features && echo

get-openapi:
	curl -v -H "x-rh-identity: ${b64_identity}" -H "x-rh-request_id: testtesttest" http://localhost:8000/api/cost-management/v1/recommendations/openshift/openapi.json

# --- Container Image ---

IMAGE_NAME ?= ros-ocp-backend
IMAGE_TAG ?= latest
CONTAINER_RUNTIME ?= $(shell command -v podman 2>/dev/null || echo docker)

.PHONY: docker-build
docker-build: ## Build container image
	$(CONTAINER_RUNTIME) build -t $(IMAGE_NAME):$(IMAGE_TAG) .

.PHONY: docker-push
docker-push: ## Push container image (set IMAGE_REGISTRY)
ifdef IMAGE_REGISTRY
	$(CONTAINER_RUNTIME) tag $(IMAGE_NAME):$(IMAGE_TAG) $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	$(CONTAINER_RUNTIME) push $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
else
	@echo "Set IMAGE_REGISTRY to push (e.g., make docker-push IMAGE_REGISTRY=quay.io/myuser)"
	@exit 1
endif

# --- Documentation ---

.PHONY: docs-install
docs-install: ## Install documentation dependencies (gomarkdoc, mkdocs-material)
	go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
	pip install --quiet mkdocs-material mkdocs-section-index

.PHONY: docs-generate
docs-generate: ## Regenerate API reference from source code
	./scripts/generate-docs.sh

.PHONY: docs-build
docs-build: docs-generate ## Build the static documentation site (→ _site/)
	mkdocs build --config-file mkdocs.yml --site-dir _site

.PHONY: docs-serve
docs-serve: docs-generate ## Serve docs locally with live reload (http://localhost:8000)
	mkdocs serve --config-file mkdocs.yml

.PHONY: docs-clean
docs-clean: ## Remove generated docs and build output
	rm -f docs-site/api-reference/plugin.md docs-site/api-reference/container.md
	rm -f docs-site/api-reference/gpu.md docs-site/api-reference/node.md
	rm -f docs-site/api-reference/pvc.md docs-site/api-reference/namespace.md
	rm -f docs-site/api-reference/snapshot.md docs-site/api-reference/kruize.md
	rm -f docs-site/api-reference/example.md
	rm -f docs-site/contributing.md docs-site/known-issues.md
	rm -rf docs-site/architecture/ docs-site/operations/ docs-site/features/
	rm -rf _site

# --- Help ---

.PHONY: help
help: ## Show all available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
