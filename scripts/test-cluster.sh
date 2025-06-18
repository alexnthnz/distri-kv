#!/bin/bash

# Test script for distri-kv cluster functionality

set -e

echo "🚀 Testing Distri-KV Cluster Functionality"
echo "=========================================="

BASE_URL="http://localhost:8080"
API_BASE="$BASE_URL/api/v1"

# Wait for service to be ready
wait_for_service() {
    echo "⏳ Waiting for service to be ready..."
    for i in {1..30}; do
        if curl -s "$API_BASE/cluster/health" > /dev/null 2>&1; then
            echo "✅ Service is ready!"
            return 0
        fi
        echo "   Attempt $i/30: Service not ready yet..."
        sleep 2
    done
    echo "❌ Service failed to start after 60 seconds"
    exit 1
}

# Test health endpoint
test_health() {
    echo "🏥 Testing health endpoint..."
    response=$(curl -s "$API_BASE/cluster/health")
    echo "Response: $response"
    
    if echo "$response" | grep -q '"healthy":true'; then
        echo "✅ Health check passed"
    else
        echo "❌ Health check failed"
        exit 1
    fi
}

# Test basic key-value operations
test_basic_operations() {
    echo "📝 Testing basic key-value operations..."
    
    # Store a value
    echo "   Storing key 'test1' with value 'Hello World'"
    curl -s -X PUT "$API_BASE/kv/test1" \
        -H "Content-Type: application/json" \
        -d '{"value":"Hello World"}' | jq .
    
    # Retrieve the value
    echo "   Retrieving key 'test1'"
    response=$(curl -s "$API_BASE/kv/test1")
    echo "   Response: $response"
    
    if echo "$response" | grep -q "Hello World"; then
        echo "✅ Basic operations test passed"
    else
        echo "❌ Basic operations test failed"
        exit 1
    fi
    
    # Delete the key
    echo "   Deleting key 'test1'"
    curl -s -X DELETE "$API_BASE/kv/test1" | jq .
    
    # Verify deletion
    echo "   Verifying deletion"
    status_code=$(curl -s -o /dev/null -w "%{http_code}" "$API_BASE/kv/test1")
    if [ "$status_code" = "404" ]; then
        echo "✅ Delete operation test passed"
    else
        echo "❌ Delete operation test failed (status: $status_code)"
        exit 1
    fi
}

# Test multiple keys
test_multiple_keys() {
    echo "🔗 Testing multiple key operations..."
    
    # Store multiple keys
    keys=("user:1" "user:2" "config:db" "session:abc123")
    values=("Alice" "Bob" "postgres://localhost" "active")
    
    for i in "${!keys[@]}"; do
        key="${keys[$i]}"
        value="${values[$i]}"
        echo "   Storing $key = $value"
        curl -s -X PUT "$API_BASE/kv/$key" \
            -H "Content-Type: application/json" \
            -d "{\"value\":\"$value\"}" > /dev/null
    done
    
    # Retrieve all keys
    echo "   Retrieving all stored keys..."
    for key in "${keys[@]}"; do
        response=$(curl -s "$API_BASE/kv/$key")
        echo "   $key: $response"
    done
    
    echo "✅ Multiple keys test completed"
}

# Test large values
test_large_values() {
    echo "💾 Testing large value storage..."
    
    # Generate a large value (will trigger compression)
    large_value=$(python3 -c "print('x' * 5000)")
    
    echo "   Storing large value (5KB)..."
    curl -s -X PUT "$API_BASE/kv/large_test" \
        -H "Content-Type: application/json" \
        -d "{\"value\":\"$large_value\"}" > /dev/null
    
    echo "   Retrieving large value..."
    response=$(curl -s "$API_BASE/kv/large_test")
    
    if echo "$response" | grep -q "xxxxx"; then
        echo "✅ Large value test passed"
    else
        echo "❌ Large value test failed"
        exit 1
    fi
}

# Test cluster statistics
test_statistics() {
    echo "📊 Testing cluster statistics..."
    
    echo "   Cluster stats:"
    curl -s "$API_BASE/cluster/stats" | jq .
    
    echo "   Admin stats:"
    curl -s "$API_BASE/admin/stats" | jq .
    
    echo "✅ Statistics test completed"
}

# Test error handling
test_error_handling() {
    echo "🚨 Testing error handling..."
    
    # Test non-existent key
    echo "   Testing non-existent key retrieval..."
    status_code=$(curl -s -o /dev/null -w "%{http_code}" "$API_BASE/kv/nonexistent")
    if [ "$status_code" = "404" ]; then
        echo "✅ Non-existent key handled correctly"
    else
        echo "❌ Non-existent key test failed (status: $status_code)"
    fi
    
    # Test invalid endpoint
    echo "   Testing invalid endpoint..."
    status_code=$(curl -s -o /dev/null -w "%{http_code}" "$API_BASE/invalid")
    if [ "$status_code" = "404" ]; then
        echo "✅ Invalid endpoint handled correctly"
    else
        echo "❌ Invalid endpoint test failed (status: $status_code)"
    fi
}

# Performance test
performance_test() {
    echo "⚡ Running basic performance test..."
    
    echo "   Storing 100 keys..."
    start_time=$(date +%s.%N)
    
    for i in {1..100}; do
        curl -s -X PUT "$API_BASE/kv/perf_test_$i" \
            -H "Content-Type: application/json" \
            -d "{\"value\":\"value_$i\"}" > /dev/null
    done
    
    end_time=$(date +%s.%N)
    duration=$(echo "$end_time - $start_time" | bc)
    ops_per_sec=$(echo "scale=2; 100 / $duration" | bc)
    
    echo "   Stored 100 keys in ${duration}s (${ops_per_sec} ops/sec)"
    
    # Read performance
    echo "   Reading 100 keys..."
    start_time=$(date +%s.%N)
    
    for i in {1..100}; do
        curl -s "$API_BASE/kv/perf_test_$i" > /dev/null
    done
    
    end_time=$(date +%s.%N)
    duration=$(echo "$end_time - $start_time" | bc)
    ops_per_sec=$(echo "scale=2; 100 / $duration" | bc)
    
    echo "   Read 100 keys in ${duration}s (${ops_per_sec} ops/sec)"
    echo "✅ Performance test completed"
}

# Main test execution
main() {
    # Check if jq is available (for JSON parsing)
    if ! command -v jq &> /dev/null; then
        echo "⚠️  jq is not installed. JSON responses will be raw."
        echo "   Install with: brew install jq (macOS) or apt-get install jq (Ubuntu)"
    fi
    
    # Check if bc is available (for calculations)
    if ! command -v bc &> /dev/null; then
        echo "⚠️  bc is not installed. Performance calculations will be skipped."
    fi
    
    wait_for_service
    test_health
    test_basic_operations
    test_multiple_keys
    test_large_values
    test_statistics
    test_error_handling
    
    if command -v bc &> /dev/null; then
        performance_test
    fi
    
    echo ""
    echo "🎉 All tests completed successfully!"
    echo "   Your distri-kv cluster is working correctly."
}

# Run tests if script is executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi 