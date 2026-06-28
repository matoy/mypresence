#!/usr/bin/env python3
"""
myPresence MCP Server — Smoke Tests
Validates that the target myPresence instance is reachable and that the
configured PAT grants access to the expected endpoints.

Usage
-----
    python smoke_tests.py

Environment variables
---------------------
MYPRESENCE_URL    Base URL of the instance (required)
MYPRESENCE_TOKEN  Personal Access Token with at least basic rights (required)

Exit code
---------
0  All tests passed
1  One or more tests failed
"""

import os
import sys
import json
import datetime
from typing import Callable

import httpx

# ── Configuration ──────────────────────────────────────────────────────────────

URL: str = os.environ.get("MYPRESENCE_URL", "").rstrip("/")
TOKEN: str = os.environ.get("MYPRESENCE_TOKEN", "")

# ── Test runner ────────────────────────────────────────────────────────────────

_results: list[tuple[str, bool, str]] = []  # (name, passed, detail)


def test(name: str):
    """Decorator that registers and runs a smoke test function."""
    def decorator(fn: Callable[[], None]):
        try:
            fn()
            _results.append((name, True, ""))
        except AssertionError as exc:
            _results.append((name, False, str(exc)))
        except httpx.HTTPStatusError as exc:
            _results.append((name, False, f"HTTP {exc.response.status_code}: {exc.response.text[:200]}"))
        except Exception as exc:  # noqa: BLE001
            _results.append((name, False, f"{type(exc).__name__}: {exc}"))
        return fn
    return decorator


def _public() -> httpx.Client:
    return httpx.Client(base_url=URL, timeout=10)


def _auth() -> httpx.Client:
    return httpx.Client(
        base_url=URL,
        headers={"Authorization": f"Bearer {TOKEN}"},
        timeout=10,
    )


# ── Pre-flight checks ──────────────────────────────────────────────────────────

def preflight() -> bool:
    ok = True
    if not URL:
        print("ERROR: MYPRESENCE_URL is not set.")
        ok = False
    if not TOKEN:
        print("ERROR: MYPRESENCE_TOKEN is not set.")
        ok = False
    return ok


# ── Smoke tests ────────────────────────────────────────────────────────────────

@test("health_check — public endpoint reachable")
def _():
    with _public() as c:
        r = c.get("/health")
    r.raise_for_status()
    data = r.json()
    assert "status" in data, f"Missing 'status' key: {data}"
    assert data["status"] == "ok", f"Unexpected status: {data['status']}"
    assert "checks" in data, f"Missing 'checks' key: {data}"
    assert data["checks"].get("database") == "ok", f"Database not ok: {data['checks']}"


@test("health_check — response contains 'uptime' field")
def _():
    with _public() as c:
        r = c.get("/health")
    r.raise_for_status()
    data = r.json()
    assert "uptime" in data, f"Missing 'uptime' field: {data}"


@test("auth — unauthenticated request to /api/tokens is rejected")
def _():
    with _public() as c:
        r = c.get("/api/tokens")
    assert r.status_code in (401, 403), (
        f"Expected 401/403 for unauthenticated request, got {r.status_code}"
    )


@test("tokens — list PATs (authenticated)")
def _():
    with _auth() as c:
        r = c.get("/api/tokens")
    r.raise_for_status()
    data = r.json()
    assert isinstance(data, list), f"Expected list, got {type(data).__name__}: {data}"


@test("news — list active banners (authenticated)")
def _():
    with _auth() as c:
        r = c.get("/api/news")
    r.raise_for_status()
    data = r.json()
    assert isinstance(data, list), f"Expected list, got {type(data).__name__}: {data}"


@test("news — each banner has required fields")
def _():
    with _auth() as c:
        r = c.get("/api/news")
    r.raise_for_status()
    banners = r.json()
    for b in banners:
        for field in ("id", "title", "content", "start_date", "end_date"):
            assert field in b, f"Banner missing field '{field}': {b}"


@test("presences — invalid team returns 4xx (not a server crash)")
def _():
    with _auth() as c:
        r = c.get("/api/presences", params={"team_id": 999999, "year": 2026, "month": 1})
    # Expect 4xx (forbidden, not found…) or possibly 200 with empty data — never 5xx
    assert r.status_code < 500, f"Unexpected 5xx: {r.status_code} — {r.text[:200]}"


@test("projects — get current month context (basic user)")
def _():
    now = datetime.date.today()
    with _auth() as c:
        r = c.get("/api/project-time", params={"year": now.year, "month": now.month})
    # 200 if projects are enabled; 404/disabled is also acceptable
    assert r.status_code in (200, 404, 403), (
        f"Unexpected status {r.status_code}: {r.text[:200]}"
    )
    if r.status_code == 200:
        data = r.json()
        assert "entries" in data or "error" in data, f"Unexpected shape: {data}"


@test("floorplans — list floor plans (may be empty or disabled)")
def _():
    with _auth() as c:
        r = c.get("/api/floorplans")
    # 200 with a list, or 404 if the feature is disabled
    assert r.status_code in (200, 404, 403), (
        f"Unexpected status {r.status_code}: {r.text[:200]}"
    )
    if r.status_code == 200:
        data = r.json()
        assert isinstance(data, list), f"Expected list, got {data}"


@test("teams — list teams (requires team_manager/team_leader/global)")
def _():
    with _auth() as c:
        r = c.get("/api/teams")
    # 200 if the token has sufficient rights, 403 otherwise — never 5xx
    assert r.status_code in (200, 403), (
        f"Unexpected status {r.status_code}: {r.text[:200]}"
    )
    if r.status_code == 200:
        data = r.json()
        assert isinstance(data, list), f"Expected list, got {data}"


@test("users — list users (requires global role, may be 403)")
def _():
    with _auth() as c:
        r = c.get("/api/users")
    assert r.status_code in (200, 403), (
        f"Unexpected status {r.status_code}: {r.text[:200]}"
    )
    if r.status_code == 200:
        data = r.json()
        assert isinstance(data, list), f"Expected list, got {data}"
        if data:
            assert "email" in data[0], f"User missing 'email': {data[0]}"


@test("activity — missing params returns 4xx (not 5xx)")
def _():
    with _auth() as c:
        r = c.get("/api/activity")  # missing required params
    assert r.status_code < 500, f"Unexpected 5xx: {r.status_code} — {r.text[:200]}"


# ── Main ───────────────────────────────────────────────────────────────────────

def main() -> int:
    print("myPresence MCP Server — Smoke Tests")
    print(f"Target: {URL or '(not set)'}")
    print("-" * 60)

    if not preflight():
        return 1

    passed = sum(1 for _, ok, _ in _results if ok)
    failed = sum(1 for _, ok, _ in _results if not ok)
    total = len(_results)

    for name, ok, detail in _results:
        icon = "✓" if ok else "✗"
        print(f"  {icon}  {name}")
        if not ok and detail:
            print(f"       {detail}")

    print("-" * 60)
    print(f"Results: {passed}/{total} passed", end="")
    if failed:
        print(f"  ({failed} failed)")
    else:
        print()

    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
