#!/usr/bin/env python3
"""Apply OpenAPI spec sync fixes from the implementation audit."""

from __future__ import annotations

import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC_PATH = ROOT / "openapi.json"

AFTER_PARAM = {
    "name": "after",
    "in": "query",
    "description": (
        "Opaque base64url cursor from a previous response's `meta.next_cursor`. "
        "When set, the server uses keyset pagination and **ignores `offset`**. "
        "See [API pagination](docs-site/pagination.md)."
    ),
    "required": False,
    "schema": {"type": "string"},
}

UNAUTHORIZED_RESPONSE = {
    "description": "Unauthorized — missing or invalid x-rh-identity header",
    "content": {
        "application/json": {
            "schema": {"$ref": "#/components/schemas/ROSAPIError"},
        }
    },
}

ERROR_RESPONSE = {
    "description": "Invalid request",
    "content": {
        "application/json": {
            "schema": {"$ref": "#/components/schemas/ROSAPIError"},
        }
    },
}

INTERNAL_ERROR_RESPONSE = {
    "description": "Internal server error",
    "content": {
        "application/json": {
            "schema": {"$ref": "#/components/schemas/ROSAPIError"},
        }
    },
}

PAGINATION_META_FIELDS = {
    "has_next": {
        "type": "boolean",
        "description": "True when another page exists after this one (keyset pagination).",
    },
    "next_cursor": {
        "type": "string",
        "nullable": True,
        "description": "Opaque cursor to pass as `after` on the next request when `has_next` is true.",
    },
    "currency": {
        "type": "string",
        "description": "ISO 4217 currency code for monetary fields (from Koku cost model). Defaults to USD.",
        "example": "USD",
    },
}

KEYSET_LIST_PATHS = [
    "/openshift/namespace/recommendations",
    "/recommendations/openshift/namespace",
    "/recommendations/openshift/namespaces",
    "/recommendations/openshift/gpu/mig",
    "/recommendations/openshift/gpu/timeslicing",
    "/recommendations/openshift/nodes",
    "/recommendations/openshift/nodes/utilization",
]

BUSINESS_HOURS_PATH_PREFIX = "/recommendations/openshift/settings/business-hours"


def has_param(op: dict, name: str) -> bool:
    return any(p.get("name") == name for p in op.get("parameters", []))


def add_after_param(op: dict) -> None:
    if has_param(op, "after"):
        return
    params = op.setdefault("parameters", [])
    limit_idx = next((i for i, p in enumerate(params) if p.get("name") == "limit"), len(params))
    params.insert(limit_idx + 1, copy.deepcopy(AFTER_PARAM))


def add_response(op: dict, code: str, response: dict) -> None:
    responses = op.setdefault("responses", {})
    if code not in responses:
        responses[code] = copy.deepcopy(response)


def merge_meta_properties(schema: dict, fields: dict) -> None:
    props = schema.setdefault("properties", {})
    meta = props.setdefault("meta", {"type": "object", "properties": {}})
    if "$ref" in meta:
        return
    meta_props = meta.setdefault("properties", {})
    for key, value in fields.items():
        meta_props.setdefault(key, copy.deepcopy(value))


