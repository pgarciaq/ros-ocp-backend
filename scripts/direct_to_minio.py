#!/usr/bin/env python3
"""Upload nise-generated ROS CSVs directly to MinIO and publish Kafka messages.

Bypasses the Koku listener for ROS processor benchmarks. The Koku listener
passes ROS CSVs through as-is — it only copies them to MinIO and sends a
Kafka notification. This script does exactly the same two operations
directly, saving hours of listener time (Parquet conversion, PostgreSQL
writes, summarization) that are irrelevant to ROS processor performance.

Data flow comparison:

  Full pipeline:  nise → tarball → ingress → Koku listener → MinIO + Kafka → ROS processor
                                              ~~~hours~~~

  Direct mode:    nise → this script → MinIO + Kafka → ROS processor
                                       ~~~minutes~~~

Prerequisites (install in the pod where this script runs):
    pip install boto3 kafka-python

Usage:
    python3 direct_to_minio.py \\
        --data-dir /data/nise_output \\
        --cluster-uuid <uuid> \\
        --cluster-alias <name> \\
        --provider-uuid <uuid>

Environment variables (override defaults for non-standard deployments):
    MINIO_ENDPOINT       MinIO URL            (default: http://minio.cost-onprem.svc.cluster.local:9000)
    MINIO_ACCESS_KEY     MinIO access key      (default: from cost-onprem-storage-credentials secret)
    MINIO_SECRET_KEY     MinIO secret key      (default: from cost-onprem-storage-credentials secret)
    KAFKA_BOOTSTRAP      Kafka bootstrap       (default: cost-onprem-kafka-kafka-bootstrap.kafka.svc.cluster.local:9092)
    KAFKA_TOPIC          Kafka topic           (default: hccm.ros.events)
    ROS_BUCKET           S3 bucket name        (default: ros-data)
    SCHEMA_NAME          Tenant schema         (default: org1234567)
    ORG_ID               Org ID                (default: 1234567)
    ACCOUNT_NUMBER       Account number        (default: 10001)
    PRESIGN_EXPIRY       Presigned URL expiry  (default: 172800 = 48 hours)
"""

import argparse
import base64
import glob
import json
import logging
import os
import sys
import time
import uuid
from datetime import datetime
from pathlib import Path

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
log = logging.getLogger("direct_to_minio")

# ROS-relevant filename prefixes (matched by the ROS processor's DetermineCSVType)
ROS_FILE_PATTERNS = [
    "ocp_ros_usage",
    "ocp_ros_namespace",
    "ocp_ros_vm_gpu_device",
    "ocp_ros_vm_usage",
    "ocp_ros_cluster_quota",
    "ocp_storage_usage",
    "ocp_snapshot_inventory",
]

DEFAULT_MINIO_ENDPOINT = "http://minio.cost-onprem.svc.cluster.local:9000"
DEFAULT_KAFKA_BOOTSTRAP = "cost-onprem-kafka-kafka-bootstrap.kafka.svc.cluster.local:9092"
DEFAULT_KAFKA_TOPIC = "hccm.ros.events"
DEFAULT_ROS_BUCKET = "ros-data"
DEFAULT_SCHEMA = "org1234567"
DEFAULT_ORG_ID = "1234567"
DEFAULT_ACCOUNT = "10001"
DEFAULT_PRESIGN_EXPIRY = 172800  # 48 hours


def is_ros_file(filename: str) -> bool:
    """Check if a filename matches a ROS-relevant pattern."""
    basename = os.path.basename(filename).lower()
    return any(pat in basename for pat in ROS_FILE_PATTERNS)


