#!/bin/bash

# Performance benchmark script for distri-kv

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
BASE_URL="http://localhost:8080"
API_BASE="$BASE_URL/api/v1"

# Check if service is running
check_service() {
    if ! curl -s "$API_BASE/cluster/health" > /dev/null 2>&1; then
        echo -e "${RED}❌ Distri-KV service is not running${NC}"
        echo -e "${YELLOW}💡 Start it with: ./scripts/start-cluster.sh${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Service is running${NC}"
}

# Benchmark write operations
benchmark_writes() {
    local num_ops=$1
    echo -e "\n${BLUE}📝 Write Benchmark - $num_ops operations${NC}"
    
    local start_time=$(date +%s.%N)
    
    for i in $(seq 1 $num_ops); do
        curl -s -X PUT "$API_BASE/kv/bench_write_$i" \
            -H "Content-Type: application/json" \
            -d "{\"value\":\"benchmark_value_$i\"}" > /dev/null
        
        # Show progress every 100 operations
        if [ $((i % 100)) -eq 0 ]; then
            echo -n "."
        fi
    done
    
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)
    local ops_per_sec=$(echo "scale=2; $num_ops / $duration" | bc)
    
    echo ""
    echo "   Operations: $num_ops"
    echo "   Duration:   ${duration}s"
    echo "   Throughput: ${ops_per_sec} ops/sec"
    echo "   Avg Latency: $(echo "scale=2; $duration * 1000 / $num_ops" | bc)ms per operation"
}

# Benchmark read operations
benchmark_reads() {
    local num_ops=$1
    echo -e "\n${BLUE}📖 Read Benchmark - $num_ops operations${NC}"
    
    local start_time=$(date +%s.%N)
    
    for i in $(seq 1 $num_ops); do
        # Read from previously written keys
        local key_id=$((i % 1000 + 1))
        curl -s "$API_BASE/kv/bench_write_$key_id" > /dev/null
        
        # Show progress every 100 operations
        if [ $((i % 100)) -eq 0 ]; then
            echo -n "."
        fi
    done
    
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)
    local ops_per_sec=$(echo "scale=2; $num_ops / $duration" | bc)
    
    echo ""
    echo "   Operations: $num_ops"
    echo "   Duration:   ${duration}s"
    echo "   Throughput: ${ops_per_sec} ops/sec"
    echo "   Avg Latency: $(echo "scale=2; $duration * 1000 / $num_ops" | bc)ms per operation"
}

# Benchmark mixed operations
benchmark_mixed() {
    local num_ops=$1
    echo -e "\n${BLUE}🔄 Mixed Benchmark - $num_ops operations (70% reads, 30% writes)${NC}"
    
    local start_time=$(date +%s.%N)
    local writes=0
    local reads=0
    
    for i in $(seq 1 $num_ops); do
        # 70% chance of read, 30% chance of write
        if [ $((RANDOM % 10)) -lt 7 ]; then
            # Read operation
            local key_id=$((RANDOM % 1000 + 1))
            curl -s "$API_BASE/kv/bench_write_$key_id" > /dev/null
            reads=$((reads + 1))
        else
            # Write operation
            curl -s -X PUT "$API_BASE/kv/mixed_bench_$i" \
                -H "Content-Type: application/json" \
                -d "{\"value\":\"mixed_value_$i\"}" > /dev/null
            writes=$((writes + 1))
        fi
        
        # Show progress every 100 operations
        if [ $((i % 100)) -eq 0 ]; then
            echo -n "."
        fi
    done
    
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)
    local ops_per_sec=$(echo "scale=2; $num_ops / $duration" | bc)
    
    echo ""
    echo "   Total Operations: $num_ops"
    echo "   Reads: $reads ($(echo "scale=1; $reads * 100 / $num_ops" | bc)%)"
    echo "   Writes: $writes ($(echo "scale=1; $writes * 100 / $num_ops" | bc)%)"
    echo "   Duration: ${duration}s"
    echo "   Throughput: ${ops_per_sec} ops/sec"
    echo "   Avg Latency: $(echo "scale=2; $duration * 1000 / $num_ops" | bc)ms per operation"
}

