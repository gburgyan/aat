#!/usr/bin/env python3
"""Convert AAT archive JSON to JUnit XML for Datadog Test Visibility."""

import argparse
import json
import os
import sys
import xml.etree.ElementTree as ET
from xml.dom.minidom import parseString


def main():
    parser = argparse.ArgumentParser(
        description="Convert AAT archive JSON to JUnit XML for Datadog Test Visibility."
    )
    parser.add_argument("archive_path", help="Path to archive.json or batch.json")
    parser.add_argument("-o", "--output", metavar="FILE", help="Output file (default: stdout)")
    parser.add_argument("--service", metavar="NAME", help="Service name added as dd_tags property")
    parser.add_argument("--pretty", action="store_true", help="Pretty-print the XML output")
    args = parser.parse_args()

    with open(args.archive_path) as f:
        data = json.load(f)

    fmt = detect_format(data)
    if fmt == "batch":
        root = convert_batch(data, os.path.dirname(os.path.abspath(args.archive_path)))
    elif fmt == "archive":
        root = ET.Element("testsuites")
        root.append(convert_archive(data))
    else:
        print(f"Error: unrecognized archive format in {args.archive_path}", file=sys.stderr)
        sys.exit(1)

    if args.service:
        for ts in root.iter("testsuite"):
            add_property(ts, "dd_tags[service]", args.service)

    xml_bytes = ET.tostring(root, encoding="unicode", xml_declaration=False)
    xml_str = '<?xml version="1.0" encoding="UTF-8"?>\n'
    if args.pretty:
        xml_str += parseString(xml_bytes).toprettyxml(indent="  ").split("\n", 1)[1]
    else:
        xml_str += xml_bytes + "\n"

    if args.output:
        with open(args.output, "w") as f:
            f.write(xml_str)
    else:
        sys.stdout.write(xml_str)


def detect_format(data):
    if "runs" in data:
        return "batch"
    if "steps" in data:
        return "archive"
    return None


def convert_archive(archive, batch_id=None, plan_name=None, permutation=None, layers=None):
    """Convert a single archive to a <testsuite> element."""
    metadata = archive.get("metadata", {})
    result = archive.get("result", {})
    steps = archive.get("steps", [])
    cleanup = archive.get("cleanup", [])

    run_id = metadata.get("runId", "unknown")

    testcases = []
    failures = 0
    total_time = 0.0

    for i, step in enumerate(steps):
        tc = convert_step(step, i, run_id, cleanup=False)
        testcases.append(tc)
        if tc.find("failure") is not None:
            failures += 1
        total_time += float(tc.get("time", "0"))

    for i, step in enumerate(cleanup):
        tc = convert_step(step, len(steps) + i, run_id, cleanup=True)
        testcases.append(tc)
        if tc.find("failure") is not None:
            failures += 1
        total_time += float(tc.get("time", "0"))

    ts = ET.Element("testsuite")
    ts.set("name", run_id)
    ts.set("tests", str(len(testcases)))
    ts.set("failures", str(failures))
    ts.set("errors", "0")
    ts.set("time", f"{total_time:.3f}")

    timestamp = metadata.get("timestamp", "")
    if timestamp:
        # Normalize to ISO without timezone offset details beyond what JUnit expects
        ts.set("timestamp", timestamp)

    # Suite-level properties
    add_property(ts, "dd_tags[aat.outcome]", result.get("outcome", "unknown"))
    add_property(ts, "dd_tags[aat.run_id]", run_id)
    if metadata.get("environment"):
        add_property(ts, "dd_tags[aat.environment]", metadata["environment"])
    if metadata.get("toolVersion"):
        add_property(ts, "dd_tags[aat.tool_version]", metadata["toolVersion"])

    plan = metadata.get("plan", {})
    plan_meta = plan.get("metadata", {}) if plan else {}
    if plan_meta.get("prompt"):
        add_property(ts, "dd_tags[aat.plan_prompt]", plan_meta["prompt"])

    if batch_id:
        add_property(ts, "dd_tags[aat.batch_id]", batch_id)
    if plan_name:
        add_property(ts, "dd_tags[aat.plan_name]", plan_name)
    if permutation:
        add_property(ts, "dd_tags[aat.permutation]", permutation)
    if layers:
        add_property(ts, "dd_tags[aat.layers]", ", ".join(layers))

    for tc in testcases:
        ts.append(tc)

    return ts


