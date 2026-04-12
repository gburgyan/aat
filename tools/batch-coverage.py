#!/usr/bin/env python3
"""Analyze AAT batch run results and report node test coverage."""

import argparse
import json
import os
import re
import sys


def main():
    parser = argparse.ArgumentParser(
        description="Analyze AAT batch run results and report node test coverage."
    )
    parser.add_argument("batch_dir", help="Path to batch run directory (containing batch.json)")
    parser.add_argument("graph_path", help="Path to graph.yaml defining all API nodes")
    parser.add_argument("--json", action="store_true", dest="json_output",
                        help="Output machine-readable JSON instead of terminal text")
    args = parser.parse_args()

    # Load graph nodes
    graph_nodes = load_graph_nodes(args.graph_path)
    if not graph_nodes:
        print("Error: no nodes found in graph", file=sys.stderr)
        sys.exit(1)

    # Load batch
    batch_path = os.path.join(args.batch_dir, "batch.json")
    if not os.path.exists(batch_path):
        print(f"Error: batch.json not found in {args.batch_dir}", file=sys.stderr)
        sys.exit(1)

    with open(batch_path) as f:
        batch = json.load(f)

    metadata = batch.get("metadata", {})
    batch_result = batch.get("result", {})
    runs = batch.get("runs", [])

    # Collect step results and planned nodes across all non-skipped runs
    step_results = []  # list of (node_name, passed_bool)
    planned_nodes = set()  # nodes that appeared in any plan

    for entry in runs:
        if entry.get("skipped"):
            continue

        run_id = entry.get("runId", "")
        archive_path = os.path.join(args.batch_dir, run_id, "archive.json")
        if not os.path.exists(archive_path):
            print(f"Warning: archive not found: {archive_path}", file=sys.stderr)
            continue

        try:
            with open(archive_path) as f:
                archive = json.load(f)
        except (json.JSONDecodeError, OSError) as e:
            print(f"Warning: failed to load {archive_path}: {e}", file=sys.stderr)
            continue

        # Collect planned nodes from the plan embedded in archive metadata
        plan_exec = archive.get("metadata", {}).get("plan", {}).get("execution", {})
        for step in plan_exec.get("steps", []):
            node = step.get("node", "")
            if node:
                planned_nodes.add(node)
        for step in plan_exec.get("cleanup", []):
            node = step.get("node", "")
            if node:
                planned_nodes.add(node)

        for step in archive.get("steps", []):
            node = step.get("node", "")
            if node:
                step_results.append((node, is_step_passed(step)))

        for step in archive.get("cleanup", []):
            node = step.get("node", "")
            if node:
                step_results.append((node, is_step_passed(step)))

    # Aggregate coverage
    coverage = aggregate_coverage(graph_nodes, step_results)

    # Output
    if args.json_output:
        print(format_json_report(graph_nodes, coverage, planned_nodes, metadata, batch_result, runs))
    else:
        print(format_report(graph_nodes, coverage, planned_nodes, metadata, batch_result, runs))


def load_graph_nodes(path):
    """Extract node names from graph.yaml using line-based parsing."""
    try:
        with open(path) as f:
            lines = f.readlines()
    except OSError as e:
        print(f"Error: cannot read graph file: {e}", file=sys.stderr)
        sys.exit(1)

    nodes = []
    in_nodes = False
    node_indent = None

    for line in lines:
        stripped = line.rstrip()

        # Skip empty lines and comments when looking for nodes section
        if not stripped or stripped.lstrip().startswith("#"):
            if in_nodes:
                continue
            continue

        # Detect the start of the nodes section
        if stripped == "nodes:" or stripped == "nodes: ":
            in_nodes = True
            continue

        if not in_nodes:
            continue

        # Determine indent level
        indent = len(line) - len(line.lstrip())

        # First non-comment line after nodes: sets the node key indent
        if node_indent is None:
            node_indent = indent

        # A line at indent 0 means we've left the nodes section
        if indent == 0 and not line[0].isspace():
            break

        # Node keys are at exactly the node_indent level
        if indent == node_indent:
            match = re.match(r"^\s+([\w][\w-]*):\s*$", line.rstrip())
            if match:
                nodes.append(match.group(1))

    return sorted(nodes)


def is_step_passed(step):
    """Mirror Go's stepRecordPassed logic from archive/summary.go."""
    if step.get("error"):
        return False
    validation = step.get("validation")
    if validation and not validation.get("passed", True):
        return False
    expect = step.get("expectFailure")
    if expect and not expect.get("passed", True):
        return False
    if step.get("responseBodyError"):
        return False
    return True


def aggregate_coverage(graph_nodes, step_results):
    """Build per-node execution counts from step results."""
    coverage = {}
    for node in graph_nodes:
        coverage[node] = {"executions": 0, "passed": 0, "failed": 0}

    for node, passed in step_results:
        if node not in coverage:
            # Node not in graph (shouldn't happen, but handle gracefully)
            coverage[node] = {"executions": 0, "passed": 0, "failed": 0}
        coverage[node]["executions"] += 1
        if passed:
            coverage[node]["passed"] += 1
        else:
            coverage[node]["failed"] += 1

    return coverage


