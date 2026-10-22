# Running targeted ROS HCP IQE tests without Nexus

**Scope:** the three read-only HCP management-scope API tests in
`iqe_ros_ocp/tests/rest/test_hcp_recommendations.py`. This is not a recipe for
the full Cost Management or ROS IQE suites.

**Validation status (2026-10-22):** the targeted run passed **3 tests** with
**5 deselected** using the Quay IQE Core image, local IQE plugin sources, and
public PyPI. It ran without Vault SSO. This validates only the three tests
listed below, not the full plugin suite. See
[ros-ocp-backend #698](https://github.com/pgarciaq/ros-ocp-backend/issues/698)
for feasibility notes.

## Nexus access and VPN access are separate

- **Nexus-free:** use the prebuilt IQE Core image and local checkouts of the
  internal IQE plugins. Set pip/uv indexes explicitly to public PyPI for
  public dependencies. IQE packages are not generally available from public
  PyPI.
- **Not necessarily VPN-free:** private GitLab checkouts and Quay may require
  internal network access. More importantly, the SNO API is on the internal
  lab network. Reaching it requires the VPN/sshuttle path in
  [SNO Cluster Operations](../agents/sno-cluster-operations.md). Pre-staging
  images and source checkouts does not provide a route to the cluster. If the
  target API cannot be reached, stop rather than trying alternate auth or
  network workarounds.

For a genuinely disconnected setup, stage the image, internal source checkouts,
and public dependency artifacts beforehand, and use only a test target that is
reachable from the disconnected host. That case has not been validated here.

## What this run covers

Run only these tests when validating management scope against a shared SNO:

| Test | Check |
|---|---|
| `test_hcp_list_scope_management` | Management list rows do not carry hosted-cluster attribution |
| `test_hcp_list_scope_unknown_rejected` | Unknown scope returns HTTP 400 |
| `test_hcp_detail_scope_management_gate` | Management detail is returned with management scope and returns 404 without it |

They issue GET requests. Do not replace the selection with the full plugin
suite: other IQE tests create sources, upload reports, change settings, or
otherwise write persistent test data.

## Prerequisites

- Linux/amd64 host with Podman; the pinned IQE Core image is `linux/amd64`.
- Access to `quay.io/cloudservices/iqe-core` and public PyPI.
- Local source checkouts for `iqe-cost-management-plugin`,
  `iqe-ros-ocp-plugin`, `iqe-sources-plugin`, `iqe-platform-ui-plugin`,
  `iqe-rbac-plugin`, `iqe-report-portal-plugin`, `iqe-settings-plugin`,
  `iqe-bindings`, and `iqe-service-accounts-plugin`.
- The local Nise fork, with its `5.4.0` tag available. The Cost Management
  plugin pins `koku-nise==5.4.0`; do not install the current Nise working tree
  if it is at another version.
- For SNO execution: read the linked SNO operations guide, connect VPN/start
  sshuttle, and verify `oc whoami` before continuing. The SNO API must have
  management-scope recommendation data for the list/detail checks not to skip.

The inspected Quay image is `iqe-core==26.10.07.0` and was pinned by this
manifest digest on 2026-10-22:

```bash
CORE_IMAGE='quay.io/cloudservices/iqe-core@sha256:2380a23a57321d4b8400a4ed686c6c9ecd7804255c4a05bf6f928719b724b3a0'
podman pull "$CORE_IMAGE"
```

Quay availability without VPN depends on the host's registry access; do not
assume it from the fact that Nexus is not used.

## Vault SSO for this targeted run

Vault SSO is **not** required by these HCP API tests. The IQE Core image's
global defaults enable `IQE_VAULT_LOADER_ENABLED`, which starts Vault OIDC
authentication during runner startup. The broader QE suites use Vault for
stage/prod user credentials and cloud-service secrets; these tests use the
local test identity below and make read-only API requests.

Disable the loader only for this local, targeted run by passing
`DYNACONF_IQE_VAULT_LOADER_ENABLED=false` to the IQE command. This leaves
unrequested Vault-backed settings unavailable (IQE may warn about them). Do not
use this override for tests that need those secrets.

## Prepare the local source tree

Set `IQE_SRC_ROOT` to the directory containing the IQE checkouts. Make a
temporary snapshot so package builds cannot modify the original clones. The
Nise snapshot must be checked out at the pinned tag:

```bash
IQE_SRC_ROOT="$HOME/dev/koku"
STAGE="$(mktemp -d /tmp/iqe-hcp-src.XXXXXX)"
set -euo pipefail

for repo in iqe-cost-management-plugin iqe-ros-ocp-plugin \
            iqe-platform-ui-plugin iqe-rbac-plugin iqe-report-portal-plugin \
            iqe-settings-plugin iqe-bindings iqe-service-accounts-plugin; do
  git clone --quiet --local --no-hardlinks "$IQE_SRC_ROOT/$repo" "$STAGE/$repo"
done
if [[ -d "$IQE_SRC_ROOT/iqe-sources-plugin/.git" ]]; then
  git clone --quiet --local --no-hardlinks \
    "$IQE_SRC_ROOT/iqe-sources-plugin" "$STAGE/iqe-sources-plugin"
else
  # This private checkout may require VPN. Pre-stage it before disconnecting.
  git clone --quiet --depth 1 \
    https://gitlab.cee.redhat.com/insights-qe/iqe-sources-plugin.git \
    "$STAGE/iqe-sources-plugin"
fi
git clone --quiet --local --no-hardlinks --branch 5.4.0 --single-branch \
  "$IQE_SRC_ROOT/nise" "$STAGE/nise-5.4.0"
```

If a required private checkout is missing, obtain it through the approved
internal source path before disconnecting from VPN. Do not substitute an
upstream PyPI package for the local Nise fork.

## Start an isolated runner

Create a temporary directory containing the `settings.local.yaml` and
`cost_management.local.yaml` shown below, then start the image with the source
and config directories mounted. `:Z` is needed on SELinux hosts; the runner
uses host networking so it can reach the host's `oc port-forward` listener.

```bash
CONFIG_DIR="$(mktemp -d /tmp/iqe-hcp-config.XXXXXX)"
# Save the settings.local.yaml from the next section to this directory first.
podman run -d --name iqe-hcp-runner --network host \
  -v "$STAGE:/source:ro,Z" \
  -v "$CONFIG_DIR:/iqe-local-config:ro,Z" \
  --entrypoint /bin/bash "$CORE_IMAGE" -lc 'sleep infinity'

# Copy the read-only mount to the container's writable temp area. Hatch may
# generate a version file while building some local plugin wheels.
podman exec iqe-hcp-runner /bin/bash -lc \
  'mkdir -p /tmp/iqe-src && cp -R /source/. /tmp/iqe-src/'
```

Install internal packages from those local copies with dependency resolution
disabled, and explicitly direct public packages to PyPI:

```bash
podman exec -e PIP_INDEX_URL=https://pypi.org/simple \
  -e UV_INDEX_URL=https://pypi.org/simple \
  iqe-hcp-runner /bin/bash -lc '
    set -e
    P=/iqe_venv/bin/python
    $P -m pip install --index-url https://pypi.org/simple hatchling hatch-vcs
    $P -m pip install --index-url https://pypi.org/simple \
      --no-deps --no-build-isolation \
      /tmp/iqe-src/iqe-bindings \
      /tmp/iqe-src/iqe-platform-ui-plugin \
      /tmp/iqe-src/iqe-report-portal-plugin \
      /tmp/iqe-src/iqe-service-accounts-plugin \
      /tmp/iqe-src/iqe-rbac-plugin \
      /tmp/iqe-src/iqe-settings-plugin \
      /tmp/iqe-src/iqe-cost-management-plugin \
      /tmp/iqe-src/iqe-sources-plugin \
      /tmp/iqe-src/nise-5.4.0 \
      /tmp/iqe-src/iqe-ros-ocp-plugin
  '

podman exec -e PIP_INDEX_URL=https://pypi.org/simple \
  -e UV_INDEX_URL=https://pypi.org/simple \
  iqe-hcp-runner /bin/bash -lc '
    /iqe_venv/bin/python -m pip install --index-url https://pypi.org/simple \
      openshift statsmodels wait_for PyGithub python-sketch \
      axe-playwright-python widgetastic.patternfly5 pypdf faker \
      pytest-lazy-fixture tenacity nulltype reportportal-client xmltodict \
      pytest-reportportal tfacon google-api-python-client google-auth msal \
      slack-sdk splunk-sdk azure-identity==1.25.3 azure-mgmt-core==1.6.0 \
      azure-mgmt-costmanagement==5.0.0 azure-storage-blob \
      google-cloud-storage google-cloud-bigquery
  '
```

These are the declared public dependencies used by the selected plugin set;
the Nise cloud-provider SDKs are included so its distribution dependencies are
present as well. Do not let pip fall back to the image's Nexus index. The full
source-plugin dependency graph is not guaranteed to resolve this way:
`openapi-validator` is an internal dependency absent from public PyPI and the
local checkouts. It is used by the source plugin's own OpenAPI tests, not by
the three HCP tests listed above. This runbook does not cover the source plugin
suite or claim a fully resolved environment for every IQE test.

## Configure the API target

Create `$CONFIG_DIR/settings.local.yaml` and substitute the identity for the
test tenant available on the target cluster. IQE Core 26.10 uses the nested
`attributes` / `auth` user format; this non-secret example targets the standard
on-prem development tenant. The password is required by the IQE user model but
is unused for `default_auth_type: identity`:

```yaml
local:
  default_user: default
  cost_management:
    local_sources: false
    use_sources: false
    wait_with_nise: false
  main:
    hostname: "127.0.0.1:18484"
    scheme: http
  http:
    default_auth_type: identity
  users:
    default:
      attributes:
        type: User
        account_number: "10001"
        org_id: "1234567"
        email: admin@example.com
        first_name: User
        last_name: Dev
        is_active: true
        is_org_admin: true
        is_internal: false
        locale: en_US
      auth:
        username: admin
        password: local-test-unused
        user_id: "10001"
      entitlements:
        cost_management:
          is_entitled: true
```

The Cost Management plugin's own `local` config overrides the global API host,
so also create `$CONFIG_DIR/cost_management.local.yaml`:

```yaml
local:
  main:
    hostname: "127.0.0.1:18484"
    scheme: http
  http:
    default_auth_type: identity
```

Without this plugin-specific file, its bundled local default points to
`localhost:8000`, not the port-forward below.

In a separate terminal, forward the ROS API service. Use the service/namespace
for the target environment if they differ:

```bash
oc port-forward -n cost-onprem svc/cost-onprem-ros-api 18484:8000
```

## Run only the HCP management-scope tests

```bash
podman exec \
  -e ENV_FOR_DYNACONF=local \
  -e IQE_ADDITIONAL_CONF_PATH=/iqe-local-config \
  -e DYNACONF_IQE_VAULT_LOADER_ENABLED=false \
  iqe-hcp-runner \
  /iqe_venv/bin/iqe tests plugin ros_ocp \
  /iqe_venv/lib64/python3.12/site-packages/iqe_ros_ocp/tests/rest/test_hcp_recommendations.py \
  -k 'test_hcp_list_scope_management or test_hcp_list_scope_unknown_rejected or test_hcp_detail_scope_management_gate'
```

The explicit file argument is intentional. In the inspected local plugin
checkout, running `-k` against the whole plugin also collected unrelated
`test_boxplots.py` (unresolved conflict marker) and
`test_gpu_recommendations.py` (missing `RECOMMENDATIONS` import), preventing
collection. The command above selected and ran the three HCP tests: **3 passed,
5 deselected**. Do not infer success from a preflight GET or collection alone.

## Cleanup

Stop the port-forward with Ctrl-C, then remove the temporary runner:

```bash
podman rm -f iqe-hcp-runner
```

After confirming they are the temporary paths created for this run, remove
`$STAGE` and `$CONFIG_DIR` if they are no longer needed. The pulled image can
remain cached for later Nexus-free runs. Do not remove other containers or
user source checkouts as part of cleanup.

## Related

- [SNO Cluster Operations](../agents/sno-cluster-operations.md) — VPN,
  sshuttle, and cluster access prerequisites.
- [IQE requirement registration](iqe-requirements-registration.md) — plugin
  requirement metadata and CI filtering.
- [Spike #698](https://github.com/pgarciaq/ros-ocp-backend/issues/698) —
  feasibility evidence and the current test result.
