#!/bin/bash

# Performance Benchmarks for pgdbtemplate.
#
# This script runs comprehensive benchmarks comparing template
# vs traditional database creation.

set -e

echo "🚀 Running pgdbtemplate Performance Benchmarks"
echo "=============================================="
echo ""

# Run benchmarks.
echo "🔄 Running Core Performance Comparison..."
echo "----------------------------------------"
go test -bench="BenchmarkDatabaseCreation_.*_5Tables" -benchmem -count=3

echo ""
echo "🔄 Running Schema Complexity Analysis..."
echo "---------------------------------------"
go test -bench="BenchmarkDatabaseCreation_.*Table" -benchmem -count=1

echo ""
echo "🔄 Running Scaling Analysis..."
echo "------------------------------"
go test -bench="BenchmarkScalingComparison_Sequential" -benchmem -timeout 10m

echo ""
echo "🔄 Running Template Initialization Benchmark..."
echo "-----------------------------------------------"
go test -bench="BenchmarkTemplateInitialization" -benchmem -count=3

echo ""
echo "🔄 Running Concurrent Performance Test..."
echo "-----------------------------------------"
go test -bench="BenchmarkConcurrentDatabaseCreation_Template" -benchmem -count=3

echo ""
echo "✅ All benchmarks completed successfully!"
echo ""
echo "📖 For detailed analysis, see BENCHMARKS.md"
