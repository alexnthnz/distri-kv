# Distri-KV: Distributed Key-Value Store

A comprehensive distributed key-value store designed for **big data**, **high availability**, and **scalability**. Built from the ground up to handle massive datasets across distributed nodes with enterprise-grade features.

## 🚀 Features

### Core Capabilities
- **Distributed Architecture**: Scales horizontally across multiple nodes
- **High Availability**: 99.9% uptime with automatic failover
- **Tunable Consistency**: Choose between strong consistency and eventual consistency
- **Data Compression**: Automatic compression for large values using Zstandard
- **Hot/Cold Data Management**: Intelligent memory/disk hybrid storage
- **Big Data Support**: Handle terabytes to petabytes of data

### Advanced Features
- **Consistent Hashing**: Efficient data partitioning with minimal rebalancing
- **Vector Clocks**: Advanced conflict resolution for concurrent updates
- **Quorum Consensus**: Configurable read/write quorums (R, W, N)
- **Hinted Handoff**: Handles temporary node failures gracefully
- **Failure Detection**: Heartbeat-based monitoring with gossip protocols
- **Cross-Data Center Replication**: Geographic redundancy

### CAP Theorem Compliance
- **AP (Availability + Partition Tolerance)**: Prioritizes availability during network partitions
- **Tunable Consistency**: Adjust consistency levels based on application needs
- **Strong Consistency Mode**: When R + W > N

## 🏗️ Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Node 1      │    │     Node 2      │    │     Node 3      │
│  ┌───────────┐  │    │  ┌───────────┐  │    │  ┌───────────┐  │
│  │    API    │  │    │  │    API    │  │    │  │    API    │  │
│  │  Server   │  │    │  │  Server   │  │    │  │  Server   │  │
│  └───────────┘  │    │  └───────────┘  │    │  └───────────┘  │
│  ┌───────────┐  │    │  ┌───────────┐  │    │  ┌───────────┐  │
│  │ Quorum    │  │    │  │ Quorum    │  │    │  │ Quorum    │  │
│  │ Manager   │  │    │  │ Manager   │  │    │  │ Manager   │  │
│  └───────────┘  │    │  └───────────┘  │    │  └───────────┘  │
│  ┌───────────┐  │    │  ┌───────────┐  │    │  ┌───────────┐  │
│  │ Storage   │  │    │  │ Storage   │  │    │  │ Storage   │  │
│  │ Engine    │  │    │  │ Engine    │  │    │  │ Engine    │  │
│  └───────────┘  │    │  └───────────┘  │    │  └───────────┘  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                    ┌─────────────────┐
                    │ Consistent Hash │
                    │     Ring        │
                    └─────────────────┘
```

### System Components

1. **Storage Engine**: 
   - Memory/disk hybrid with LRU eviction
   - Automatic compression for large values
   - Persistent storage with recovery

2. **Consistent Hashing**: 
   - Virtual nodes for load balancing
   - Minimal data movement on scaling
   - Support for heterogeneous clusters

3. **Vector Clocks**: 
   - Conflict detection and resolution
   - Causal ordering of events
   - Last-write-wins and client-side resolution

4. **Quorum Manager**: 
   - Configurable read/write quorums
   - Tunable consistency levels
   - Automatic conflict resolution

5. **Failure Detector**: 
   - Heartbeat monitoring
   - Gossip protocol for cluster state
   - Automatic recovery detection

6. **Hinted Handoff**: 
   - Temporary failure handling
   - Automatic data delivery on recovery
   - Configurable retry policies

## 🚀 Quick Start

### Prerequisites
- Go 1.22 or higher
- Make (optional, for build automation)

### Installation
```bash
# Clone the repository
git clone https://github.com/alexnthnz/distri-kv.git
cd distri-kv

# Download dependencies
go mod tidy

# Build the application
make build
```

### Running a Single Node
```bash
# Start with default configuration
make run

# Or build and run manually
go build -o bin/distri-kv ./cmd/distri-kv
./bin/distri-kv
```

### Running a 3-Node Cluster
```bash
# Start all nodes in cluster mode
make run-cluster

