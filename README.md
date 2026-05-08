# Presence App

<!-- Replace matoy/myPresence and matoy with actual values -->
[![CI](https://github.com/matoy/myPresence/actions/workflows/ci.yml/badge.svg)](https://github.com/matoy/myPresence/actions/workflows/ci.yml)
[![Release](https://github.com/matoy/myPresence/actions/workflows/release.yml/badge.svg)](https://github.com/matoy/myPresence/actions/workflows/release.yml)
[![Docker Pulls](https://img.shields.io/docker/pulls/matoy/mypresence)](https://hub.docker.com/r/matoy/mypresence)
[![Docker Image Size](https://img.shields.io/docker/image-size/matoy/mypresence/latest)](https://hub.docker.com/r/matoy/mypresence)
[![Go Version](https://img.shields.io/badge/go-1.23-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

A web application for managing employee presence and absences, built with Go and SQLite. Reactive UI using Alpine.js + Tailwind CSS, deployed via Docker.

---

## Features

- **Personal monthly calendar**: each user enters their own presence/absences using click or drag-to-select; right-click a day to declare a half-day (AM or PM) with a different status per half
- **Floor plans & desk reservations**: upload a floor map image, place clickable seats on it, and let users book desks directly from the calendar or the floor plan page
- **Team management**: assign users to teams
- **Activity Report**: summary of billable days per team
- **Customizable statuses**: color, label, billable flag (€)
- **Public holidays**: displayed in grey on the calendar, with an optional imputation flag
- **Role management**: granular per-user permissions
- **SAML 2.0 SSO**: IDP integration with automatic user provisioning
- **REST API with Personal Access Tokens**: every feature is accessible via authenticated HTTP requests; users generate tokens with a chosen description and expiry; tokens carry no more permissions than the issuing user
- **Multilingual UI**: full interface available in English 🇺🇸, French 🇫🇷, German 🇩🇪, Spanish 🇪🇸, and Italian 🇮🇹; language preference stored in a cookie

---

## Quick Start

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose

### Start

```bash
docker compose up -d
```

The application is available at **http://localhost:8080**

Default credentials: `admin` / `admin`

### Stop

```bash
docker compose down
```

Data is persisted in the `presence-data` Docker volume (SQLite database at `/data/app.db`).

---

## Docker CLI Examples

These examples use `docker run` directly — no Compose file needed. Replace `matoy/mypresence:latest` with the image you built locally if needed.

### Minimal (SQLite, default settings)

```bash
docker run -d \
  --name mypresence \
  -p 8080:8080 \
  -v mypresence-data:/data \
  matoy/mypresence:latest
```

Open **http://localhost:8080** — default credentials: `admin` / `admin`.

### Custom port and admin credentials

```bash
docker run -d \
  --name mypresence \
  -p 9090:8080 \
  -v mypresence-data:/data \
  -e ADMIN_USER=myuser \
  -e ADMIN_PASSWORD=s3cr3tP@ss \
  -e SECRET_KEY=change-me-32-chars-minimum-here!! \
  matoy/mypresence:latest
```

### Custom branding

```bash
docker run -d \
  --name mypresence \
  -p 8080:8080 \
  -v mypresence-data:/data \
  -e APP_NAME="My Company Presence" \
  -e PRIMARY_COLOR="#6366f1" \
  -e SECONDARY_COLOR="#4338ca" \
  -e ACCENT_COLOR="#f59e0b" \
  matoy/mypresence:latest
```

### With PostgreSQL backend

Start a PostgreSQL container first, then run the app pointing to it:

```bash
# 1 — PostgreSQL
docker run -d \
  --name presence-postgres \
  --network presence-net \
  -e POSTGRES_DB=mypresence \
  -e POSTGRES_USER=presence \
  -e POSTGRES_PASSWORD=dbpassword \
  -v presence-pg-data:/var/lib/postgresql/data \
  postgres:17-alpine

# 2 — App
docker run -d \
  --name mypresence \
  --network presence-net \
  -p 8080:8080 \
  -v mypresence-data:/data \
  -e DB_DRIVER=postgres \
  -e DB_HOST=presence-postgres \
  -e DB_NAME=mypresence \
  -e DB_USER=presence \
  -e DB_PASSWORD=dbpassword \
  matoy/mypresence:latest
```

> Create the shared network first: `docker network create presence-net`

### With Prometheus metrics enabled

```bash
docker run -d \
  --name mypresence \
  -p 8080:8080 \
  -v mypresence-data:/data \
  -e METRICS_TOKEN=my-strong-random-token \
  matoy/mypresence:latest
```

Scrape metrics with: `curl -H "Authorization: Bearer my-strong-random-token" http://localhost:8080/metrics`

### With SAML 2.0 SSO (e.g. Microsoft Entra ID)

```bash
docker run -d \
  --name mypresence \
  -p 8080:8080 \
  -v mypresence-data:/data \
  -e SAML_IDP_METADATA_URL=https://login.microsoftonline.com/<tenant-id>/federationmetadata/2007-06/federationmetadata.xml \
  -e SAML_ENTITY_ID=https://presence.example.com/saml/metadata \
  -e SAML_ROOT_URL=https://presence.example.com \
  matoy/mypresence:latest
```

The SP certificate and key are auto-generated on first startup and stored in the data volume. To use your own:

```bash
docker run -d \
  --name mypresence \
  -p 8080:8080 \
  -v mypresence-data:/data \
  -e SAML_IDP_METADATA_URL=https://login.microsoftonline.com/<tenant-id>/federationmetadata/2007-06/federationmetadata.xml \
  -e SAML_ENTITY_ID=https://presence.example.com/saml/metadata \
  -e SAML_ROOT_URL=https://presence.example.com \
  -e SAML_SP_CERT_FILE=/data/saml/sp-cert.pem \
  -e SAML_SP_KEY_FILE=/data/saml/sp-key.pem \
  matoy/mypresence:latest
```

**Automatic role assignment from Entra groups** — add one `-e` per group to map:

```bash
docker run -d \
  --name mypresence \
  -p 8080:8080 \
  -v mypresence-data:/data \
  -e SAML_IDP_METADATA_URL=https://login.microsoftonline.com/<tenant-id>/federationmetadata/2007-06/federationmetadata.xml \
  -e SAML_ENTITY_ID=https://presence.example.com/saml/metadata \
  -e SAML_ROOT_URL=https://presence.example.com \
  -e SAML_GROUP_GLOBAL=<object-id-global-admins> \
  -e SAML_GROUP_TEAM_MANAGER=<object-id-team-managers> \
  -e SAML_GROUP_TEAM_LEADER=<object-id-team-leaders> \
  -e SAML_GROUP_STATUS_MANAGER=<object-id-status-managers> \
  -e SAML_GROUP_ACTIVITY_VIEWER=<object-id-activity-viewers> \
  -e SAML_GROUP_FLOORPLAN_MANAGER=<object-id-floorplan-managers> \
  -e SAML_GROUP_PROJECTS_MANAGER=<object-id-project-managers> \
  -e SAML_GROUP_PROJECTS_VIEWER=<object-id-project-viewers> \
  matoy/mypresence:latest
```

> In Entra ID, configure the app to include a **Group** claim with format **Object ID**. The default claim URI (`http://schemas.microsoft.com/ws/2008/06/identity/claims/groups`) is used automatically; override with `SAML_GROUPS_CLAIM` if your IdP uses a different URI.

### Stop and remove

```bash
docker stop mypresence && docker rm mypresence
```

> The `mypresence-data` volume is kept — data survives container removal. To also delete data: `docker volume rm mypresence-data`

---

## Configuration

All options are set via environment variables in `docker-compose.yml`.

### General

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listening port |
| `DATA_DIR` | `/data` | Directory for the SQLite database and uploaded files |
| `SECRET_KEY` | `change-me-...` | Session cookie signing key (32 characters recommended) |

### Branding

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_NAME` | `Presence` | Application name (header, browser tab) |
| `PRIMARY_COLOR` | `#3b82f6` | Primary UI color (buttons, links) |
| `SECONDARY_COLOR` | `#1e40af` | Secondary color (header gradient) |
| `ACCENT_COLOR` | `#f59e0b` | Accent color (badges) |
| `LOGO_PATH` | *(empty)* | Path to a logo file (`logo.png`, `logo.svg`, or `logo.jpg` inside `DATA_DIR`) |

### Local Authentication (Admin)

| Variable | Default | Description |
|----------|---------|-------------|
| `ADMIN_USER` | `admin` | Local admin username |
| `ADMIN_PASSWORD` | `admin` | Local admin password |

> The local admin account always has the `global` role. Change these values in production.

### SAML 2.0 SSO

| Variable | Required | Description |
|----------|----------|-------------|
| `SAML_IDP_METADATA_URL` | Yes | IdP metadata URL |
| `SAML_ENTITY_ID` | Yes | Service Provider Entity ID |
| `SAML_ROOT_URL` | Yes | Public application URL (e.g. `https://presence.example.com`) |
| `SAML_SP_CERT_FILE` | No | Path to the SP certificate (auto-generated if not provided) |
| `SAML_SP_KEY_FILE` | No | Path to the SP private key |

SSO is enabled when both `SAML_IDP_METADATA_URL` and `SAML_ENTITY_ID` are set. New SAML users are automatically provisioned with the `basic` role.

**SAML SP endpoints**:
- Metadata: `GET /saml/metadata`
- Initiation: `GET /saml/login`
- ACS (Assertion Consumer Service): `POST /saml/acs`

Roles can be automatically assigned at login based on IDP group membership (e.g. Microsoft Entra ID Security Groups). Configure Entra to include a group claim (`http://schemas.microsoft.com/ws/2008/06/identity/claims/groups`, format: Object ID) then map each group to an application role.

---

## Roles & Permissions

Roles are cumulative (stored as a comma-separated string per user). The `global` role grants all permissions.

| Role | Access |
|------|--------|
| `basic` | Personal calendar (own presences only) |
| `team_leader` | View calendar and activity report for own team; view project reports for own teams |
| `team_manager` | Team management + edit any user's presences |
| `status_manager` | Create / edit / delete presence statuses |
| `activity_viewer` | View Activity Report (billable days) by team |
| `floorplan_manager` | Create / edit floor plans and seats |
| `projects_admin` | Create / edit / delete projects; view full project reports |
| `projects_viewer` | View project time entries and reports |
| `global` | Full access — includes user/role management, public holidays, and project administration |

Roles are assigned from **👤 Users & Roles** (`/admin/users`), accessible to the `global` role only.

---

## Pages

| URL | Required role | Description |
|-----|---------------|-------------|
| `/` | Any logged-in user | Personal monthly calendar |
| `/floorplan` | Any logged-in user | Floor plan viewer and desk reservation |
| `/projects` | Any logged-in user | Declare and track time on projects |
| `/settings/tokens` | Any logged-in user | Manage Personal Access Tokens (API keys) |
| `/admin/teams` | `team_manager` or `team_leader` | Manage teams and members |
| `/admin/statuses` | `status_manager` | Manage presence statuses |
| `/admin/activity` | `activity_viewer` or `team_leader` | Activity report by team and period |
| `/admin/floorplans` | `floorplan_manager` | Manage floor plans and seats |
| `/admin/projects` | `projects_admin` | Create and manage projects |
| `/admin/projects-report` | `projects_admin`, `projects_viewer`, or `team_leader` | Project time tracking and reporting |
| `/admin/holidays` | `global` | Manage public holidays |
| `/admin/users` | `global` | Manage users, roles and passwords |
| `/admin/users/{id}/logs` | `global` | Presence audit log for a user |
| `/health` | *(none)* | Health check — public, no authentication |
| `/api/docs` | *(none)* | Full REST API documentation (plain text, public) |

### Features

| Variable | Default | Description |
|----------|---------|-------------|
| `DISABLE_FLOORPLANS` | `false` | Set to `true` to disable the floor plan module (page, seat reservation, admin) |
| `DISABLE_PROJECTS` | `false` | Set to `true` to disable the project management module (user time entry, admin, reporting) |
| `DISABLE_API` | `false` | Set to `true` to disable the REST API entirely (PAT management, Bearer auth, `/api/docs`) |

---

## REST API

myPresence exposes a full REST API that mirrors every user action available in the browser interface. All endpoints require authentication using a **Personal Access Token (PAT)**.

### Personal Access Tokens

Each user can generate tokens at **🔑 `/settings/tokens`** (accessible from the top-right menu).

- Choose a **description** (e.g. *"Reporting script"*) and an **expiry** (7, 30, 90, 365 days, or no expiry)
- The raw token (prefixed `mpa_`) is shown **once** — copy it before closing the dialog
- Tokens inherit **exactly** the permissions of the issuing user — no elevation, no restriction
- Revocation is immediate; all integrations using the token stop working instantly

### Authentication header

```
Authorization: Bearer mpa_<your-token>
```

### API documentation

The full endpoint reference (request/response format for every route) is available at:

- **In-app**: [`/api/docs`](http://localhost:8080/api/docs) — served as plain text, no authentication required
- **In the repository**: [`API.md`](API.md)

### Example

```bash
# Get your presences for April 2026
curl -H "Authorization: Bearer mpa_yourtoken" \
     "http://localhost:8080/api/presences?team_id=1&year=2026&month=4"

# Set a presence
curl -X POST -H "Authorization: Bearer mpa_yourtoken" \
     -H "Content-Type: application/json" \
     -d '{"user_id":5,"dates":["2026-04-14"],"status_id":3,"half":"full"}' \
     http://localhost:8080/api/presences
```

---

## Floor Plans & Desk Reservations

The floor plan feature allows administrators to set up interactive office maps so users can book a specific desk directly from the calendar.

### Admin setup (`floorplan_manager` role required)

1. Go to **🗺️ Plans admin** in the navigation.
2. Create one or more floor plans (e.g. *Floor 2*, *Open Space A*).
3. Upload a background image (PNG, JPG, GIF, or WEBP) for each plan.
4. Click anywhere on the image to place a seat and give it a short label (e.g. `A1`, `B12`).
5. Drag-hover over an existing seat pin and click × to remove it.

### User workflow

- Navigate to **🗺️ Plans** to see the floor map for any date.
  - Use the **← / →** arrows to navigate day by day.
  - Switch between **Morning / Full day / Afternoon** to scope the reservation to a half-day.
  - **Green** = free — click to book. **Blue** = your own reservation — click to cancel. **Red** = taken.
- From the **calendar**, seat icons (🪑) appear on days where you have a reservation.
  - **Right-click** one or more selected days → *Reserve a desk* to bulk-book a desk.
  - **Right-click** → *Cancel desk reservation* to remove all your reservations on the selected days.

### Rules

- A seat can only be booked when the user has an **on-site** presence declared for that date.
- Days without an on-site presence are silently skipped during bulk booking.
- Each seat allows one reservation per `(seat, date, half)` combination.

---

## Project Management & Time Tracking

The project management feature allows organizations to track employee time allocation across projects, with automatic billable days enforcement and role-based reporting.

### Admin setup (`projects_admin` role required)

1. Go to **📂 Projects (admin)** in the navigation.
2. Create projects with:
   - **Name**: Project title
   - **Code**: Short identifier (e.g. `PROJ-001`)
   - **Team**: Assign the project to a team (optional)
   - **Active**: Enable/disable the project
   - **Dates**: Start and end dates for the project timeline
3. Projects appear on the user time entry page only after creation.

### User workflow

- Navigate to **📂 Projects** to declare time:
  - View all active projects for the current month.
  - Enter decimal days (e.g. `0.5` for half-day, `1` for full day) for each project.
  - The app enforces a **billable days cap**: the total time declared cannot exceed billable days for the month (based on recorded presences).
  - A progress bar shows declared vs. available billable days.
  - **Save** each project entry individually.
- Navigate to **📂 Projects (report)** (`projects_admin`, `projects_viewer`, or `team_leader`) to review:
  - All project time entries per user, aggregated by month.
  - **Filters**: text search (project name/code), active/inactive, team selection.
  - **Team leaders** see only projects in their assigned teams.

### Rules

- Each user can declare up to the total **billable days** in a given month (calculated from presences with billable status).
- Only **active** projects are visible on the user time entry page.
- Projects outside the declared time entry period are hidden.
- Time entries are stored as decimal values (supporting half-days, quarter-days, etc.).
- Team leaders can view reports but only for their teams.

---

## Calendar

- Month navigation (← →) and direct year/month selection
- Days selected by **click** or **drag** (range selection); blocked on weekends and non-imputable holidays
- After selection, a colour-coded **status picker** appears to apply or clear a presence
- **Right-click** on any day opens a context menu to:
  - Declare a **half-day** (AM or PM) with an independent status per half
  - Insert the day’s status into your **calendar client** as an `.ics` event (Outlook, Google Calendar, etc.)
  - **Reserve a desk** on the selected period (see [Floor Plans](#floor-plans--desk-reservations))
  - **Cancel desk reservation(s)** on the selected period
  - Clear all presences for the day
- Days with a desk reservation display a 🪑 icon
- Hovering over a cell shows a tooltip with the status name or holiday name
- **Weekends** are greyed out and cannot be selected
- **Public holidays** are greyed out and non-selectable by default
  - *Allow imputed* option: the holiday remains visually grey but can receive a status

---

## Default Presence Statuses

Automatically seeded on first startup:

| Name | Color | Billable | On-site |
|------|-------|----------|---------|
| On site | 🟢 green | Yes | Yes |
| Remote work | 🟣 purple | Yes | No |
| Business trip | 🔵 blue | Yes | Yes |
| Leave | 🟠 orange | No | No |
| Sick leave | 🔴 red | No | No |
| Training | 🟡 yellow | No | No |
| Absent | ⚫ grey | No | No |

All statuses are fully editable from `/admin/statuses`. The **On-site** flag determines whether a desk reservation is allowed for that day.

---

## Database Backends

myPresence supports four SQL backends, selectable via the `DB_DRIVER` environment variable. All schema migrations run automatically on startup — no manual SQL required.

| Driver value | Engine | Minimum version |
|---|---|---|
| `sqlite` *(default)* | SQLite (embedded, no server) | — |
| `postgres` | PostgreSQL | 14+ |
| `mysql` | MySQL or MariaDB | MySQL 8+ / MariaDB 10.6+ |
| `sqlserver` | Microsoft SQL Server | 2019+ |

### SQLite (default)

No extra configuration needed. The database file is created automatically in `DATA_DIR` (`/data/app.db`).

```bash
-e DB_DRIVER=sqlite
```

### PostgreSQL

Create the database and user first:

```sql
CREATE DATABASE mypresence;
CREATE USER mypresence WITH PASSWORD 'strongpassword';
GRANT ALL PRIVILEGES ON DATABASE mypresence TO mypresence;
```

Then set:

```bash
-e DB_DRIVER=postgres
-e DB_HOST=postgres-host
-e DB_PORT=5432
-e DB_NAME=mypresence
-e DB_USER=mypresence
-e DB_PASSWORD=strongpassword
-e DB_SSL_MODE=disable   # disable | require | verify-full
```

### MySQL / MariaDB

Create the database and user first:

```sql
CREATE DATABASE mypresence CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'mypresence'@'%' IDENTIFIED BY 'strongpassword';
GRANT ALL PRIVILEGES ON mypresence.* TO 'mypresence'@'%';
FLUSH PRIVILEGES;
```

Then set:

```bash
-e DB_DRIVER=mysql
-e DB_HOST=mysql-host
-e DB_PORT=3306
-e DB_NAME=mypresence
-e DB_USER=mypresence
-e DB_PASSWORD=strongpassword
-e DB_SSL_MODE=disable   # disable | require | skip-verify | verify-full
```

### Microsoft SQL Server

Create the database and login first:

```sql
CREATE DATABASE mypresence;
CREATE LOGIN mypresence WITH PASSWORD = 'StrongP@ssword1';
USE mypresence;
CREATE USER mypresence FOR LOGIN mypresence;
GRANT CONTROL ON DATABASE::mypresence TO mypresence;
```

Then set:

```bash
-e DB_DRIVER=sqlserver
-e DB_HOST=sqlserver-host
-e DB_PORT=1433
-e DB_NAME=mypresence
-e DB_USER=mypresence
-e DB_PASSWORD=StrongP@ssword1
```

> `DB_SSL_MODE` is not used for SQL Server — TLS is negotiated automatically by the driver.

### Notes

- All four backends share the exact same schema and application behaviour.
- Switching backends requires a fresh database — there is no built-in migration tool between drivers.
- The `docker-compose.yml` includes commented-out service definitions for PostgreSQL, MariaDB, and SQL Server for local testing.

---

## Technical Architecture

```
myPresence/
├── Dockerfile              # Multi-stage build: Go → Alpine runtime
├── docker-compose.yml
├── main.go                 # Router, middleware wiring, template rendering
├── funcs.go                # Pure template helper functions (tested separately)
├── internal/
│   ├── config/             # Configuration loader (env vars)
│   ├── db/                 # Database layer (migrations, CRUD, seat reservations)
│   ├── handlers/           # HTTP handlers (calendar, activity, floorplan, admin, auth)
│   ├── middleware/         # Auth session, RequireRole() factory
│   └── models/             # Data structs and role constants
└── web/
    ├── static/
    │   ├── css/app.css
    │   └── js/app.js       # Alpine.js — drag-select calendar, half-day, seat modal, admin AJAX
    └── templates/          # Go HTML templates (layout + pages)
```

**Stack**:
- **Backend**: Go 1.23, `modernc.org/sqlite` (CGO-free), `crewjam/saml`
- **Frontend**: Alpine.js, Tailwind CSS (CDN)
- **Database**: SQLite (single file at `/data/app.db`)
- **Deployment**: Docker multi-stage build, static binary, Alpine 3.19 runtime

### Database Schema

| Table | Description |
|-------|-------------|
| `users` | Users (email, name, roles as comma-separated string, optional password hash) |
| `teams` | Teams |
| `user_teams` | User ↔ team many-to-many mapping |
| `statuses` | Presence statuses (name, color, billable, on_site flag, sort order) |
| `presences` | Recorded presences (user_id, date YYYY-MM-DD, half `full`/`AM`/`PM`, status_id) |
| `presence_logs` | Audit log of all set/clear presence actions (actor, target user, date, half, status) |
| `admin_logs` | Audit log of admin operations on entities (teams, statuses, holidays, users) |
| `sessions` | Active sessions (token, user_id, 30-day expiry) |
| `holidays` | Public holidays (date, name, allow_imputed) |
| `floorplans` | Floor map definitions (name, image path, sort order) |
| `seats` | Seats placed on a floorplan (label, x/y position as percentage of image) |
| `seat_reservations` | Seat bookings (seat_id, user_id, date, half — unique per seat+date+half) |
| `personal_access_tokens` | API tokens (description, SHA-256 hash, prefix, expiry, last-used timestamp, user_id) |

---

## Health Check

The endpoint `GET /health` is **public** (no session or authentication required) and is designed for monitoring systems.

```bash
curl http://localhost:8080/health
```

Example response:

```json
{
  "status": "ok",
  "uptime": "3h25m10s",
  "checks": {
    "database": "ok"
  },
  "time": "2026-04-13T12:00:00Z"
}
```

| HTTP code | Meaning |
|-----------|---------|
| `200 OK` | All checks passed — application is healthy |
| `503 Service Unavailable` | One or more checks failed (e.g. database unreachable) |

The response includes:
- **`status`**: `"ok"` or `"degraded"`
- **`uptime`**: time since last container start
- **`checks`**: individual check results (currently: `database`)
- **`time`**: current UTC timestamp (ISO 8601)

> Responses are never cached (`Cache-Control: no-store`).

---

## Prometheus Metrics

myPresence exposes a Prometheus-compatible `/metrics` endpoint protected by a static Bearer token.

### Enable

Set `METRICS_TOKEN` to a strong random value in `docker-compose.yml`:

```yaml
- METRICS_TOKEN=your-strong-random-token
```

The endpoint returns `404 Not Found` when the variable is unset or empty.

### Prometheus scrape config

```yaml
# prometheus.yml
scrape_configs:
  - job_name: mypresence
    bearer_token: your-strong-random-token   # same value as METRICS_TOKEN
    static_configs:
      - targets: ['presence-app:8080']       # hostname:port of the container
```

Alternatively, pass the token as a query parameter (less preferred):

```
GET http://presence-app:8080/metrics?token=your-strong-random-token
```

### Exposed metrics

| Metric | Type | Description |
|--------|------|-------------|
| `mypresence_http_requests_total{method,path,status_class}` | Counter | HTTP request count by method, normalised path, and status class (`2xx` / `4xx` / `5xx`) |
| `mypresence_http_request_duration_seconds{method,path}` | Histogram | HTTP latency distribution (P50 / P95 / P99) |
| `mypresence_auth_logins_total{method,result}` | Counter | Login attempts — `method`: `local`/`saml`, `result`: `success`/`failure` |
| `mypresence_auth_logouts_total` | Counter | Logout count |
| `mypresence_presence_operations_total{action,half}` | Counter | Presence set/clear API calls |
| `mypresence_presence_days_total{action}` | Counter | Individual day-records written or deleted |
| `mypresence_db_users_total` | Gauge | Registered users |
| `mypresence_db_sessions_active_total` | Gauge | Active sessions |
| `mypresence_db_teams_total` | Gauge | Teams |
| `mypresence_db_statuses_total` | Gauge | Presence statuses |
| `mypresence_db_presences_total` | Gauge | Total presence records stored |
| `mypresence_db_floorplans_total` | Gauge | Floor plans |
| `mypresence_db_seats_total` | Gauge | Seats across all floor plans |

Standard Go runtime metrics (`go_goroutines`, `go_memstats_*`, `go_gc_duration_seconds`, …) are also included automatically.

### Grafana dashboard

A ready-to-import Grafana dashboard is available at [`grafana/dashboard.json`](grafana/dashboard.json).

Import it via **Dashboards → Import → Upload JSON file** and select your Prometheus datasource. The dashboard covers:

- **Overview**: request rate, error rate, P95 latency, active sessions
- **HTTP Traffic**: requests/s by status class, latency percentiles, per-route table (RPS / errors / P95)
- **Authentication**: login success rate, failed login count, events timeline
- **Presence Activity**: set/clear operations, half-day breakdown (donut), daily write rate
- **Database**: gauge trends for users, sessions, presences, teams, seats
- **Go Runtime**: goroutines, heap memory, GC pause duration

---

## Rebuilding After Changes

Templates and static files are embedded (`//go:embed`) into the binary at build time. Any change requires a rebuild:

```bash
docker compose down && docker compose up -d --build
```

---

## Screenshots

### Login
![Login page](docs/screenshots/01-login.png)

### Personal Calendar (April 2026)
![Calendar with presence data](docs/screenshots/02-calendar.png)

### Activity Report
![Activity report for Engineering team](docs/screenshots/07-activity.png)

### Teams
![Teams management](docs/screenshots/05-teams.png)

### Users & Roles
![Users and roles management](docs/screenshots/04-users.png)

### Statuses
![Status configuration](docs/screenshots/03-statuses.png)

### Public Holidays
![Holidays configuration](docs/screenshots/06-holidays.png)

### Floor Plans
![Floor plan with seat reservations](docs/screenshots/08-floorplan.png)

### Grafana Dashboard
![Grafana dashboard](docs/screenshots/09-grafana.png)
