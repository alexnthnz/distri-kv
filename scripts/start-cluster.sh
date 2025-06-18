#!/bin/bash

# Start 3-node distri-kv cluster for development and testing

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="$PROJECT_DIR/bin"
EXAMPLES_DIR="$PROJECT_DIR/examples"
PIDS_FILE="$PROJECT_DIR/.cluster_pids"

# Check if binary exists
if [ ! -f "$BIN_DIR/distri-kv" ]; then
    echo -e "${RED}❌ Binary not found at $BIN_DIR/distri-kv${NC}"
    echo -e "${YELLOW}💡 Run 'make build' first${NC}"
    exit 1
fi

# Function to cleanup on exit
cleanup() {
    echo -e "\n${YELLOW}🧹 Cleaning up cluster...${NC}"
    if [ -f "$PIDS_FILE" ]; then
        while read -r pid; do
            if kill -0 "$pid" 2>/dev/null; then
                echo "   Stopping process $pid"
                kill "$pid" 2>/dev/null || true
            fi
        done < "$PIDS_FILE"
        rm -f "$PIDS_FILE"
    fi
    echo -e "${GREEN}✅ Cleanup complete${NC}"
}

# Trap cleanup on script exit
trap cleanup EXIT INT TERM

# Function to start a node
start_node() {
    local node_name="$1"
    local config_file="$2"
    local expected_port="$3"
    
    echo -e "${BLUE}🚀 Starting $node_name...${NC}"
    
    if [ ! -f "$config_file" ]; then
        echo -e "${RED}❌ Config file not found: $config_file${NC}"
        return 1
    fi
    
    # Start the node in background
    "$BIN_DIR/distri-kv" -config "$config_file" &
    local pid=$!
    echo "$pid" >> "$PIDS_FILE"
    
    echo "   PID: $pid"
    echo "   Config: $config_file"
    echo "   Expected port: $expected_port"
    
    # Wait for the node to be ready
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if curl -s "http://localhost:$expected_port/api/v1/cluster/health" > /dev/null 2>&1; then
            echo -e "${GREEN}✅ $node_name is ready on port $expected_port${NC}"
            return 0
        fi
        
        # Check if process is still running
        if ! kill -0 "$pid" 2>/dev/null; then
            echo -e "${RED}❌ $node_name process died unexpectedly${NC}"
            return 1
        fi
        
        echo "   Attempt $attempt/$max_attempts: Waiting for $node_name to be ready..."
        sleep 2
        ((attempt++))
    done
    
    echo -e "${RED}❌ $node_name failed to start after $((max_attempts * 2)) seconds${NC}"
    return 1
}

# Function to show cluster status
show_status() {
    echo -e "\n${BLUE}📊 Cluster Status:${NC}"
    echo "==================="
    
    local nodes=(
        "Node 1:8080"
        "Node 2:8081" 
        "Node 3:8082"
    )
    
    for node_info in "${nodes[@]}"; do
        local name="${node_info%:*}"
        local port="${node_info#*:}"
        
        if curl -s "http://localhost:$port/api/v1/cluster/health" > /dev/null 2>&1; then
            echo -e "   ${GREEN}✅ $name (port $port): Running${NC}"
        else
            echo -e "   ${RED}❌ $name (port $port): Not responding${NC}"
        fi
    done
    
    echo ""
    echo -e "${YELLOW}🔗 API Endpoints:${NC}"
    echo "   Node 1: http://localhost:8080/api/v1"
    echo "   Node 2: http://localhost:8081/api/v1"
    echo "   Node 3: http://localhost:8082/api/v1"
    echo ""
    echo -e "${YELLOW}📖 Quick Test Commands:${NC}"
    echo "   # Store a key:"
    echo "   curl -X PUT http://localhost:8080/api/v1/kv/test -H 'Content-Type: application/json' -d '{\"value\":\"Hello World\"}'"
    echo ""
    echo "   # Retrieve a key:"
    echo "   curl http://localhost:8080/api/v1/kv/test"
    echo ""
    echo "   # View cluster stats:"
    echo "   curl http://localhost:8080/api/v1/cluster/stats | jq ."
    echo ""
    echo "   # Run comprehensive tests:"
    echo "   ./scripts/test-cluster.sh"
}

# Main execution
main() {
    echo -e "${GREEN}🎯 Starting Distri-KV Cluster${NC}"
    echo "=============================="
    
    # Create PID file
    rm -f "$PIDS_FILE"
    touch "$PIDS_FILE"
    
    # Start nodes sequentially
    start_node "Node 1" "$EXAMPLES_DIR/../config.yaml" "8080" || exit 1
    sleep 2
    
    start_node "Node 2" "$EXAMPLES_DIR/node2.yaml" "8081" || exit 1
    sleep 2
    
    start_node "Node 3" "$EXAMPLES_DIR/node3.yaml" "8082" || exit 1
    sleep 2
    
    show_status
    
    echo -e "\n${GREEN}🎉 Cluster started successfully!${NC}"
    echo -e "${YELLOW}💡 Press Ctrl+C to stop all nodes${NC}"
    
    # Wait for user interrupt
    while true; do
        sleep 1
    done
}

# Handle command line arguments
case "${1:-}" in
    "status")
        show_status
        exit 0
        ;;
    "help"|"-h"|"--help")
        echo "Usage: $0 [status|help]"
        echo ""
        echo "Commands:"
        echo "  (no args)  Start the 3-node cluster"
        echo "  status     Show cluster status without starting"
        echo "  help       Show this help message"
        echo ""
        echo "The script will start three nodes:"
        echo "  - Node 1 on port 8080 (config.yaml)"
        echo "  - Node 2 on port 8081 (examples/node2.yaml)"
        echo "  - Node 3 on port 8082 (examples/node3.yaml)"
        exit 0
        ;;
    "")
        main
        ;;
    *)
        echo -e "${RED}❌ Unknown command: $1${NC}"
        echo "Run '$0 help' for usage information"
        exit 1
        ;;
esac 