def format_report(graph_nodes, coverage, planned_nodes, metadata, batch_result, runs):
    """Format human-readable terminal report."""
    lines = []

    # Header
    batch_id = metadata.get("batchId", "unknown")
    timestamp = metadata.get("timestamp", "")

    total_runs = batch_result.get("totalRuns", len(runs))
    passed_runs = batch_result.get("passedRuns", 0)
    failed_runs = batch_result.get("failedRuns", 0)
    error_runs = batch_result.get("errorRuns", 0)
    skipped_runs = batch_result.get("skippedRuns", 0)

    lines.append("=== AAT Node Coverage Report ===")
    lines.append(f"Batch:     {batch_id}")
    if timestamp:
        lines.append(f"Timestamp: {timestamp}")
    lines.append(f"Runs:      {total_runs} total ({passed_runs} passed, {failed_runs} failed, {error_runs} error, {skipped_runs} skipped)")
    lines.append("")

    # Categorize nodes
    tested_passed = []
    tested_failed = []
    planned_not_reached = []
    not_in_plan = []

    for node in sorted(graph_nodes):
        c = coverage.get(node, {"executions": 0, "passed": 0, "failed": 0})
        if c["executions"] == 0:
            if node in planned_nodes:
                planned_not_reached.append(node)
            else:
                not_in_plan.append(node)
        elif c["failed"] > 0:
            tested_failed.append((node, c))
        else:
            tested_passed.append((node, c))

    covered = len(tested_passed) + len(tested_failed)
    total_nodes = len(graph_nodes)
    pct = (covered / total_nodes * 100) if total_nodes > 0 else 0

    lines.append(f"Graph:     {total_nodes} nodes")
    lines.append(f"Covered:   {covered} nodes ({pct:.1f}%)")
    lines.append(f"Planned but not reached: {len(planned_not_reached)} nodes")
    lines.append(f"Not in any plan:         {len(not_in_plan)} nodes")
    lines.append("")

    # Find max node name length for alignment
    all_names = ([n for n, _ in tested_passed] + [n for n, _ in tested_failed]
                 + planned_not_reached + not_in_plan)
    max_name = max((len(n) for n in all_names), default=0)

    # Tested & Passed
    lines.append(f"--- Tested & Passed ({len(tested_passed)} nodes) ---")
    for node, c in tested_passed:
        lines.append(f"  {node:<{max_name}}  {c['executions']} executions ({c['passed']} passed)")
    lines.append("")

    # Tested & Failed
    lines.append(f"--- Tested & Failed ({len(tested_failed)} nodes) ---")
    for node, c in tested_failed:
        lines.append(f"  {node:<{max_name}}  {c['executions']} executions ({c['passed']} passed, {c['failed']} failed)")
    lines.append("")

    # Planned but not reached
    lines.append(f"--- Planned but Not Reached ({len(planned_not_reached)} nodes) ---")
    for node in planned_not_reached:
        lines.append(f"  {node}")
    lines.append("")

    # Not in any plan
    lines.append(f"--- Not in Any Plan ({len(not_in_plan)} nodes) ---")
    for node in not_in_plan:
        lines.append(f"  {node}")

    return "\n".join(lines)


def format_json_report(graph_nodes, coverage, planned_nodes, metadata, batch_result, runs):
    """Format machine-readable JSON report."""
    tested_passed = []
    tested_failed = []
    planned_not_reached = []
    not_in_plan = []

    for node in sorted(graph_nodes):
        c = coverage.get(node, {"executions": 0, "passed": 0, "failed": 0})
        if c["executions"] == 0:
            if node in planned_nodes:
                planned_not_reached.append(node)
            else:
                not_in_plan.append(node)
        elif c["failed"] > 0:
            tested_failed.append({
                "node": node,
                "executions": c["executions"],
                "passed": c["passed"],
                "failed": c["failed"],
            })
        else:
            tested_passed.append({
                "node": node,
                "executions": c["executions"],
                "passed": c["passed"],
                "failed": 0,
            })

    covered = len(tested_passed) + len(tested_failed)
    total_nodes = len(graph_nodes)
    pct = round(covered / total_nodes * 100, 1) if total_nodes > 0 else 0

    report = {
        "batch": {
            "batchId": metadata.get("batchId", "unknown"),
            "timestamp": metadata.get("timestamp", ""),
            "totalRuns": batch_result.get("totalRuns", len(runs)),
            "passedRuns": batch_result.get("passedRuns", 0),
            "failedRuns": batch_result.get("failedRuns", 0),
            "errorRuns": batch_result.get("errorRuns", 0),
            "skippedRuns": batch_result.get("skippedRuns", 0),
        },
        "graph": {
            "totalNodes": total_nodes,
        },
        "summary": {
            "coveredNodes": covered,
            "plannedNotReached": len(planned_not_reached),
            "notInAnyPlan": len(not_in_plan),
            "coveragePercent": pct,
            "testedAndPassed": len(tested_passed),
            "testedAndFailed": len(tested_failed),
        },
        "nodes": {
            "testedAndPassed": tested_passed,
            "testedAndFailed": tested_failed,
            "plannedNotReached": planned_not_reached,
            "notInAnyPlan": not_in_plan,
        },
    }

    return json.dumps(report, indent=2)


if __name__ == "__main__":
    main()