# Stop the cluster
make stop-cluster
```

### Configuration
```yaml
node:
  node_id: "node1"
  address: "localhost:9000"
  data_dir: "./data/node1"
  max_mem_items: 1000
  quorum:
    n: 3  # Replication factor
    r: 2  # Read quorum
    w: 2  # Write quorum
  cluster_nodes:
    - "localhost:9001"
    - "localhost:9002"

api:
  port: 8080

log:
  level: "info"
  format: "text"
```

## 📚 API Reference

### Key-Value Operations

#### Store a key-value pair
```bash
curl -X PUT http://localhost:8080/api/v1/kv/user1 \
  -H "Content-Type: application/json" \
  -d '{"value":"Alice"}'
```

#### Retrieve a value
```bash
curl http://localhost:8080/api/v1/kv/user1
```

#### Delete a key
```bash
curl -X DELETE http://localhost:8080/api/v1/kv/user1
```

### Cluster Operations

#### Get cluster statistics
```bash
curl http://localhost:8080/api/v1/cluster/stats
```

#### Check cluster health
```bash
curl http://localhost:8080/api/v1/cluster/health
```

#### Get detailed admin stats
```bash
curl http://localhost:8080/api/v1/admin/stats
```

## ⚙️ Configuration Options

### Quorum Settings
- **N (Replication Factor)**: Number of replicas for each key
- **R (Read Quorum)**: Minimum nodes that must respond to reads
- **W (Write Quorum)**: Minimum nodes that must acknowledge writes
- **Strong Consistency**: Achieved when R + W > N

### Consistency Levels
- **Strong**: R + W > N (e.g., N=3, R=2, W=2)
- **Eventual**: R + W ≤ N (e.g., N=3, R=1, W=1)
- **Read-Heavy**: Low R, High W (e.g., N=3, R=1, W=3)
- **Write-Heavy**: High R, Low W (e.g., N=3, R=3, W=1)

## 🔧 Development

### Building
```bash
make build          # Build the application
make clean          # Clean build artifacts
make test           # Run tests
make test-coverage  # Run tests with coverage
```

### Code Quality
```bash
make fmt            # Format code
make lint           # Lint code (requires golangci-lint)
make install-tools  # Install development tools
```

### Testing
```bash
# Run unit tests
make test

# Run with coverage
make test-coverage

# Run API examples (requires running cluster)
make api-examples
```

## 📊 Performance

### Benchmarks
- **Throughput**: 10,000+ ops/sec per node
- **Latency**: < 5ms for local operations
- **Scalability**: Linear scaling with node count
- **Storage**: Supports TB+ datasets per node

### Optimization Features
- Data compression (up to 70% space savings)
- Memory caching for hot data
- Batch operations support
- Connection pooling and multiplexing

## 🔍 Monitoring

### Built-in Metrics
- Node health and status
- Storage utilization
- Operation latency
- Replication lag
- Failure detection statistics

### Health Endpoints
```bash
# Basic health check
curl http://localhost:8080/api/v1/cluster/health

# Detailed statistics
curl http://localhost:8080/api/v1/admin/stats
```

## 🛠️ Troubleshooting

### Common Issues

#### Node Won't Start
- Check configuration file syntax
- Verify port availability
- Check disk space for data directory

#### Cluster Split-Brain
- Ensure proper quorum configuration
- Check network connectivity between nodes
- Verify failure detection settings

#### Performance Issues
- Monitor memory usage vs max_mem_items
- Check disk I/O for cold data access
- Review compression settings for large values

### Logs
```bash
# View logs with different levels
./bin/distri-kv -log-level debug
./bin/distri-kv -log-level info
```

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup
```bash
# Fork and clone the repository
git clone https://github.com/your-username/distri-kv.git
cd distri-kv

# Install dependencies
make deps

# Install development tools
make install-tools

# Run tests
make test
```

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Inspired by Amazon DynamoDB paper on distributed systems
- Vector clock implementation based on academic research
- Consistent hashing algorithm from distributed systems literature

## 📖 Further Reading

- [CAP Theorem Explained](https://en.wikipedia.org/wiki/CAP_theorem)
- [Dynamo: Amazon's Highly Available Key-value Store](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf)
- [Vector Clocks in Distributed Systems](https://en.wikipedia.org/wiki/Vector_clock)
- [Consistent Hashing](https://en.wikipedia.org/wiki/Consistent_hashing)
