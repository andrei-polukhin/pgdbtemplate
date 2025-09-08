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
go test -run=^$ -bench="BenchmarkDatabaseCreation_.*_5Tables" -benchmem -count=3

echo ""
echo "🔄 Running Schema Complexity Analysis..."
echo "---------------------------------------"
go test -run=^$ -bench="BenchmarkDatabaseCreation_.*Table" -benchmem -count=1

echo ""
echo "🔄 Running Scaling Analysis..."
echo "------------------------------"
go test -run=^$ -bench="BenchmarkScalingComparison_Sequential" -benchmem -timeout 10m

echo ""
echo "🔄 Running Template Initialization Benchmark..."
echo "-----------------------------------------------"
go test -run=^$ -bench="BenchmarkTemplateInitialization" -benchmem -count=3

echo ""
echo "🔄 Running Concurrent Performance Test..."
echo "-----------------------------------------"
go test -run=^$ -bench="BenchmarkConcurrentDatabaseCreation" -benchmem -count=3

echo ""
echo "🔄 Running Comprehensive Cleanup Benchmarks..."
echo "----------------------------------------------"
go test -run=^$ -bench="BenchmarkComprehensiveCleanup" -benchmem -count=1

echo ""
echo "🔄 Running Realistic Test Suite Benchmarks..."
echo "--------------------------------------------"
go test -run=^$ -bench="BenchmarkRealisticTestSuite" -benchmem -count=1

echo ""
echo "✅ All benchmarks completed successfully!"
echo ""
echo "📖 For detailed analysis, see BENCHMARKS.md"
