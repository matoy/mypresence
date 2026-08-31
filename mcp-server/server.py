#!/usr/bin/env python3
"""
myPresence MCP Server
Exposes the myPresence REST API as MCP tools via stdio (FastMCP).

Required environment variables
-------------------------------
MYPRESENCE_URL   Base URL of the myPresence instance, e.g. http://localhost:8080
MYPRESENCE_TOKEN Personal Access Token (prefix mpa_…)
"""

import os
from typing import Optional

import httpx
from fastmcp import FastMCP

# ── Configuration ──────────────────────────────────────────────────────────────

MYPRESENCE_URL: str = os.environ.get("MYPRESENCE_URL", "").rstrip("/")
MYPRESENCE_TOKEN: str = os.environ.get("MYPRESENCE_TOKEN", "")

mcp = FastMCP(
    name="myPresence",
    instructions=(
        "Tools to interact with a myPresence instance (presence tracking, seat booking, "
        "project time, teams, users, news banners, holidays and statuses). "
        "All tools except `health_check` require MYPRESENCE_TOKEN to be set. "
        "A PAT inherits exactly the rights of the user who created it."
    ),
)

# ── HTTP helpers ───────────────────────────────────────────────────────────────


def _require_config() -> tuple[str, str]:
    if not MYPRESENCE_URL:
        raise RuntimeError("MYPRESENCE_URL environment variable is not set.")
    if not MYPRESENCE_TOKEN:
        raise RuntimeError("MYPRESENCE_TOKEN environment variable is not set.")
    return MYPRESENCE_URL, MYPRESENCE_TOKEN


def _auth_client() -> httpx.Client:
    url, token = _require_config()
    return httpx.Client(
        base_url=url,
        headers={"Authorization": f"Bearer {token}"},
        timeout=30,
    )


def _get(path: str, **params) -> dict | list:
    clean = {k: v for k, v in params.items() if v is not None}
    with _auth_client() as c:
        r = c.get(path, params=clean or None)
    r.raise_for_status()
    return r.json()


def _post(path: str, body: dict | None = None) -> dict:
    with _auth_client() as c:
        r = c.post(path, json=body)
    r.raise_for_status()
    return r.json()


def _put(path: str, body: dict | None = None) -> dict:
    with _auth_client() as c:
        r = c.put(path, json=body)
    r.raise_for_status()
    return r.json()


def _delete(path: str, body: dict | None = None) -> dict:
    with _auth_client() as c:
        r = c.delete(path, json=body)
    r.raise_for_status()
    return r.json()


# ═══════════════════════════════════════════════════════════════════════════════
# Health
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def health_check() -> dict:
    """
    Return application and database health.
    Public endpoint — does not require authentication.
    Returns status, uptime, database check and current server time.
    """
    if not MYPRESENCE_URL:
        raise RuntimeError("MYPRESENCE_URL environment variable is not set.")
    with httpx.Client(base_url=MYPRESENCE_URL, timeout=10) as c:
        r = c.get("/health")
    r.raise_for_status()
    return r.json()


# ═══════════════════════════════════════════════════════════════════════════════
# Personal Access Tokens
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def list_tokens() -> list:
    """
    List all Personal Access Tokens (PATs) owned by the authenticated user.
    Token hashes are never returned.
    """
    return _get("/api/tokens")


@mcp.tool()
def create_token(description: str, expires_in: int = 90) -> dict:
    """
    Create a new Personal Access Token.
    The raw token value is returned ONCE — store it immediately.

    Args:
        description: Human-readable label for the token (e.g. "CI script").
        expires_in:  Validity in days (0 = no expiry, max 3650). Default 90.

    Returns dict with keys: id, token, description, token_prefix, expires_at, created_at.
    """
    return _post("/api/tokens", {"description": description, "expires_in": expires_in})


@mcp.tool()
def delete_token(token_id: int) -> dict:
    """
    Revoke a Personal Access Token owned by the authenticated user.

    Args:
        token_id: Numeric ID of the token to revoke.
    """
    return _delete(f"/api/tokens/{token_id}")


# ═══════════════════════════════════════════════════════════════════════════════
# News Banners
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def list_active_news() -> list:
    """
    Return currently active news banners (start_date ≤ today ≤ end_date).
    Requires authentication only.
    """
    return _get("/api/news")


