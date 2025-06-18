# 🚀 Distri-KV Quick Start Guide

Welcome to **Distri-KV**, a high-performance distributed key-value store! This guide will get you up and running in minutes.

## 📋 Prerequisites

- **Go 1.19+** (for building from source)
- **curl** (for testing API endpoints)
- **jq** (optional, for pretty JSON output)

## 🏗️ Quick Setup

### 1. Build the Application

```bash
# Clone and build
git clone <your-repo-url>
cd distri-kv
make build
```

### 2. Start a Single Node

```bash
# Start with default configuration
./bin/distri-kv

# Or with custom settings
./bin/distri-kv -port 8080 -log-level info
```

### 3. Test Basic Operations

```bash
# Store a key-value pair
curl -X PUT http://localhost:8080/api/v1/kv/hello \
  -H "Content-Type: application/json" \
  -d '{"value":"world"}'

# Retrieve the value
curl http://localhost:8080/api/v1/kv/hello

# Delete the key
curl -X DELETE http://localhost:8080/api/v1/kv/hello
```

## 🔧 Advanced Setup

### Start a 3-Node Cluster

```bash
# Use the provided cluster script
./scripts/start-cluster.sh

# Cluster will be available at:
# - Node 1: http://localhost:8080
# - Node 2: http://localhost:8081
# - Node 3: http://localhost:8082
```

### Configuration Options

Create a `config.yaml` file:

```yaml
node:
  node_id: "node1"
  address: "localhost:9000"
  data_dir: "./data"
  api_port: 8080

storage:
  memory_cache_size: 1000
  compression_enabled: true
  compression_threshold: 1024

cluster:
  peers:
    - "localhost:9001"
    - "localhost:9002"
  replication_factor: 3
  read_quorum: 2
  write_quorum: 2

failure_detection:
  heartbeat_interval: "5s"
  failure_timeout: "30s"

logging:
  level: "info"
  format: "json"
```

## 📖 API Reference

### Key-Value Operations

```bash
# Store a value
curl -X PUT http://localhost:8080/api/v1/kv/{key} \
  -H "Content-Type: application/json" \
  -d '{"value":"your-value"}'

# Retrieve a value
curl http://localhost:8080/api/v1/kv/{key}

# Delete a value
curl -X DELETE http://localhost:8080/api/v1/kv/{key}

# Store with metadata
curl -X PUT http://localhost:8080/api/v1/kv/{key} \
  -H "Content-Type: application/json" \
  -d '{"value":"your-value","metadata":{"ttl":3600}}'
```

### Cluster Management

```bash
# Check cluster health
curl http://localhost:8080/api/v1/cluster/health

# View cluster statistics
curl http://localhost:8080/api/v1/cluster/stats | jq .

# View admin statistics
curl http://localhost:8080/api/v1/admin/stats | jq .

# Check node status
curl http://localhost:8080/api/v1/admin/nodes | jq .
```

## 🧪 Testing & Benchmarking

### Run Comprehensive Tests

```bash
# Test all functionality
./scripts/test-cluster.sh

# Run unit tests
make test

# Run specific storage tests
go test ./internal/storage/ -v
```

### Performance Benchmarks

```bash
# Quick benchmark
./scripts/benchmark.sh small

# Standard benchmark
./scripts/benchmark.sh medium

# Intensive benchmark
./scripts/benchmark.sh large
```

## 🔍 Monitoring & Debugging

### Check Logs

```bash
# View logs in real-time
tail -f ./data/distri-kv.log

# JSON formatted logs
jq . ./data/distri-kv.log
```

### Health Checks

```bash
# Simple health check
curl http://localhost:8080/api/v1/cluster/health

# Detailed system status
curl http://localhost:8080/api/v1/admin/stats | jq .
```

### Troubleshooting

Common issues and solutions:

1. **Port already in use**
   ```bash
   # Check what's using the port
   lsof -i :8080
   
   # Use a different port
   ./bin/distri-kv -port 8081
   ```

2. **Permission denied on data directory**
   ```bash
   # Fix permissions
   chmod 755 ./data
   ```

3. **Cluster nodes can't connect**
   ```bash
   # Check firewall settings
   # Verify node addresses in config
   # Check network connectivity
   ```

## 🎯 Common Use Cases

### 1. Session Store

```bash
# Store user session
curl -X PUT http://localhost:8080/api/v1/kv/session:user123 \
  -H "Content-Type: application/json" \
  -d '{"value":"{\"user_id\":123,\"login_time\":\"2024-01-01T10:00:00Z\"}"}'

# Retrieve session
curl http://localhost:8080/api/v1/kv/session:user123
```

### 2. Configuration Cache

```bash
# Store application config
curl -X PUT http://localhost:8080/api/v1/kv/config:app \
  -H "Content-Type: application/json" \
  -d '{"value":"{\"debug\":true,\"timeout\":30}"}'

# Get config
curl http://localhost:8080/api/v1/kv/config:app
```

### 3. Distributed Counter

```bash
# Increment counter (simulate with multiple operations)
for i in {1..10}; do
  curl -X PUT http://localhost:8080/api/v1/kv/counter:page_views \
    -H "Content-Type: application/json" \
    -d "{\"value\":\"$i\"}"
done
```

## 🔧 Production Deployment

### Docker Deployment

```dockerfile
FROM golang:1.19-alpine AS builder
WORKDIR /app
COPY . .
RUN make build

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/bin/distri-kv .
COPY --from=builder /app/config.yaml .
EXPOSE 8080
CMD ["./distri-kv"]
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: distri-kv
spec:
  serviceName: distri-kv
  replicas: 3
  selector:
    matchLabels:
      app: distri-kv
  template:
    metadata:
      labels:
        app: distri-kv
    spec:
      containers:
      - name: distri-kv
        image: distri-kv:latest
        ports:
        - containerPort: 8080
        - containerPort: 9000
        volumeMounts:
        - name: data
          mountPath: /data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi
```

## 📚 Next Steps

1. **Explore the Architecture**: Read the main [README.md](README.md) for detailed architecture information
2. **Configure for Production**: Tune settings in `config.yaml` for your use case
3. **Set up Monitoring**: Integrate with your monitoring stack using the `/admin/stats` endpoint
4. **Scale Horizontally**: Add more nodes to handle increased load
5. **Backup Strategy**: Implement regular backups of the data directory

## 🤝 Getting Help

- **Documentation**: Check the main [README.md](README.md)
- **Issues**: Report bugs and feature requests
- **Community**: Join our discussions

## 🎉 You're Ready!

You now have a working distributed key-value store! Start building your applications with high-performance, scalable data storage.

---

**Happy coding! 🚀** 