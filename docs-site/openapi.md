# API Specification

The ROS-OCP Backend API is documented using the OpenAPI 3.0 specification.

## Viewing the Specification

The authoritative spec is [`openapi.json`](../openapi.json) at the repository root.

You can view it interactively using:

- **Swagger UI**: Paste the raw URL into [editor.swagger.io](https://editor.swagger.io)
- **Redoc**: Use [redocly.github.io/redoc](https://redocly.github.io/redoc/?url=https://raw.githubusercontent.com/pgarciaq/ros-ocp-backend/pgarciaq-rosocp-superpowers-phase9/openapi.json)
- **Local**: `npx @redocly/cli preview-docs openapi.json`

## Key Endpoints

| Group | Path | Method | Description |
|-------|------|--------|-------------|
| Containers | `/recommendations/openshift/workloads` | GET | Container recommendations |
| GPU | `/recommendations/openshift/gpu` | GET | GPU utilization summary |
| GPU | `/recommendations/openshift/gpu/timeslicing` | GET | Time-slicing recommendations |
| GPU | `/recommendations/openshift/gpu/mig` | GET | MIG partition recommendations |
| Nodes | `/recommendations/openshift/nodes` | GET | Node utilization recommendations |
| PVCs | `/recommendations/openshift/pvcs` | GET | PVC right-sizing recommendations |
| Namespaces | `/recommendations/openshift/namespaces` | GET | Namespace quota recommendations |
| Snapshots | `/recommendations/openshift/snapshots` | GET | Stale snapshot list |
| Settings | `/recommendations/openshift/settings/terms` | GET/PUT/DELETE | Term configuration |
| Settings | `/recommendations/openshift/settings/capabilities` | GET | Plugin capabilities |
| Settings | `/recommendations/openshift/settings/snapshot` | GET/PUT | Snapshot staleness threshold |

## Authentication

All endpoints require the `x-rh-identity` header (base64-encoded JSON identity).
See the [API Versioning](architecture/api-versioning.md) doc for compatibility policy.
