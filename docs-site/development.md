# Local Development

See the [Contributing Guide](contributing.md) for full setup instructions.

## Quick Start

```bash
# Prerequisites
go install (see go.mod for version)
podman run -d --name ros-test-db -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16

# Run
cp .env.example .env  # edit as needed
go run . start

# Test
go test ./...                    # unit tests (short)
go test -count=1 ./...           # all tests including integration
```

## Environment Variables

ROS-OCP uses [Viper](https://github.com/spf13/viper) for configuration.
Create a `.env` file at the repository root (see `.env.example`).

Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_DB_HOST` | `localhost` | PostgreSQL host |
| `ROS_DB_PORT` | `5432` | PostgreSQL port |
| `ROS_DB_NAME` | `ros_ocp` | Database name |
| `ROS_DB_USER` | `postgres` | Database user |
| `ROS_DB_PASSWORD` | `postgres` | Database password |
| `ROS_KAFKA_BROKERS` | `localhost:9092` | Kafka bootstrap servers |
| `ROS_ENABLED_PLUGINS` | (all) | Comma-separated plugin allowlist |
| `ROS_DISABLED_PLUGINS` | (none) | Comma-separated plugin denylist |

## Plugin Development

See the [Plugin Architecture](architecture/plugin-architecture.md) and
[example plugin](api-reference/example.md) for how to add new recommendation domains.