@mcp.tool()
def list_all_news() -> list:
    """
    Return ALL news banners including past and future ones.
    Requires the `activity_viewer` role.
    """
    return _get("/api/admin/news")


@mcp.tool()
def create_news_banner(
    title: str,
    content: str,
    start_date: str,
    end_date: str,
    bg_color: str = "#dc2626",
) -> dict:
    """
    Create a news banner displayed to all users.
    Requires the `activity_viewer` role.

    Args:
        title:      Short banner title.
        content:    Banner body (Markdown supported).
        start_date: First display date, YYYY-MM-DD.
        end_date:   Last display date, YYYY-MM-DD.
        bg_color:   Background hex color, e.g. "#d97706". Defaults to red (#dc2626).
    """
    return _post(
        "/api/admin/news",
        {
            "title": title,
            "content": content,
            "start_date": start_date,
            "end_date": end_date,
            "bg_color": bg_color,
        },
    )


@mcp.tool()
def update_news_banner(
    news_id: int,
    title: str,
    content: str,
    start_date: str,
    end_date: str,
    bg_color: str = "#dc2626",
) -> dict:
    """
    Update an existing news banner.
    Requires the `activity_viewer` role.

    Args:
        news_id:    ID of the banner to update.
        title:      New title.
        content:    New body (Markdown supported).
        start_date: New first display date, YYYY-MM-DD.
        end_date:   New last display date, YYYY-MM-DD.
        bg_color:   New background hex color.
    """
    return _put(
        f"/api/admin/news/{news_id}",
        {
            "title": title,
            "content": content,
            "start_date": start_date,
            "end_date": end_date,
            "bg_color": bg_color,
        },
    )


@mcp.tool()
def delete_news_banner(news_id: int) -> dict:
    """
    Delete a news banner permanently.
    Requires the `activity_viewer` role.

    Args:
        news_id: ID of the banner to delete.
    """
    return _delete(f"/api/admin/news/{news_id}")


# ═══════════════════════════════════════════════════════════════════════════════
# Presences
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def get_team_presences(team_id: int, year: int, month: int) -> dict:
    """
    Return presences for all members of a team for a given month.
    Requires `activity_viewer`, team leader, or `global` access.
    Team leaders can only query their own teams.

    Args:
        team_id: Team to query.
        year:    4-digit year (e.g. 2026).
        month:   Month number 1–12.

    Returns dict with `users` (presences keyed by date) and `statuses` list.
    """
    return _get("/api/presences", team_id=team_id, year=year, month=month)


@mcp.tool()
def set_presences(
    user_id: int,
    dates: list[str],
    status_id: int,
    half: str = "full",
) -> dict:
    """
    Set presence(s) for one or more dates for a user.
    Users can only modify their own presences unless they hold `global` or `team_manager`.

    Args:
        user_id:   ID of the user.
        dates:     List of dates in YYYY-MM-DD format.
        status_id: Presence status ID.
        half:      "full", "AM", or "PM". Default "full".
    """
    return _post(
        "/api/presences",
        {"user_id": user_id, "dates": dates, "status_id": status_id, "half": half},
    )


@mcp.tool()
def clear_presences(user_id: int, dates: list[str], half: str = "full") -> dict:
    """
    Clear presence(s) for one or more dates for a user.
    Users can only clear their own presences unless they hold `global` or `team_manager`.

    Args:
        user_id: ID of the user.
        dates:   List of dates in YYYY-MM-DD format.
        half:    "full", "AM", or "PM". Default "full".
    """
    return _post(
        "/api/presences/clear",
        {"user_id": user_id, "dates": dates, "half": half},
    )


# ═══════════════════════════════════════════════════════════════════════════════
# Floor Plans & Seats
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def list_floorplans() -> list:
    """
    List all floor plans.
    Returns id, name, image_path, and sort_order for each plan.
    Feature is unavailable if the server was started with DISABLE_FLOORPLANS=true.
    """
    return _get("/api/floorplans")


@mcp.tool()
def get_floorplan_seats(floorplan_id: int) -> list:
    """
    List all seats defined on a floor plan (without booking status).

    Args:
        floorplan_id: ID of the floor plan.
    """
    return _get(f"/api/floorplans/{floorplan_id}/seats")


