# Near Lake Supervisor

A service that monitors the NEAR Lake indexer's block height and automatically restarts the container if the block height stops progressing.

## Features

- Queries the indexer's Prometheus metrics endpoint at configurable intervals
- Monitors `near_indexer_streaming_current_block_height` metric
- Automatically restarts the indexer container if block height stalls
- Configurable query interval, stall timeout, and restart cooldown period

## Configuration

Copy `config/example.yaml` to `config/local.yaml` and adjust the settings:

- `indexerURL`: The URL of the indexer's metrics endpoint (default: `http://indexer:3030`)
- `queryInterval`: How often to query the block height (e.g., `30s`, `1m`, `5m`)
- `stallTimeout`: How long the block height can be stalled before restarting (e.g., `5m`, `10m`)
- `restartSleep`: How long to wait after restart before resuming queries (e.g., `30s`, `1m`)
- `metricName`: The Prometheus metric name to query (default: `near_indexer_streaming_current_block_height`)
- `composeFile`: Path to docker-compose.yaml file (default: `/app/docker-compose.yaml`)
- `composeService`: Name of the service to restart (default: `indexer`)

## Usage

### Using Docker Compose

The service is included in `docker-compose.yaml` and will automatically monitor the `indexer` service.

```bash
docker-compose up -d
```

### Building and Running Locally

```bash
go mod download
go build -o near-lake-supervisor .
./near-lake-supervisor
```

## How It Works

1. The service queries the indexer's metrics endpoint at the configured interval
2. It extracts the `near_indexer_streaming_current_block_height` value
3. If the block height hasn't increased within the `stallTimeout` period, it restarts the container
4. After restart, it waits for `restartSleep` duration before resuming monitoring

## Requirements

- Docker and docker-compose (for container restart functionality)
- Access to the indexer's metrics endpoint
- Docker socket access (mounted in docker-compose)
