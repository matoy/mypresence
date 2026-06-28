# myPresence MCP Server

[MCP](https://modelcontextprotocol.io/) (Model Context Protocol) server for the
**myPresence** application, implemented in Python with
[FastMCP](https://github.com/jlowin/fastmcp).

It exposes the full myPresence REST API as MCP tools consumable over stdio by
any compatible client (Claude Desktop, GitHub Copilot, Cursor, etc.).

---

## Requirements

| Tool | Minimum version |
|------|----------------|
| Python | 3.11 |
| pip | recent |
| myPresence | running instance |

---

## Installation

```bash
cd mcp-server
python -m venv .venv

# Windows
.venv\Scripts\activate

# macOS / Linux
source .venv/bin/activate

pip install -r requirements.txt
```

---

## Configuration

The server reads two environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `MYPRESENCE_URL` | ✅ | Base URL of the instance, e.g. `http://localhost:8080` |
| `MYPRESENCE_TOKEN` | ✅ | Personal Access Token (PAT) of the user |

### Obtaining a PAT

1. Log in to the myPresence web interface.
2. Go to **Settings → Tokens** (or use `GET /api/tokens`).
3. Create a token with a description and an expiry duration.
4. Copy the returned value (it starts with `mpa_`) — **it is shown only once**.

A PAT inherits exactly the rights of the user who created it. No session cookie
or login flow is required — the token alone is sufficient.

### Example (shell)

```bash
export MYPRESENCE_URL="http://localhost:8080"
export MYPRESENCE_TOKEN="mpa_6f3a8b1c2d3e..."
```

---

## Starting the server

```bash
python server.py
```

The server listens on **stdio** (standard input/output). It does not open any
network port.

---

## MCP client integration

### Claude Desktop

Add an entry to `claude_desktop_config.json`
(`~/Library/Application Support/Claude/` or `%APPDATA%\Claude\`):

```json
{
  "mcpServers": {
    "myPresence": {
      "command": "python",
      "args": ["C:/path/to/mcp-server/server.py"],
      "env": {
        "MYPRESENCE_URL": "http://localhost:8080",
        "MYPRESENCE_TOKEN": "mpa_..."
      }
    }
  }
}
```

### VS Code (GitHub Copilot)

In `.vscode/mcp.json` at the workspace root:

```json
{
  "servers": {
    "myPresence": {
      "type": "stdio",
      "command": "python",
      "args": ["${workspaceFolder}/mcp-server/server.py"],
      "env": {
        "MYPRESENCE_URL": "http://localhost:8080",
        "MYPRESENCE_TOKEN": "mpa_..."
      }
    }
  }
}
```

---

## Available tools

### 🔍 Health

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `health_check` | `GET /health` | None (public) |

### 🔑 Personal Access Tokens

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `list_tokens` | `GET /api/tokens` | Authenticated |
| `create_token` | `POST /api/tokens` | Authenticated |
| `delete_token` | `DELETE /api/tokens/{id}` | Authenticated |

### 📢 News Banners

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `list_active_news` | `GET /api/news` | Authenticated |
| `list_all_news` | `GET /api/admin/news` | `activity_viewer` |
| `create_news_banner` | `POST /api/admin/news` | `activity_viewer` |
| `update_news_banner` | `PUT /api/admin/news/{id}` | `activity_viewer` |
| `delete_news_banner` | `DELETE /api/admin/news/{id}` | `activity_viewer` |

### 📅 Presences

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `get_team_presences` | `GET /api/presences` | `activity_viewer` or `team_leader` |
| `set_presences` | `POST /api/presences` | Authenticated (own account) / `global` / `team_manager` |
| `clear_presences` | `POST /api/presences/clear` | Authenticated (own account) / `global` / `team_manager` |

### 🗺️ Floor Plans & Seats

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `list_floorplans` | `GET /api/floorplans` | Authenticated |
| `get_floorplan_seats` | `GET /api/floorplans/{id}/seats` | Authenticated |
| `get_seats_availability` | `GET /api/seats` | Authenticated |
| `reserve_seat` | `POST /api/reservations` | Authenticated |
| `reserve_seat_bulk` | `POST /api/reservations/bulk` | Authenticated |
| `cancel_reservations_bulk` | `DELETE /api/reservations/bulk` | Authenticated |
| `cancel_reservation` | `DELETE /api/reservations/{id}` | Authenticated |

### 👥 Users

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `list_users` | `GET /api/users` | `global` |
| `update_user_roles` | `PUT /api/users/{id}/roles` | `global` |

### 🏢 Teams

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `list_teams` | `GET /api/teams` | `team_manager`, `team_leader`, or `global` |
| `create_team` | `POST /admin/teams` | `team_manager` or `global` |
| `update_team` | `PUT /admin/teams/{id}` | `team_manager` or `global` |
| `delete_team` | `DELETE /admin/teams/{id}` | `team_manager` or `global` |
| `add_team_member` | `POST /admin/teams/{id}/members` | `team_manager`, `global`, or `team_leader` |
| `remove_team_member` | `DELETE /admin/teams/{id}/members/{userId}` | `team_manager`, `global`, or `team_leader` |

### 🗓️ Public Holidays

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `create_holiday` | `POST /admin/holidays` | `global` |
| `update_holiday` | `PUT /admin/holidays/{id}` | `global` |
| `delete_holiday` | `DELETE /admin/holidays/{id}` | `global` |

### 🏷️ Presence Statuses

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `create_status` | `POST /admin/statuses` | `global` or `status_manager` |
| `update_status` | `PUT /admin/statuses/{id}` | `global` or `status_manager` |
| `delete_status` | `DELETE /admin/statuses/{id}` | `global` or `status_manager` |

### 📊 Projects

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `get_my_projects` | `GET /api/projects` | Authenticated |
| `get_my_project_time` | `GET /api/project-time` | Authenticated |
| `set_project_time` | `POST /api/project-time` | Authenticated |
| `get_projects_report` | `GET /api/projects-report` | `projects_admin`, `projects_viewer`, or `team_leader` |
| `list_admin_projects` | `GET /api/admin/projects` | `projects_admin` |
| `create_project` | `POST /api/admin/projects` | `projects_admin` |
| `update_project` | `PUT /api/admin/projects/{id}` | `projects_admin` |

### 📈 Activity Report

| Tool | Endpoint | Required role |
|------|----------|---------------|
| `get_activity_report` | `GET /api/activity` | `activity_viewer` or `team_leader` |

---

## Smoke tests

The `smoke_tests.py` script verifies that the myPresence instance is reachable
and that the configured PAT works correctly.

```bash
python smoke_tests.py
```

Example output:

```
myPresence MCP Server — Smoke Tests
Target: http://localhost:8080
------------------------------------------------------------
  ✓  health_check — public endpoint reachable
  ✓  health_check — response contains 'uptime' field
  ✓  auth — unauthenticated request to /api/tokens is rejected
  ✓  tokens — list PATs (authenticated)
  ✓  news — list active banners (authenticated)
  ✓  news — each banner has required fields
  ✓  presences — invalid team returns 4xx (not a server crash)
  ✓  projects — get current month context (basic user)
  ✓  floorplans — list floor plans (may be empty or disabled)
  ✓  teams — list teams (requires team_manager/team_leader/global)
  ✓  users — list users (requires global role, may be 403)
  ✓  activity — missing params returns 4xx (not 5xx)
------------------------------------------------------------
Results: 12/12 passed
```

Tests are intentionally tolerant of insufficient permissions (403) because the
PAT used may not have all roles. Only 5xx errors and unexpected response shapes
are treated as failures.

**Exit code:** `0` if all tests pass, `1` otherwise.

---

## Folder structure

```
mcp-server/
├── server.py          # FastMCP server (entry point)
├── smoke_tests.py     # Quick validation script
├── requirements.txt   # Python dependencies
└── README.md          # This documentation
```

---

## Security

- The PAT is **never** logged or printed by the server.
- The server makes no network calls outside of `MYPRESENCE_URL`.
- The stdio transport ensures no port is opened on the machine.
- Tokens have a configurable expiry; rotate them regularly.
