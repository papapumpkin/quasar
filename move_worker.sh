#!/bin/bash
set -e

# Move source files
declare -A MOVES=(
  ["internal/nebula/worker.go"]="internal/nebula/worker/worker.go"
  ["internal/nebula/worker_exec.go"]="internal/nebula/worker/exec.go"
  ["internal/nebula/worker_fabric.go"]="internal/nebula/worker/fabric.go"
  ["internal/nebula/worker_options.go"]="internal/nebula/worker/options.go"
  ["internal/nebula/worker_healing.go"]="internal/nebula/worker/healing.go"
  ["internal/nebula/scheduler.go"]="internal/nebula/worker/scheduler.go"
  ["internal/nebula/tracker.go"]="internal/nebula/worker/tracker.go"
  ["internal/nebula/gate.go"]="internal/nebula/worker/gate.go"
  ["internal/nebula/progress.go"]="internal/nebula/worker/progress.go"
  ["internal/nebula/metrics.go"]="internal/nebula/worker/metrics.go"
  ["internal/nebula/metrics_store.go"]="internal/nebula/worker/metrics_store.go"
  ["internal/nebula/dashboard.go"]="internal/nebula/worker/dashboard.go"
  ["internal/nebula/watcher.go"]="internal/nebula/worker/watcher.go"
  ["internal/nebula/hotreload.go"]="internal/nebula/worker/hotreload.go"
  ["internal/nebula/decompose.go"]="internal/nebula/worker/decompose.go"
  ["internal/nebula/decompose_dag.go"]="internal/nebula/worker/decompose_dag.go"
  ["internal/nebula/healing.go"]="internal/nebula/worker/failure_diagnosis.go"
  ["internal/nebula/checkpoint.go"]="internal/nebula/worker/phase_checkpoint.go"
)

# Move test files
declare -A TEST_MOVES=(
  ["internal/nebula/scheduler_test.go"]="internal/nebula/worker/scheduler_test.go"
  ["internal/nebula/tracker_test.go"]="internal/nebula/worker/tracker_test.go"
  ["internal/nebula/gate_test.go"]="internal/nebula/worker/gate_test.go"
  ["internal/nebula/metrics_test.go"]="internal/nebula/worker/metrics_test.go"
  ["internal/nebula/metrics_store_test.go"]="internal/nebula/worker/metrics_store_test.go"
  ["internal/nebula/dashboard_test.go"]="internal/nebula/worker/dashboard_test.go"
  ["internal/nebula/watcher_test.go"]="internal/nebula/worker/watcher_test.go"
  ["internal/nebula/decompose_test.go"]="internal/nebula/worker/decompose_test.go"
  ["internal/nebula/decompose_dag_test.go"]="internal/nebula/worker/decompose_dag_test.go"
  ["internal/nebula/healing_test.go"]="internal/nebula/worker/failure_diagnosis_test.go"
  ["internal/nebula/checkpoint_test.go"]="internal/nebula/worker/phase_checkpoint_test.go"
  ["internal/nebula/worker_resume_test.go"]="internal/nebula/worker/resume_test.go"
  ["internal/nebula/worker_decompose_test.go"]="internal/nebula/worker/worker_decompose_test.go"
  ["internal/nebula/worker_fabric_test.go"]="internal/nebula/worker/fabric_test.go"
  ["internal/nebula/worker_fabric_integration_test.go"]="internal/nebula/worker/fabric_integration_test.go"
  ["internal/nebula/worker_board_test.go"]="internal/nebula/worker/board_test.go"
  ["internal/nebula/worker_board_integration_test.go"]="internal/nebula/worker/board_integration_test.go"
  ["internal/nebula/worker_changes_test.go"]="internal/nebula/worker/changes_test.go"
)

for src in "${!MOVES[@]}"; do
  dst="${MOVES[$src]}"
  if [ -f "$src" ]; then
    git mv "$src" "$dst"
    echo "Moved $src -> $dst"
  else
    echo "SKIP (not found): $src"
  fi
done

for src in "${!TEST_MOVES[@]}"; do
  dst="${TEST_MOVES[$src]}"
  if [ -f "$src" ]; then
    git mv "$src" "$dst"
    echo "Moved $src -> $dst"
  else
    echo "SKIP (not found): $src"
  fi
done

echo "--- Step 2: Update package declarations ---"
for f in internal/nebula/worker/*.go; do
  sed -i '' 's/^package nebula$/package worker/' "$f"
  echo "Updated package in $f"
done

echo "Done with moves and package updates"