@mcp.tool()
def get_seats_availability(
    floorplan_id: int,
    date: Optional[str] = None,
    half: str = "full",
) -> dict:
    """
    List seats with their booking status for the authenticated user on a given date.

    Args:
        floorplan_id: ID of the floor plan.
        date:         Date in YYYY-MM-DD format (default: today).
        half:         "full", "AM", or "PM". Default "full".

    Each seat has a `status` field: "free", "mine" (booked by caller), or "taken".
    """
    return _get("/api/seats", floorplan_id=floorplan_id, date=date, half=half)


@mcp.tool()
def reserve_seat(seat_id: int, date: str, half: str = "full") -> dict:
    """
    Reserve a single seat for a specific date and half-day.

    Args:
        seat_id: ID of the seat to book.
        date:    Date in YYYY-MM-DD format.
        half:    "full", "AM", or "PM". Default "full".

    Returns dict with `id` (reservation ID) and `status`.
    """
    return _post("/api/reservations", {"seat_id": seat_id, "date": date, "half": half})


@mcp.tool()
def reserve_seat_bulk(
    seat_id: int,
    dates: list[str],
    half: str = "full",
) -> dict:
    """
    Reserve the same seat across multiple dates.
    Dates where the caller has no on-site presence are silently skipped.

    Args:
        seat_id: ID of the seat to book.
        dates:   List of dates in YYYY-MM-DD format.
        half:    "full", "AM", or "PM". Default "full".

    Returns dict with `booked` (number of reservations created).
    """
    return _post(
        "/api/reservations/bulk",
        {"seat_id": seat_id, "dates": dates, "half": half},
    )


@mcp.tool()
def cancel_reservations_bulk(dates: list[str]) -> dict:
    """
    Cancel all seat reservations owned by the authenticated user across multiple dates.

    Args:
        dates: List of dates in YYYY-MM-DD format.
    """
    return _delete("/api/reservations/bulk", {"dates": dates})


@mcp.tool()
def cancel_reservation(reservation_id: int) -> dict:
    """
    Cancel a specific seat reservation owned by the authenticated user.

    Args:
        reservation_id: ID of the reservation to cancel.
    """
    return _delete(f"/api/reservations/{reservation_id}")


# ═══════════════════════════════════════════════════════════════════════════════
# Users
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def list_users() -> list:
    """
    List all users in the system.
    Requires the `global` role.
    Returns id, email, name, roles, is_local, disabled, created_at for each user.
    """
    return _get("/api/users")


@mcp.tool()
def update_user_roles(user_id: int, roles: list[str]) -> dict:
    """
    Replace the role set for a user.
    Requires the `global` role.

    Valid roles: basic, team_manager, status_manager, activity_viewer,
    floorplan_manager, projects_manager, projects_viewer, global.

    Args:
        user_id: ID of the user to update.
        roles:   Complete list of roles to assign (replaces current roles).
    """
    return _put(f"/api/users/{user_id}/roles", {"roles": roles})


# ═══════════════════════════════════════════════════════════════════════════════
# Teams
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def list_teams() -> list:
    """
    List all teams.
    Requires `team_manager`, team leader, or `global` access.
    """
    return _get("/api/teams")


@mcp.tool()
def create_team(name: str) -> dict:
    """
    Create a new team.
    Requires `team_manager` or `global` role.

    Args:
        name: Name of the new team (must be unique).
    """
    return _post("/admin/teams", {"name": name})


@mcp.tool()
def update_team(team_id: int, name: str) -> dict:
    """
    Rename an existing team.
    Requires `team_manager` or `global` role.

    Args:
        team_id: ID of the team to rename.
        name:    New name (must be unique).
    """
    return _put(f"/admin/teams/{team_id}", {"name": name})


@mcp.tool()
def delete_team(team_id: int) -> dict:
    """
    Delete a team and all its memberships permanently.
    Requires `team_manager` or `global` role.

    Args:
        team_id: ID of the team to delete.
    """
    return _delete(f"/admin/teams/{team_id}")


@mcp.tool()
def add_team_member(team_id: int, user_id: int) -> dict:
    """
    Add a user to a team.
    Requires `team_manager`, `global`, or designated team leader for this team.

    Args:
        team_id: ID of the team.
        user_id: ID of the user to add.
    """
    return _post(f"/admin/teams/{team_id}/members", {"user_id": user_id})


@mcp.tool()
def remove_team_member(team_id: int, user_id: int) -> dict:
    """
    Remove a user from a team.
    Requires `team_manager`, `global`, or designated team leader for this team.

    Args:
        team_id: ID of the team.
        user_id: ID of the user to remove.
    """
    return _delete(f"/admin/teams/{team_id}/members/{user_id}")