def find_ros_csvs(data_dir: str) -> dict[str, list[Path]]:
    """Walk data_dir and return {date_label: [csv_paths]} for ROS files.

    nise --write-monthly output structure:
        <data_dir>/<cluster-uuid>/YYYYMMDD-YYYYMMDD/<files>.csv

    Each YYYYMMDD-YYYYMMDD directory becomes one manifest (one Kafka message).
    The date label is extracted from the directory name.
    """
    result: dict[str, list[Path]] = {}
    data_path = Path(data_dir)

    csv_files = sorted(data_path.rglob("*.csv"))
    if not csv_files:
        log.error("No CSV files found in %s", data_dir)
        sys.exit(1)

    for csv_file in csv_files:
        if not is_ros_file(csv_file.name):
            continue
        date_label = csv_file.parent.name
        result.setdefault(date_label, []).append(csv_file)

    if not result:
        log.error(
            "No ROS-relevant CSV files found in %s. "
            "Expected files matching: %s",
            data_dir,
            ", ".join(ROS_FILE_PATTERNS),
        )
        sys.exit(1)

    total_files = sum(len(v) for v in result.values())
    log.info(
        "Found %d ROS CSV files across %d date ranges in %s",
        total_files,
        len(result),
        data_dir,
    )
    return result


def build_s3_key(schema: str, provider_uuid: str, date_str: str, filename: str) -> str:
    """Build the S3 object key matching the Koku listener's ROSReportShipper pattern."""
    return f"{schema}/source={provider_uuid}/date={date_str}/{filename}"


def upload_single_file(
    s3_client,
    transfer_config,
    bucket: str,
    csv_path: Path,
    key: str,
    manifest_id: str,
    max_retries: int = 3,
) -> None:
    """Upload a single file with retry logic."""
    for attempt in range(max_retries):
        try:
            s3_client.upload_file(
                str(csv_path),
                bucket,
                key,
                ExtraArgs={"Metadata": {"ManifestId": manifest_id}},
                Config=transfer_config,
            )
            return
        except Exception as e:
            if attempt < max_retries - 1:
                wait = 2 ** attempt
                log.warning("  Retry %d/%d for %s: %s (waiting %ds)", attempt + 1, max_retries, csv_path.name, e, wait)
                time.sleep(wait)
            else:
                raise


def upload_and_presign(
    s3_client,
    transfer_config,
    bucket: str,
    csv_files: list[Path],
    schema: str,
    provider_uuid: str,
    date_str: str,
    presign_expiry: int,
    manifest_id: str,
) -> tuple[list[str], list[str]]:
    """Upload CSV files to MinIO and return (presigned_urls, object_keys)."""
    presigned_urls = []
    object_keys = []

    for i, csv_path in enumerate(csv_files, 1):
        key = build_s3_key(schema, provider_uuid, date_str, csv_path.name)
        if i % 100 == 0 or i == len(csv_files):
            log.info("  Uploading %d/%d: %s", i, len(csv_files), csv_path.name)

        upload_single_file(s3_client, transfer_config, bucket, csv_path, key, manifest_id)

        url = s3_client.generate_presigned_url(
            "get_object",
            Params={"Bucket": bucket, "Key": key},
            ExpiresIn=presign_expiry,
        )
        presigned_urls.append(url)
        object_keys.append(key)

    return presigned_urls, object_keys


def build_kafka_message(
    request_id: str,
    manifest_id: str,
    cluster_uuid: str,
    cluster_alias: str,
    provider_uuid: str,
    org_id: str,
    account: str,
    source_id: str,
    presigned_urls: list[str],
    object_keys: list[str],
    expected_files: list[str],
) -> dict:
    """Build the Kafka message matching the ROS processor's KafkaMsg struct."""
    identity = {
        "identity": {
            "account_number": account,
            "org_id": org_id,
            "type": "User",
            "user": {
                "username": "benchmark",
                "email": "benchmark@test.com",
                "is_org_admin": True,
            },
        },
        "entitlements": {"cost_management": {"is_entitled": True}},
    }
    b64_identity = base64.b64encode(json.dumps(identity).encode()).decode()

    return {
        "request_id": request_id,
        "b64_identity": b64_identity,
        "metadata": {
            "account": account,
            "org_id": org_id,
            "source_id": source_id,
            "provider_uuid": provider_uuid,
            "cluster_uuid": cluster_uuid,
            "operator_version": "benchmark-direct",
            "cluster_alias": cluster_alias,
            "manifest_id": manifest_id,
            "expected_files": expected_files,
        },
        "files": presigned_urls,
        "object_keys": object_keys,
    }