def main() -> None:
    spec = json.loads(SPEC_PATH.read_text(encoding="utf-8"))
    schemas = spec.setdefault("components", {}).setdefault("schemas", {})
    responses = spec.setdefault("components", {}).setdefault("responses", {})
    responses["Unauthorized"] = UNAUTHORIZED_RESPONSE

    # /status response shape
    status_get = spec["paths"]["/status"]["get"]
    status_get["responses"]["200"]["content"]["application/json"]["schema"] = {
        "type": "object",
        "properties": {
            "api-server": {
                "type": "string",
                "example": "working",
                "description": "API server health indicator",
            }
        },
        "required": ["api-server"],
    }

    # /readyz endpoint
    spec["paths"]["/readyz"] = {
        "get": {
            "tags": ["Health"],
            "summary": "Readiness check",
            "description": "Reports readiness of the API server and its dependencies (database). Not prefixed with `/api/cost-management/v1`.",
            "operationId": "getReadyz",
            "servers": [{"url": "/"}],
            "responses": {
                "200": {
                    "description": "All readiness checks passed",
                    "content": {
                        "application/json": {
                            "schema": {
                                "type": "object",
                                "properties": {
                                    "status": {"type": "string", "example": "ok"},
                                    "checks": {
                                        "type": "object",
                                        "additionalProperties": {"type": "string"},
                                    },
                                },
                                "required": ["status", "checks"],
                            }
                        }
                    },
                },
                "503": {
                    "description": "One or more readiness checks failed",
                    "content": {
                        "application/json": {
                            "schema": {
                                "type": "object",
                                "properties": {
                                    "status": {"type": "string", "example": "error"},
                                    "checks": {
                                        "type": "object",
                                        "additionalProperties": {"type": "string"},
                                    },
                                },
                                "required": ["status", "checks"],
                            }
                        }
                    },
                },
            },
        }
    }

    # /healthz endpoint
    spec["paths"]["/healthz"] = {
        "get": {
            "tags": ["Health"],
            "summary": "Runtime health check",
            "description": "Detects runtime degradation: goroutine count, GC pause pressure, and scheduler responsiveness. Not prefixed with `/api/cost-management/v1`.",
            "operationId": "getHealthz",
            "servers": [{"url": "/"}],
            "responses": {
                "200": {
                    "description": "All runtime health checks passed",
                    "content": {
                        "application/json": {
                            "schema": {
                                "type": "object",
                                "properties": {
                                    "ok": {"type": "boolean", "example": True},
                                    "checks": {
                                        "type": "object",
                                        "additionalProperties": {"type": "string"},
                                    },
                                },
                                "required": ["ok", "checks"],
                            }
                        }
                    },
                },
                "503": {
                    "description": "One or more runtime health checks failed",
                    "content": {
                        "application/json": {
                            "schema": {
                                "type": "object",
                                "properties": {
                                    "ok": {"type": "boolean", "example": False},
                                    "checks": {
                                        "type": "object",
                                        "additionalProperties": {"type": "string"},
                                    },
                                },
                                "required": ["ok", "checks"],
                            }
                        }
                    },
                },
            },
        }
    }

    # NotificationEntry.suggested_direction
    schemas["NotificationEntry"]["properties"]["suggested_direction"] = {
        "type": "string",
        "description": "Optional remediation hint for notification code 13 (stranded resources), e.g. memory-optimized or compute-optimized.",
        "example": "memory-optimized",
    }

    # IdleDetectionSettingsResponse.settings_locked
    schemas["IdleDetectionSettingsResponse"]["properties"]["settings_locked"] = {
        "type": "boolean",
        "description": "True when ROS_SETTINGS_LOCKED prevents tenant idle-detection settings writes",
    }

    # Quota schema gaps
    qr = schemas["QuotaResourceValues"]["properties"]
    qr.setdefault(
        "storage_request_bytes",
        {"type": "integer", "format": "int64", "nullable": True},
    )
    qr.setdefault("pods", {"type": "integer", "format": "int64", "nullable": True})

    qu = schemas["QuotaUtilizationPercents"]["properties"]
    qu.setdefault(
        "storage_request_percent",
        {"type": "number", "format": "float", "nullable": True},
    )
    qu.setdefault(
        "pods_percent",
        {"type": "number", "format": "float", "nullable": True},
    )

    schemas["QuotaRecommendation"]["properties"].setdefault(
        "quota_name",
        {
            "type": "string",
            "description": "Kubernetes ResourceQuota object name",
        },
    )

    # Pagination meta on list schemas
    merge_meta_properties(schemas["NamespaceRecommendationList"], PAGINATION_META_FIELDS)
    merge_meta_properties(schemas["GPUMIGListResponse"], PAGINATION_META_FIELDS)
    merge_meta_properties(schemas["NodeUtilizationListResponse"], PAGINATION_META_FIELDS)

    gmu = schemas["GPUMIGListMeta"]["properties"]
    for key, value in PAGINATION_META_FIELDS.items():
        gmu.setdefault(key, copy.deepcopy(value))

    num = schemas["NodeUtilizationMeta"]["properties"]
    num.setdefault("has_next", copy.deepcopy(PAGINATION_META_FIELDS["has_next"]))
    num.setdefault("next_cursor", copy.deepcopy(PAGINATION_META_FIELDS["next_cursor"]))

    # Internal tags response schemas
    schemas.setdefault(
        "TagKeyCatalog",
        {
            "type": "object",
            "properties": {
                "key": {"type": "string"},
                "values": {"type": "array", "items": {"type": "string"}},
            },
            "required": ["key", "values"],
        },
    )
    schemas.setdefault(
        "TagsSyncResponse",
        {
            "type": "object",
            "properties": {
                "updated": {
                    "type": "integer",
                    "description": "Number of org_container_keys rows updated",
                }
            },
            "required": ["updated"],
        },
    )
    schemas.setdefault(
        "TagsStatusResponse",
        {
            "type": "object",
            "properties": {
                "org_id": {"type": "string"},
                "source": {"type": "string"},
                "note": {"type": "string"},
                "synced_at": {"type": "string", "format": "date-time", "nullable": True},
                "tag_keys": {
                    "type": "array",
                    "items": {"$ref": "#/components/schemas/TagKeyCatalog"},
                },
            },
            "required": ["org_id", "tag_keys"],
        },
    )

    tags_sync = spec["paths"]["/internal/tags/sync"]["post"]
    tags_sync["responses"]["200"] = {
        "description": "Tags synced",
        "content": {
            "application/json": {
                "schema": {"$ref": "#/components/schemas/TagsSyncResponse"},
            }
        },
    }
    add_response(tags_sync, "400", ERROR_RESPONSE)
    add_response(tags_sync, "500", INTERNAL_ERROR_RESPONSE)

    tags_status = spec["paths"]["/internal/tags/status"]["get"]
    tags_status["responses"]["200"] = {
        "description": "Tag sync status",
        "content": {
            "application/json": {
                "schema": {"$ref": "#/components/schemas/TagsStatusResponse"},
            }
        },
    }
    add_response(tags_status, "400", ERROR_RESPONSE)
    add_response(tags_status, "500", INTERNAL_ERROR_RESPONSE)

    # VM term settings PUT 422
    vm_terms_put = spec["paths"]["/recommendations/openshift/settings/vm/terms"]["put"]
    add_response(
        vm_terms_put,
        "422",
        {
            "description": "Term locked by administrator environment variables",
            "content": {
                "application/json": {
                    "schema": {"$ref": "#/components/schemas/ROSAPIError"},
                }
            },
        },
    )

    # Keyset `after` on list endpoints
    for path in KEYSET_LIST_PATHS:
        op = spec["paths"].get(path, {}).get("get")
        if op:
            add_after_param(op)

    # Business-hours routes: add 503
    for path, methods in spec["paths"].items():
        if not path.startswith(BUSINESS_HOURS_PATH_PREFIX):
            continue
        for op in methods.values():
            if isinstance(op, dict) and "responses" in op:
                add_response(
                    op,
                    "503",
                    {
                        "description": "Service unavailable due to a database error",
                        "content": {
                            "application/json": {
                                "schema": {"$ref": "#/components/schemas/ROSAPIError"},
                            }
                        },
                    },
                )

    # Authenticated v1 routes: add 401 via shared component
    for path, methods in spec["paths"].items():
        if path in ("/status", "/healthz", "/readyz"):
            continue
        if path.startswith("/internal/"):
            continue
        for op in methods.values():
            if not isinstance(op, dict):
                continue
            if "responses" not in op:
                continue
            add_response(op, "401", {"$ref": "#/components/responses/Unauthorized"})

    SPEC_PATH.write_text(json.dumps(spec, indent=2) + "\n", encoding="utf-8")
    print(f"Updated {SPEC_PATH}")


if __name__ == "__main__":
    main()