@mcp.tool()
def get_team_leaders(team_id: int) -> dict:
    """
    Get user IDs of designated leaders for a team.

    Args:
        team_id: ID of the team.
    """
    return _get(f"/api/admin/teams/{team_id}/leaders")


@mcp.tool()
def set_team_leaders(team_id: int, user_ids: list[int]) -> dict:
    """
    Set designated leaders for a team.
    Requires `team_manager` or `global` role.

    Args:
        team_id:  ID of the team.
        user_ids: List of user IDs to designate as team leaders.
    """
    return _put(f"/api/admin/teams/{team_id}/leaders", {"user_ids": user_ids})


# ═══════════════════════════════════════════════════════════════════════════════
# Holidays
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def create_holiday(date: str, name: str, allow_imputed: bool = False) -> dict:
    """
    Create a public holiday.
    Requires the `global` role.

    Args:
        date:          Date of the holiday, YYYY-MM-DD.
        name:          Display name (e.g. "Bastille Day").
        allow_imputed: When True, employees can log chargeable time on this day.
    """
    return _post("/admin/holidays", {"date": date, "name": name, "allow_imputed": allow_imputed})


@mcp.tool()
def update_holiday(holiday_id: int, date: str, name: str, allow_imputed: bool = False) -> dict:
    """
    Update an existing public holiday.
    Requires the `global` role.

    Args:
        holiday_id:    ID of the holiday to update.
        date:          New date, YYYY-MM-DD.
        name:          New name.
        allow_imputed: Whether chargeable time is allowed on this day.
    """
    return _put(
        f"/admin/holidays/{holiday_id}",
        {"date": date, "name": name, "allow_imputed": allow_imputed},
    )


@mcp.tool()
def delete_holiday(holiday_id: int) -> dict:
    """
    Delete a public holiday.
    Requires the `global` role.

    Args:
        holiday_id: ID of the holiday to delete.
    """
    return _delete(f"/admin/holidays/{holiday_id}")


# ═══════════════════════════════════════════════════════════════════════════════
# Statuses
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def create_status(
    name: str,
    color: str,
    billable: bool = False,
    on_site: bool = False,
    sort_order: int = 0,
) -> dict:
    """
    Create a new presence status type (e.g. "On site", "Remote", "Leave").
    Requires `global` or `status_manager` role.

    Args:
        name:       Display name for the status.
        color:      Hex color code, e.g. "#22c55e".
        billable:   Whether this status counts as billable time.
        on_site:    Whether this status counts as on-site presence.
        sort_order: Display order (lower = first).
    """
    return _post(
        "/admin/statuses",
        {
            "name": name,
            "color": color,
            "billable": billable,
            "on_site": on_site,
            "sort_order": sort_order,
        },
    )


@mcp.tool()
def update_status(
    status_id: int,
    name: str,
    color: str,
    billable: bool = False,
    on_site: bool = False,
    sort_order: int = 0,
) -> dict:
    """
    Update an existing presence status type.
    Requires `global` or `status_manager` role.

    Args:
        status_id:  ID of the status to update.
        name:       New display name.
        color:      New hex color code.
        billable:   Whether this status counts as billable time.
        on_site:    Whether this status counts as on-site presence.
        sort_order: New display order.
    """
    return _put(
        f"/admin/statuses/{status_id}",
        {
            "name": name,
            "color": color,
            "billable": billable,
            "on_site": on_site,
            "sort_order": sort_order,
        },
    )


@mcp.tool()
def delete_status(status_id: int) -> dict:
    """
    Delete a presence status type.
    Existing presences referencing this status are NOT automatically removed.
    Requires `global` or `status_manager` role.

    Args:
        status_id: ID of the status to delete.
    """
    return _delete(f"/admin/statuses/{status_id}")


# ═══════════════════════════════════════════════════════════════════════════════
# Projects
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def get_my_projects(year: Optional[int] = None, month: Optional[int] = None) -> dict:
    """
    Return the authenticated user's project declaration context for a month.
    Feature is unavailable if the server was started with DISABLE_PROJECTS=true.

    Args:
        year:  4-digit year (default: current year).
        month: Month number 1–12 (default: current month).

    Returns projects list, existing entries, entry_map, billable_days and total_declared.
    """
    return _get("/api/projects", year=year, month=month)