def convert_step(step, index, run_id, cleanup=False):
    """Convert a single step to a <testcase> element."""
    step_id = step.get("stepId") or step.get("node", f"step-{index}")
    node = step.get("node", "unknown")
    name = f"[cleanup] {step_id}" if cleanup else step_id
    duration_s = step.get("duration_ms", 0) / 1000.0

    tc = ET.Element("testcase")
    tc.set("name", name)
    tc.set("classname", node)
    tc.set("time", f"{duration_s:.3f}")

    # Check failure conditions in priority order
    failed, failure_type, failure_msg = is_step_failed(step, cleanup=cleanup)
    if failed:
        failure = ET.SubElement(tc, "failure")
        failure.set("type", failure_type)
        failure.set("message", truncate(failure_msg, 1000))
        body = build_failure_body(step)
        if body:
            failure.text = body

    # Properties (dd_tags)
    response = step.get("response")
    request = step.get("request")

    if response and response.get("status"):
        add_property(tc, "dd_tags[http.status_code]", str(response["status"]))
    if request:
        if request.get("method"):
            add_property(tc, "dd_tags[http.method]", request["method"])
        if request.get("url"):
            add_property(tc, "dd_tags[http.url]", request["url"])

    add_property(tc, "dd_tags[aat.node]", node)
    add_property(tc, "dd_tags[aat.run_id]", run_id)
    add_property(tc, "dd_tags[aat.step_index]", str(index))

    return tc


def is_step_failed(step, cleanup=False):
    """Determine if a step failed. Returns (failed, type, message)."""
    # 1. Explicit error
    error = step.get("error")
    if error:
        error_class = step.get("errorClassification", {})
        category = error_class.get("category", "error") if error_class else "error"
        return True, category, error

    # 2. Validation failure
    validation = step.get("validation")
    if validation and not validation.get("passed", True):
        failed_assertions = [
            r.get("message", "assertion failed")
            for r in validation.get("results", [])
            if not r.get("passed", True) and not r.get("skipped", False)
        ]
        msg = "; ".join(failed_assertions) if failed_assertions else "validation failed"
        return True, "assertion", msg

    # 3. Expected failure didn't occur
    expect = step.get("expectFailure")
    if expect and not expect.get("passed", True):
        expected = expect.get("expected", [])
        actual = expect.get("actual", 0)
        msg = f"Expected status {expected}, got {actual}"
        return True, "expected_failure", msg

    # 4. HTTP error without passing expectFailure
    # Cleanup steps are best-effort; HTTP errors don't count as failures
    if not cleanup:
        response = step.get("response")
        if response:
            status = response.get("status", 0)
            if status >= 400:
                # Check if expectFailure passed (step should count as passing)
                if expect and expect.get("passed", False):
                    return False, "", ""
                return True, f"HTTP_{status}", f"HTTP {status}"

    return False, "", ""


def build_failure_body(step):
    """Build detailed failure body text."""
    parts = []

    error = step.get("error")
    if error:
        parts.append(f"Error: {error}")
        error_class = step.get("errorClassification")
        if error_class:
            parts.append(f"Category: {error_class.get('category', 'unknown')}")
            if error_class.get("detail") and error_class["detail"] != error:
                parts.append(f"Detail: {error_class['detail']}")

    validation = step.get("validation")
    if validation and not validation.get("passed", True):
        for r in validation.get("results", []):
            if not r.get("passed", True) and not r.get("skipped", False):
                parts.append(f"Assertion ({r.get('type', '?')}): {r.get('message', '?')}")

    expect = step.get("expectFailure")
    if expect and not expect.get("passed", True):
        parts.append(f"Expected failure with status {expect.get('expected', [])}, got {expect.get('actual', '?')}")

    return "\n".join(parts) if parts else None


def add_property(element, name, value):
    """Add a <property> to the <properties> child of element, creating it if needed."""
    props = element.find("properties")
    if props is None:
        # Insert properties before testcase children (failure elements)
        props = ET.Element("properties")
        element.insert(0, props)
    prop = ET.SubElement(props, "property")
    prop.set("name", name)
    prop.set("value", value)


def convert_batch(batch, batch_dir):
    """Convert a batch archive to <testsuites>, loading per-run archives."""
    root = ET.Element("testsuites")
    metadata = batch.get("metadata", {})
    batch_id = metadata.get("batchId", "unknown-batch")

    for entry in batch.get("runs", []):
        run_id = entry.get("runId", "")
        if entry.get("skipped"):
            continue

        archive_path = os.path.join(batch_dir, run_id, "archive.json")
        if not os.path.exists(archive_path):
            print(f"Warning: archive not found: {archive_path}", file=sys.stderr)
            continue

        try:
            with open(archive_path) as f:
                archive = json.load(f)
        except (json.JSONDecodeError, OSError) as e:
            print(f"Warning: failed to load {archive_path}: {e}", file=sys.stderr)
            continue

        ts = convert_archive(
            archive,
            batch_id=batch_id,
            plan_name=entry.get("planName"),
            permutation=entry.get("permutation"),
            layers=entry.get("layers"),
        )
        root.append(ts)

    return root


def truncate(s, max_len):
    """Truncate string to max_len, adding ellipsis if needed."""
    if len(s) <= max_len:
        return s
    return s[:max_len - 3] + "..."


if __name__ == "__main__":
    main()
