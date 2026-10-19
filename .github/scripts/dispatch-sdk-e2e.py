#!/usr/bin/env python3
"""Dispatch the server SDK battery and propagate the exact run's conclusion.

Canonical caller: copy this file into .github/scripts/dispatch-sdk-e2e.py in
analytics-go and rudder-sdk-node. It requires only Python's standard library.
"""
from __future__ import annotations

import json
import os
import re
import sys
import time
from urllib.error import HTTPError
from urllib.request import Request, urlopen

REPOSITORY = "rudderlabs/rudder-sdk-e2e-tests"
WORKFLOW = "server-sdk-validation.yml"
API = f"https://api.github.com/repos/{REPOSITORY}"
PILOT_REPOSITORIES = {"rudderlabs/analytics-go", "rudderlabs/rudder-sdk-node"}


def request(path, token, payload=None):
    body = json.dumps(payload).encode() if payload is not None else None
    headers = {"Accept": "application/vnd.github+json", "Authorization": f"Bearer {token}",
               "X-GitHub-Api-Version": "2026-03-10", "Content-Type": "application/json"}
    try:
        with urlopen(Request(API + path, body, headers), timeout=30) as response:
            content = response.read()
            return json.loads(content) if content else {}
    except HTTPError as error:
        # Never print an API response that could include credentials or inputs.
        raise RuntimeError(f"GitHub API request failed: HTTP {error.code}") from error


def inputs(environment):
    repository = environment["GITHUB_REPOSITORY"]
    sha = environment["GITHUB_SHA"]
    suite = environment.get("SDK_E2E_SUITE", "reliability")
    if repository not in PILOT_REPOSITORIES:
        raise ValueError("repository is not in the Go/Node pilot allowlist")
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("GITHUB_SHA must be a full lowercase commit SHA")
    if suite not in ("reliability", "full"):
        raise ValueError("SDK_E2E_SUITE must be reliability or full")
    run_id, attempt = environment["GITHUB_RUN_ID"], environment["GITHUB_RUN_ATTEMPT"]
    if not run_id.isdigit() or not attempt.isdigit():
        raise ValueError("invalid caller run identity")
    correlation = f"{repository.split('/')[1]}-{run_id}-{attempt}-{sha}"
    return {"selection": "auto", "source_repository": repository, "source_sha": sha,
            "suite": suite, "request_id": correlation}


def validate_run(run, run_id, correlation):
    if (run.get("id") != run_id or run.get("repository", {}).get("full_name") != REPOSITORY
            or run.get("event") != "workflow_dispatch" or run.get("head_branch") != "main"
            or run.get("path") != f".github/workflows/{WORKFLOW}"
            or run.get("display_title") != f"sdk-e2e {correlation}"):
        raise RuntimeError("central run identity does not match this dispatch")


def dispatch_and_wait(token, selected, timeout=3000, poll=15):
    started = time.monotonic()
    dispatched = request(f"/actions/workflows/{WORKFLOW}/dispatches", token,
                         {"ref": "main", "inputs": selected})
    run_id = dispatched.get("workflow_run_id")
    if type(run_id) is not int or run_id <= 0:
        raise RuntimeError("dispatch did not return a workflow_run_id; refusing to guess a matching run")
    url = f"https://github.com/{REPOSITORY}/actions/runs/{run_id}"
    print(f"Server SDK validation: {url}", flush=True)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
            summary.write(f"[Server SDK validation]({url})\n\nCommit: `{selected['source_sha']}`\n")
    while time.monotonic() - started < timeout:
        run = request(f"/actions/runs/{run_id}", token)
        validate_run(run, run_id, selected["request_id"])
        if run.get("status") == "completed":
            if run.get("conclusion") != "success":
                raise RuntimeError(f"server SDK validation concluded {run.get('conclusion')}; see {url}")
            return url
        time.sleep(min(poll, max(0, timeout - (time.monotonic() - started))))
    raise RuntimeError(f"timed out waiting for server SDK validation; central run may still be active: {url}")


if __name__ == "__main__":
    try:
        token = os.environ.get("GH_TOKEN")
        if not token:
            raise ValueError("GH_TOKEN is required for dispatch and polling")
        dispatch_and_wait(token, inputs(os.environ))
    except (ValueError, KeyError, OSError, RuntimeError) as error:
        print(f"Server SDK validation failed: {error}", file=sys.stderr)
        sys.exit(1)