def parse_date_from_range(date_range: str) -> str:
    """Extract a YYYY-MM-DD date from a nise date range like '20260601-20260701'.

    Uses the start date of the range.
    """
    try:
        start = date_range.split("-")[0]
        dt = datetime.strptime(start, "%Y%m%d")
        return dt.strftime("%Y-%m-%d")
    except (ValueError, IndexError):
        return datetime.now().strftime("%Y-%m-%d")


def main():
    parser = argparse.ArgumentParser(
        description="Upload nise ROS CSVs directly to MinIO and publish Kafka messages",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    parser.add_argument(
        "--data-dir",
        required=True,
        help="Path to nise output directory (contains CSV files or cluster-uuid subdirs)",
    )
    parser.add_argument("--cluster-uuid", required=True, help="Cluster UUID (must be valid UUID v4)")
    parser.add_argument("--cluster-alias", required=True, help="Cluster alias/name")
    parser.add_argument(
        "--provider-uuid",
        required=True,
        help="Provider UUID (from Koku source registration, or any valid UUID for direct mode)",
    )
    parser.add_argument("--source-id", default="1", help="Source ID string (default: 1)")
    parser.add_argument("--dry-run", action="store_true", help="Show what would be done without uploading")
    args = parser.parse_args()

    # Validate cluster UUID
    try:
        uuid.UUID(args.cluster_uuid, version=4)
    except ValueError:
        log.error("--cluster-uuid must be a valid UUID v4, got: %s", args.cluster_uuid)
        sys.exit(1)

    # Config from environment
    minio_endpoint = os.environ.get("MINIO_ENDPOINT", DEFAULT_MINIO_ENDPOINT)
    minio_access_key = os.environ.get("MINIO_ACCESS_KEY")
    minio_secret_key = os.environ.get("MINIO_SECRET_KEY")
    kafka_bootstrap = os.environ.get("KAFKA_BOOTSTRAP", DEFAULT_KAFKA_BOOTSTRAP)
    kafka_topic = os.environ.get("KAFKA_TOPIC", DEFAULT_KAFKA_TOPIC)
    ros_bucket = os.environ.get("ROS_BUCKET", DEFAULT_ROS_BUCKET)
    schema = os.environ.get("SCHEMA_NAME", DEFAULT_SCHEMA)
    org_id = os.environ.get("ORG_ID", DEFAULT_ORG_ID)
    account = os.environ.get("ACCOUNT_NUMBER", DEFAULT_ACCOUNT)
    presign_expiry = int(os.environ.get("PRESIGN_EXPIRY", DEFAULT_PRESIGN_EXPIRY))

    log.info("Configuration:")
    log.info("  MinIO:     %s (bucket: %s)", minio_endpoint, ros_bucket)
    log.info("  Kafka:     %s (topic: %s)", kafka_bootstrap, kafka_topic)
    log.info("  Cluster:   %s (%s)", args.cluster_uuid, args.cluster_alias)
    log.info("  Provider:  %s", args.provider_uuid)
    log.info("  Schema:    %s (org_id: %s)", schema, org_id)

    # Find ROS CSV files
    ros_files = find_ros_csvs(args.data_dir)

    if args.dry_run:
        log.info("DRY RUN — would upload %d manifests:", len(ros_files))
        for date_range, files in sorted(ros_files.items()):
            date_str = parse_date_from_range(date_range)
            log.info("  %s (%s): %d files", date_range, date_str, len(files))
            for f in files:
                key = build_s3_key(schema, args.provider_uuid, date_str, f.name)
                log.info("    → s3://%s/%s", ros_bucket, key)
        return

    # Credentials required for actual upload
    if not minio_access_key or not minio_secret_key:
        log.error(
            "MINIO_ACCESS_KEY and MINIO_SECRET_KEY must be set. "
            "Read them from the Kubernetes secret:\n"
            "  export MINIO_ACCESS_KEY=$(oc get secret cost-onprem-storage-credentials "
            "-n cost-onprem -o jsonpath='{.data.access-key}' | base64 -d)\n"
            "  export MINIO_SECRET_KEY=$(oc get secret cost-onprem-storage-credentials "
            "-n cost-onprem -o jsonpath='{.data.secret-key}' | base64 -d)"
        )
        sys.exit(1)

    # Initialize S3 client with high multipart threshold to avoid multipart upload issues
    import boto3
    from boto3.s3.transfer import TransferConfig

    s3_client = boto3.client(
        "s3",
        endpoint_url=minio_endpoint,
        aws_access_key_id=minio_access_key,
        aws_secret_access_key=minio_secret_key,
        config=boto3.session.Config(signature_version="s3v4"),
    )
    transfer_config = TransferConfig(multipart_threshold=256 * 1024 * 1024)

    # Verify bucket exists
    try:
        s3_client.head_bucket(Bucket=ros_bucket)
    except Exception as e:
        log.error("Cannot access bucket '%s': %s", ros_bucket, e)
        sys.exit(1)

    # Initialize Kafka producer
    from kafka import KafkaProducer

    producer = KafkaProducer(
        bootstrap_servers=kafka_bootstrap,
        value_serializer=lambda v: json.dumps(v).encode("utf-8"),
        security_protocol="PLAINTEXT",
        max_request_size=10 * 1024 * 1024,  # 10MB
        request_timeout_ms=30000,
    )

    # Process each date range, batching files into groups for resilience
    batch_size = int(os.environ.get("BATCH_SIZE", "200"))
    total_files_uploaded = 0
    total_messages_sent = 0
    start_time = time.time()

    for date_range, csv_files in sorted(ros_files.items()):
        date_str = parse_date_from_range(date_range)

        # Split large file lists into batches
        batches = [csv_files[i : i + batch_size] for i in range(0, len(csv_files), batch_size)]
        log.info(
            "Processing %s (%s): %d files in %d batches",
            date_range, date_str, len(csv_files), len(batches),
        )

        for batch_idx, batch_files in enumerate(batches, 1):
            manifest_id = str(uuid.uuid4())
            request_id = str(uuid.uuid4())

            log.info(
                "  Batch %d/%d: %d files, manifest=%s",
                batch_idx, len(batches), len(batch_files), manifest_id[:8],
            )

            presigned_urls, object_keys = upload_and_presign(
                s3_client,
                transfer_config,
                ros_bucket,
                batch_files,
                schema,
                args.provider_uuid,
                date_str,
                presign_expiry,
                manifest_id,
            )

            expected_files = [f.name for f in batch_files]

            msg = build_kafka_message(
                request_id=request_id,
                manifest_id=manifest_id,
                cluster_uuid=args.cluster_uuid,
                cluster_alias=args.cluster_alias,
                provider_uuid=args.provider_uuid,
                org_id=org_id,
                account=account,
                source_id=args.source_id,
                presigned_urls=presigned_urls,
                object_keys=object_keys,
                expected_files=expected_files,
            )

            future = producer.send(kafka_topic, msg)
            future.get(timeout=30)
            log.info(
                "  Kafka message sent for batch %d/%d, manifest %s (%d files)",
                batch_idx, len(batches), manifest_id[:8], len(batch_files),
            )

            total_files_uploaded += len(batch_files)
            total_messages_sent += 1

    producer.flush()
    producer.close()

    elapsed = time.time() - start_time
    log.info(
        "Done. Uploaded %d files across %d Kafka messages in %.1fs. "
        "The ROS processor should begin processing shortly.",
        total_files_uploaded,
        total_messages_sent,
        elapsed,
    )


if __name__ == "__main__":
    main()
