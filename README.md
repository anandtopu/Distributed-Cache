# Distributed Cache (WIP)

This repo contains a cache node that exposes **both REST and gRPC** APIs.

## Run

```bash
go run ./cmd/cache-node --http :8080 --grpc :9090 --capacity 100000
```

### Cluster mode (static membership)

You can enable a simple distributed mode (consistent hashing + replication + request forwarding)
by providing the `--nodes` list and this node's `--node-id`.

Example (run 3 nodes in 3 terminals):

```bash
go run ./cmd/cache-node --node-id node1 --http :8080 --grpc :9090 --replication 2 --nodes node1=localhost:9090,node2=localhost:9091,node3=localhost:9092
go run ./cmd/cache-node --node-id node2 --http :8081 --grpc :9091 --replication 2 --nodes node1=localhost:9090,node2=localhost:9091,node3=localhost:9092
go run ./cmd/cache-node --node-id node3 --http :8082 --grpc :9092 --replication 2 --nodes node1=localhost:9090,node2=localhost:9091,node3=localhost:9092
```

In cluster mode, you can send REST/gRPC requests to **any** node; it will route/forward internally.

### Docker Compose demo

```bash
docker compose up --build
```

### Observability (Prometheus + Grafana)

When running via Docker Compose, each node exposes Prometheus metrics at:

- `node1`: http://localhost:8080/metrics
- `node2`: http://localhost:8081/metrics
- `node3`: http://localhost:8082/metrics

The compose stack also starts:

- Prometheus: http://localhost:9095
- Grafana: http://localhost:3000 (admin/admin)

Grafana is provisioned with a minimal dashboard: **Distributed Cache Overview**.

### Smoke test

PowerShell:

```powershell
./scripts/smoke_cluster.ps1
```

Bash:

```bash
./scripts/smoke_cluster.sh
```

REST ports:

- `node1`: `localhost:8080`
- `node2`: `localhost:8081`
- `node3`: `localhost:8082`

## REST API

- `POST /{key}`

Request body:

```json
{ "value": "<base64>", "ttl_ms": 60000 }
```

- `GET /{key}` -> `200`:

```json
{ "value": "<base64>", "expires_at_unix_ms": 123 }
```

- `DELETE /{key}` -> `204`

## gRPC API

Proto: `api/proto/cache/v1/cache.proto`

Service: `cache.v1.CacheService`
