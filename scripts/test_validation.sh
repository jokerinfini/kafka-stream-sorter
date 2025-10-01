#!/usr/bin/env bash
set -eu
if [ -n "${BASH_VERSION:-}" ]; then
  set -o pipefail
fi

# Integration test script to validate sorted outputs (requirement #5)
# This script consumes a sample from each sorted topic and validates ordering

echo "========================================"
echo " Kafka Sort Pipeline - Validation Test"
echo "========================================"
echo ""

# Configuration
SAMPLE_SIZE=1000
KAFKA_CONTAINER="kafka"
BOOTSTRAP_SERVER="kafka:9092"

# Function to validate numeric ordering (for ID topic)
validate_id_sort() {
    local topic=$1
    echo "[Test] Validating numeric sort for topic: $topic"
    
    # Consume sample and extract IDs
    local ids=$(docker compose exec -T $KAFKA_CONTAINER bash -lc "
        kafka-console-consumer --bootstrap-server $BOOTSTRAP_SERVER \
        --topic $topic --from-beginning --max-messages $SAMPLE_SIZE --timeout-ms 10000 2>/dev/null \
        | cut -d',' -f1
    ")
    
    if [ -z "$ids" ]; then
        echo "  [FAIL] No data found in topic $topic"
        return 1
    fi
    
    # Check if IDs are in ascending order
    local prev=-2147483648  # Min int32
    local count=0
    local errors=0
    
    while IFS= read -r id; do
        if [ -n "$id" ]; then
            count=$((count + 1))
            if [ "$id" -lt "$prev" ]; then
                echo "  [ERROR] Out of order: $prev > $id at position $count"
                errors=$((errors + 1))
                if [ $errors -ge 5 ]; then
                    echo "  [FAIL] Too many errors, stopping validation"
                    return 1
                fi
            fi
            prev=$id
        fi
    done <<< "$ids"
    
    if [ $errors -eq 0 ]; then
        echo "  [PASS] All $count IDs are in ascending numeric order"
        return 0
    else
        echo "  [FAIL] Found $errors ordering violations out of $count records"
        return 1
    fi
}

# Function to validate lexicographic ordering (for name/continent topics)
validate_lexicographic_sort() {
    local topic=$1
    local field_index=$2
    local field_name=$3
    
    echo "[Test] Validating lexicographic sort for topic: $topic (field: $field_name)"
    
    # Consume sample and extract the specified field
    local fields=$(docker compose exec -T $KAFKA_CONTAINER bash -lc "
        kafka-console-consumer --bootstrap-server $BOOTSTRAP_SERVER \
        --topic $topic --from-beginning --max-messages $SAMPLE_SIZE --timeout-ms 10000 2>/dev/null \
        | cut -d',' -f$field_index
    ")
    
    if [ -z "$fields" ]; then
        echo "  [FAIL] No data found in topic $topic"
        return 1
    fi
    
    # Check if fields are in lexicographic order
    local prev=""
    local count=0
    local errors=0
    
    while IFS= read -r field; do
        if [ -n "$field" ]; then
            count=$((count + 1))
            if [ -n "$prev" ] && [[ "$field" < "$prev" ]]; then
                echo "  [ERROR] Out of order: '$prev' > '$field' at position $count"
                errors=$((errors + 1))
                if [ $errors -ge 5 ]; then
                    echo "  [FAIL] Too many errors, stopping validation"
                    return 1
                fi
            fi
            prev=$field
        fi
    done <<< "$fields"
    
    if [ $errors -eq 0 ]; then
        echo "  [PASS] All $count ${field_name}s are in ascending lexicographic order"
        return 0
    else
        echo "  [FAIL] Found $errors ordering violations out of $count records"
        return 1
    fi
}

# Function to check topic exists and has data
check_topic() {
    local topic=$1
    echo "[Check] Verifying topic exists: $topic"
    
    local exists=$(docker compose exec -T $KAFKA_CONTAINER bash -lc "
        kafka-topics --bootstrap-server $BOOTSTRAP_SERVER --list 2>/dev/null | grep -c \"^${topic}$\" || true
    ")
    
    if [ "$exists" -eq 0 ]; then
        echo "  [FAIL] Topic $topic does not exist"
        return 1
    fi
    
    # Check message count
    local describe=$(docker compose exec -T $KAFKA_CONTAINER bash -lc "
        kafka-topics --bootstrap-server $BOOTSTRAP_SERVER --describe --topic $topic 2>/dev/null
    ")
    
    echo "  [PASS] Topic exists"
    echo "$describe" | grep -E "Partition.*Leader" | head -3
    return 0
}

# Main test execution
echo "[Info] Sample size: $SAMPLE_SIZE records per topic"
echo "[Info] Bootstrap server: $BOOTSTRAP_SERVER"
echo ""

# Check if Kafka is ready
echo "[Check] Verifying Kafka connection..."
if ! docker compose exec -T $KAFKA_CONTAINER bash -lc "
    kafka-topics --bootstrap-server $BOOTSTRAP_SERVER --list >/dev/null 2>&1
"; then
    echo "[FAIL] Cannot connect to Kafka. Is the service running?"
    echo "Run: docker compose up -d"
    exit 1
fi
echo "  [PASS] Kafka is reachable"
echo ""

# Track test results
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Test 1: Check source topic
echo "=== Test 1: Source Topic ==="
if check_topic "source"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
TESTS_RUN=$((TESTS_RUN + 1))
echo ""

# Test 2: Validate sorted_id topic
echo "=== Test 2: Sorted ID Topic ==="
if check_topic "sorted_id" && validate_id_sort "sorted_id"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
TESTS_RUN=$((TESTS_RUN + 1))
echo ""

# Test 3: Validate sorted_name topic
echo "=== Test 3: Sorted Name Topic ==="
if check_topic "sorted_name" && validate_lexicographic_sort "sorted_name" 2 "name"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
TESTS_RUN=$((TESTS_RUN + 1))
echo ""

# Test 4: Validate sorted_continent topic
echo "=== Test 4: Sorted Continent Topic ==="
if check_topic "sorted_continent" && validate_lexicographic_sort "sorted_continent" 4 "continent"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
TESTS_RUN=$((TESTS_RUN + 1))
echo ""

# Summary
echo "========================================"
echo " Test Summary"
echo "========================================"
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo "[SUCCESS] All validation tests passed! ✓"
    # Pause so the window doesn't close immediately when run via double-click
    read -p "Press Enter to exit" || true
    exit 0
else
    echo "[FAILURE] Some tests failed. Please review the output above."
    # Pause so the window doesn't close immediately when run via double-click
    read -p "Press Enter to exit" || true
    exit 1
fi