# Benchmark large values
benchmark_large_values() {
    local num_ops=$1
    echo -e "\n${BLUE}💾 Large Value Benchmark - $num_ops operations (10KB values)${NC}"
    
    # Generate a 10KB value
    local large_value=$(python3 -c "print('x' * 10240)")
    
    local start_time=$(date +%s.%N)
    
    for i in $(seq 1 $num_ops); do
        curl -s -X PUT "$API_BASE/kv/large_bench_$i" \
            -H "Content-Type: application/json" \
            -d "{\"value\":\"$large_value\"}" > /dev/null
        
        # Show progress every 10 operations (fewer for large values)
        if [ $((i % 10)) -eq 0 ]; then
            echo -n "."
        fi
    done
    
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)
    local ops_per_sec=$(echo "scale=2; $num_ops / $duration" | bc)
    local throughput_mb=$(echo "scale=2; $num_ops * 10.24 / 1024 / $duration" | bc)
    
    echo ""
    echo "   Operations: $num_ops"
    echo "   Value Size: 10KB each"
    echo "   Total Data: $(echo "scale=1; $num_ops * 10.24 / 1024" | bc)MB"
    echo "   Duration: ${duration}s"
    echo "   Throughput: ${ops_per_sec} ops/sec"
    echo "   Data Throughput: ${throughput_mb}MB/s"
}

# Show system stats
show_system_stats() {
    echo -e "\n${YELLOW}📊 System Statistics:${NC}"
    echo "====================="
    
    echo -e "\n${BLUE}Cluster Stats:${NC}"
    curl -s "$API_BASE/cluster/stats" | jq . 2>/dev/null || curl -s "$API_BASE/cluster/stats"
    
    echo -e "\n${BLUE}Admin Stats:${NC}"
    curl -s "$API_BASE/admin/stats" | jq . 2>/dev/null || curl -s "$API_BASE/admin/stats"
}

# Main benchmark execution
main() {
    echo -e "${GREEN}⚡ Distri-KV Performance Benchmark${NC}"
    echo "=================================="
    
    # Check dependencies
    if ! command -v bc &> /dev/null; then
        echo -e "${RED}❌ bc is required for calculations${NC}"
        echo -e "${YELLOW}💡 Install with: brew install bc (macOS) or apt-get install bc (Ubuntu)${NC}"
        exit 1
    fi
    
    check_service
    
    # Run benchmarks with different scales
    local scale="${1:-medium}"
    
    case $scale in
        "small")
            echo -e "${YELLOW}🔬 Running small-scale benchmarks...${NC}"
            benchmark_writes 100
            benchmark_reads 500
            benchmark_mixed 300
            benchmark_large_values 10
            ;;
        "medium")
            echo -e "${YELLOW}🏃 Running medium-scale benchmarks...${NC}"
            benchmark_writes 1000
            benchmark_reads 5000
            benchmark_mixed 2000
            benchmark_large_values 50
            ;;
        "large")
            echo -e "${YELLOW}🚀 Running large-scale benchmarks...${NC}"
            benchmark_writes 5000
            benchmark_reads 25000
            benchmark_mixed 10000
            benchmark_large_values 200
            ;;
        *)
            echo -e "${RED}❌ Unknown scale: $scale${NC}"
            echo "Available scales: small, medium, large"
            exit 1
            ;;
    esac
    
    show_system_stats
    
    echo -e "\n${GREEN}🎉 Benchmark completed!${NC}"
    echo -e "${YELLOW}💡 Tips for better performance:${NC}"
    echo "   - Increase memory cache size in config"
    echo "   - Use SSD storage for better disk I/O"
    echo "   - Tune compression settings for your data"
    echo "   - Scale horizontally with more nodes"
}

# Handle command line arguments
case "${1:-}" in
    "help"|"-h"|"--help")
        echo "Usage: $0 [scale]"
        echo ""
        echo "Scales:"
        echo "  small   - Quick benchmark (recommended for testing)"
        echo "  medium  - Standard benchmark (default)"
        echo "  large   - Intensive benchmark (for performance testing)"
        echo ""
        echo "Example:"
        echo "  $0 small    # Run small-scale benchmark"
        echo "  $0          # Run medium-scale benchmark"
        echo "  $0 large    # Run large-scale benchmark"
        exit 0
        ;;
    "small"|"medium"|"large"|"")
        main "$1"
        ;;
    *)
        echo -e "${RED}❌ Unknown argument: $1${NC}"
        echo "Run '$0 help' for usage information"
        exit 1
        ;;
esac 