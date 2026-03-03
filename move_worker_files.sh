#!/bin/bash
set -e

# Step 1: Move 19 source files
git mv internal/nebula/worker.go internal/nebula/worker/worker.go
git mv internal/nebula/worker_exec.go internal/nebula/worker/exec.go
git mv internal/nebula/worker_fabric.go internal/nebula/worker/fabric.go
git mv internal/nebula/worker_options.go internal/nebula/worker/options.go
git mv internal/nebula/worker_healing.go internal/nebula/worker/healing.go
git mv internal/nebula/scheduler.go internal/nebula/worker/scheduler.go
git mv internal/nebula/tracker.go internal/nebula/worker/tracker.go
git mv internal/nebula/gate.go internal/nebula/worker/gate.go
git mv internal/nebula/progress.go internal/nebula/worker/progress.go
git mv internal/nebula/metrics.go internal/nebula/worker/metrics.go
git mv internal/nebula/metrics_store.go internal/nebula/worker/metrics_store.go
git mv internal/nebula/dashboard.go internal/nebula/worker/dashboard.go
git mv internal/nebula/watcher.go internal/nebula/worker/watcher.go
git mv internal/nebula/hotreload.go internal/nebula/worker/hotreload.go
git mv internal/nebula/decompose.go internal/nebula/worker/decompose.go
git mv internal/nebula/decompose_dag.go internal/nebula/worker/decompose_dag.go
git mv internal/nebula/healing.go internal/nebula/worker/failure_diagnosis.go
git mv internal/nebula/healing_remediate.go internal/nebula/worker/healing_remediate.go
git mv internal/nebula/checkpoint.go internal/nebula/worker/phase_checkpoint.go

echo "Source files moved"

# Step 2: Move test files
# Tests for worker.go
git mv internal/nebula/worker_board_integration_test.go internal/nebula/worker/board_integration_test.go
git mv internal/nebula/worker_board_test.go internal/nebula/worker/board_test.go
git mv internal/nebula/worker_changes_test.go internal/nebula/worker/changes_test.go
git mv internal/nebula/worker_decompose_test.go internal/nebula/worker/worker_decompose_test.go
git mv internal/nebula/worker_resume_test.go internal/nebula/worker/resume_test.go

# Tests for worker_fabric.go
git mv internal/nebula/worker_fabric_integration_test.go internal/nebula/worker/fabric_integration_test.go
git mv internal/nebula/worker_fabric_test.go internal/nebula/worker/fabric_test.go

# Tests for other moved files
git mv internal/nebula/scheduler_test.go internal/nebula/worker/scheduler_test.go
git mv internal/nebula/tracker_test.go internal/nebula/worker/tracker_test.go
git mv internal/nebula/gate_test.go internal/nebula/worker/gate_test.go
git mv internal/nebula/metrics_test.go internal/nebula/worker/metrics_test.go
git mv internal/nebula/metrics_store_test.go internal/nebula/worker/metrics_store_test.go
git mv internal/nebula/dashboard_test.go internal/nebula/worker/dashboard_test.go
git mv internal/nebula/watcher_test.go internal/nebula/worker/watcher_test.go
git mv internal/nebula/decompose_test.go internal/nebula/worker/decompose_test.go
git mv internal/nebula/decompose_dag_test.go internal/nebula/worker/decompose_dag_test.go
git mv internal/nebula/healing_test.go internal/nebula/worker/failure_diagnosis_test.go
git mv internal/nebula/checkpoint_test.go internal/nebula/worker/phase_checkpoint_test.go

# hail_timeout_test.go might test worker functionality - check and move if relevant
git mv internal/nebula/hail_timeout_test.go internal/nebula/worker/hail_timeout_test.go

echo "Test files moved"

# Step 3: Update package declarations
for f in internal/nebula/worker/*.go; do
  if [ "$f" = "internal/nebula/worker/doc.go" ]; then
    continue
  fi
  sed -i '' 's/^package nebula$/package worker/' "$f"
done

echo "Package declarations updated"
echo "Done!"