@mcp.tool()
def get_my_project_time(year: Optional[int] = None, month: Optional[int] = None) -> dict:
    """
    Return the authenticated user's project time entries and month totals.

    Args:
        year:  4-digit year (default: current year).
        month: Month number 1–12 (default: current month).
    """
    return _get("/api/project-time", year=year, month=month)


@mcp.tool()
def set_project_time(
    project_id: int,
    year: int,
    month: int,
    days: float,
) -> dict:
    """
    Create or update a project time entry for the authenticated user.
    If days <= 0 the entry is removed.
    Total declared days cannot exceed the user's billable days for the month.

    Args:
        project_id: ID of the project.
        year:       4-digit year.
        month:      Month number 1–12.
        days:       Number of days to declare (decimals allowed, e.g. 3.5).
    """
    return _post(
        "/api/project-time",
        {"project_id": project_id, "year": year, "month": month, "days": days},
    )


@mcp.tool()
def get_projects_report(
    q: Optional[str] = None,
    active: Optional[str] = None,
    team: Optional[int] = None,
) -> dict:
    """
    Return the project report (all projects, all users, monthly breakdown).
    Requires `projects_manager`, `projects_viewer`, team leader, or `global` access.
    Team leaders only see their own teams.

    Args:
        q:      Optional text filter on project code or name.
        active: Optional filter: "1" (active), "0" (inactive), or omit for all.
        team:   Optional team ID filter.
    """
    return _get("/api/projects-report", q=q, active=active, team=team)


@mcp.tool()
def list_admin_projects(
    q: Optional[str] = None,
    active: Optional[str] = None,
    team: Optional[int] = None,
) -> dict:
    """
    List projects and teams for admin management.
    Requires `projects_manager` role.

    Args:
        q:      Optional text filter on project code or name.
        active: Optional filter: "1" (active), "0" (inactive), or omit for all.
        team:   Optional team ID filter.
    """
    return _get("/api/admin/projects", q=q, active=active, team=team)


@mcp.tool()
def create_project(
    name: str,
    code: str,
    team_id: int,
    active: bool = True,
    start_date: Optional[str] = None,
    end_date: Optional[str] = None,
) -> dict:
    """
    Create a new project.
    Requires `projects_manager` role.

    Args:
        name:       Project display name.
        code:       Short project code (e.g. "ERP-01").
        team_id:    ID of the owning team.
        active:     Whether the project is active. Default True.
        start_date: Optional start date, YYYY-MM-DD.
        end_date:   Optional end date, YYYY-MM-DD.
    """
    body: dict = {"name": name, "code": code, "team_id": team_id, "active": active}
    if start_date:
        body["start_date"] = start_date
    if end_date:
        body["end_date"] = end_date
    return _post("/api/admin/projects", body)


@mcp.tool()
def update_project(
    project_id: int,
    name: str,
    code: str,
    team_id: int,
    active: bool = True,
    start_date: Optional[str] = None,
    end_date: Optional[str] = None,
) -> dict:
    """
    Update an existing project.
    Requires `projects_manager` role.

    Args:
        project_id: ID of the project to update.
        name:       New display name.
        code:       New short code.
        team_id:    New owning team ID.
        active:     Whether the project is active.
        start_date: New start date, YYYY-MM-DD (optional).
        end_date:   New end date, YYYY-MM-DD (optional).
    """
    body: dict = {"name": name, "code": code, "team_id": team_id, "active": active}
    if start_date:
        body["start_date"] = start_date
    if end_date:
        body["end_date"] = end_date
    return _put(f"/api/admin/projects/{project_id}", body)


# ═══════════════════════════════════════════════════════════════════════════════
# Activity Report
# ═══════════════════════════════════════════════════════════════════════════════


@mcp.tool()
def get_activity_report(team_id: int, year: int, month: int) -> dict:
    """
    Return presence statistics for all members of a team over a month.
    Requires `activity_viewer`, team leader, or `global` access.

    Args:
        team_id: Team to report on.
        year:    4-digit year (e.g. 2026).
        month:   Month number 1–12.

    Returns per-user status counts, billable_days, on_site_days and working_days.
    """
    return _get("/api/activity", team_id=team_id, year=year, month=month)


# ═══════════════════════════════════════════════════════════════════════════════
# Entry point
# ═══════════════════════════════════════════════════════════════════════════════

if __name__ == "__main__":
    mcp.run()
