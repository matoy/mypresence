# ============================================================
# seed-demo-data.ps1  —  populates myPresence with demo data
# Fully self-contained: works on a fresh container with only
# the built-in admin/admin account.
# Run: pwsh -File scripts\seed-demo-data.ps1
# ============================================================

$Base = "http://localhost:8080"

# ── Login ─────────────────────────────────────────────────────────────────────
Write-Host "Logging in..."
$wr = [System.Net.WebRequest]::Create("$Base/login")
$wr.Method = "POST"; $wr.ContentType = "application/x-www-form-urlencoded"; $wr.AllowAutoRedirect = $false
$bd = [System.Text.Encoding]::UTF8.GetBytes("username=admin&password=admin")
$wr.ContentLength = $bd.Length; $st = $wr.GetRequestStream(); $st.Write($bd,0,$bd.Length); $st.Close()
$re = $wr.GetResponse()
$sc = (($re.Headers["Set-Cookie"]) -split ";")[0]
$re.Close()
Write-Host "  Session: $($sc.Substring(0,30))..."

$jh = @{ Cookie = $sc; "Content-Type" = "application/json" }

# ── Date references (all seeding is relative to today) ───────────────────────
$Now       = Get-Date
$CurYear   = $Now.Year
# Three completed months used for presences and time declarations
$m1        = $Now.AddMonths(-1)   # last month
$m2        = $Now.AddMonths(-2)   # detailed-pattern month
$m3        = $Now.AddMonths(-3)   # simple-pattern month
# Helper: last day of a given month
function Last-Day ($year, $month) { [DateTime]::new($year, $month, [DateTime]::DaysInMonth($year, $month)).ToString("yyyy-MM-dd") }
function First-Day ($year, $month) { "$year-$('{0:D2}' -f $month)-01" }

# ── Helpers ───────────────────────────────────────────────────────────────────
function PostJSON ($url, $obj) {
    $body = $obj | ConvertTo-Json -Compress -Depth 10
    try   { return Invoke-RestMethod $url -Method POST -Headers $jh -Body $body }
    catch { $m = $_.ErrorDetails.Message; if (-not $m) { $m = $_.Exception.Message }; Write-Warning "  POST $url -> $m"; return $null }
}
function PutJSON ($url, $obj) {
    $body = $obj | ConvertTo-Json -Compress -Depth 10
    try   { Invoke-RestMethod $url -Method PUT -Headers $jh -Body $body | Out-Null }
    catch { Write-Warning "  PUT $url -> $($_.Exception.Message)" }
}
function SetPresences ([int]$uid, [string[]]$dates, [int]$statusId) {
    if (-not $dates -or $dates.Count -eq 0) { return }
    PostJSON "$Base/api/presences" @{ user_id=$uid; dates=@($dates); status_id=$statusId; half="full" } | Out-Null
}
function Get-Weekdays ($year, $month) {
    $d = [DateTime]::new($year, $month, 1); $days = @()
    while ($d.Month -eq $month) {
        if ($d.DayOfWeek -notin 'Saturday','Sunday') { $days += $d.ToString("yyyy-MM-dd") }
        $d = $d.AddDays(1)
    }
    return $days
}

# ── Status IDs (auto-seeded at startup) ──────────────────────────────────────
$SITE=1; $REMOTE=2; $TRIP=3; $LEAVE=4; $SICK=5; $TRAINING=6

# ── 1. Create users ───────────────────────────────────────────────────────────
Write-Host "`nCreating users..."
$users = @(
    @{ email="alice.martin@corp.local";  name="Alice Martin";  password="demo1234"; role="team_manager" },
    @{ email="bob.dupont@corp.local";    name="Bob Dupont";    password="demo1234"; role="basic" },
    @{ email="claire.leroy@corp.local";  name="Claire Leroy";  password="demo1234"; role="basic" },
    @{ email="david.simon@corp.local";   name="David Simon";   password="demo1234"; role="basic" },
    @{ email="emma.garcia@corp.local";   name="Emma Garcia";   password="demo1234"; role="activity_viewer" },
    @{ email="felix.nguyen@corp.local";  name="Felix Nguyen";  password="demo1234"; role="basic" },
    @{ email="grace.chen@corp.local";    name="Grace Chen";    password="demo1234"; role="basic" },
    @{ email="hugo.moreau@corp.local";   name="Hugo Moreau";   password="demo1234"; role="basic" },
    @{ email="iris.blanc@corp.local";    name="Iris Blanc";    password="demo1234"; role="basic" },
    @{ email="julien.roux@corp.local";   name="Julien Roux";   password="demo1234"; role="basic" }
)
$userIDs = @{}
$userIDs["admin"] = 1
foreach ($u in $users) {
    $r = PostJSON "$Base/admin/users" @{ email=$u.email; name=$u.name; password=$u.password }
    if ($r -and $r.id) {
        $userIDs[$u.email] = [int]$r.id
        Write-Host "  '$($u.name)' id=$($r.id)"
        # Set roles (skip if basic — that's the default)
        if ($u.role -and $u.role -ne "basic") {
            $roleArray = @($u.role -split "," | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" })
            PutJSON "$Base/api/users/$($r.id)/roles" @{ roles=$roleArray }
        }
    } else {
        Write-Warning "  Failed to create '$($u.name)' (may already exist)"
    }
}
# Resolve IDs for users that already existed
$existingUsers = Invoke-RestMethod "$Base/api/users" -Headers $jh -ErrorAction SilentlyContinue
if ($existingUsers) {
    foreach ($eu in $existingUsers) {
        if (-not $userIDs[$eu.email]) {
            $userIDs[$eu.email] = [int]$eu.id
            Write-Host "  '$($eu.name)' id=$($eu.id) (existing)"
        }
    }
}

# Shorthand IDs for the fixed demo cast
$U = @{
    admin  = $userIDs["admin"]
    alice  = if ($userIDs["alice.martin@corp.local"]) { $userIDs["alice.martin@corp.local"] } else { 2 }
    bob    = if ($userIDs["bob.dupont@corp.local"])   { $userIDs["bob.dupont@corp.local"] }   else { 3 }
    claire = if ($userIDs["claire.leroy@corp.local"]) { $userIDs["claire.leroy@corp.local"] } else { 4 }
    david  = if ($userIDs["david.simon@corp.local"])  { $userIDs["david.simon@corp.local"] }  else { 5 }
    emma   = if ($userIDs["emma.garcia@corp.local"])  { $userIDs["emma.garcia@corp.local"] }  else { 6 }
    felix  = if ($userIDs["felix.nguyen@corp.local"]) { $userIDs["felix.nguyen@corp.local"] } else { 7 }
    grace  = if ($userIDs["grace.chen@corp.local"])   { $userIDs["grace.chen@corp.local"] }   else { 8 }
    hugo   = if ($userIDs["hugo.moreau@corp.local"])  { $userIDs["hugo.moreau@corp.local"] }  else { 9 }
    iris   = if ($userIDs["iris.blanc@corp.local"])   { $userIDs["iris.blanc@corp.local"] }   else { 10 }
    julien = if ($userIDs["julien.roux@corp.local"])  { $userIDs["julien.roux@corp.local"] }  else { 11 }
}
Write-Host "  User IDs: admin=$($U.admin), alice=$($U.alice), bob=$($U.bob) ..."

# ── 2. Create holidays (Global, France, Morocco, Czech Republic for current year) ─────────────
Write-Host "`nCreating public holidays (Global, FR, MA, CZ $CurYear)..."
# Fixed holidays (month-day never changes)
$holidays = @(
    @{ date="$CurYear-01-01"; name="New Year's Day";              allow_imputed=$false; country_code="" },
    @{ date="$CurYear-05-01"; name="Labour Day";                  allow_imputed=$false; country_code="" },
    @{ date="$CurYear-05-08"; name="Victory in Europe Day";       allow_imputed=$false; country_code="FR, CZ" },
    @{ date="$CurYear-07-14"; name="Bastille Day";                allow_imputed=$false; country_code="FR" },
    @{ date="$CurYear-07-30"; name="Throne Day (Morocco)";        allow_imputed=$false; country_code="MA" },
    @{ date="$CurYear-08-15"; name="Assumption Day";              allow_imputed=$false; country_code="FR" },
    @{ date="$CurYear-09-28"; name="St Wenceslas Day (Czech)";    allow_imputed=$false; country_code="CZ" },
    @{ date="$CurYear-10-28"; name="Independence Day (Czech)";    allow_imputed=$false; country_code="CZ" },
    @{ date="$CurYear-11-01"; name="All Saints' Day";             allow_imputed=$false; country_code="FR" },
    @{ date="$CurYear-11-06"; name="Green March (Morocco)";       allow_imputed=$false; country_code="MA" },
    @{ date="$CurYear-11-11"; name="Armistice Day";               allow_imputed=$false; country_code="FR, BE" },
    @{ date="$CurYear-11-18"; name="Independence Day (Morocco)";  allow_imputed=$false; country_code="MA" },
    @{ date="$CurYear-12-25"; name="Christmas Day";               allow_imputed=$false; country_code="" }
)
foreach ($h in $holidays) {
    $r = PostJSON "$Base/admin/holidays" $h
    if ($r -and $r.id) { Write-Host "  '$($h.name)' [$($h.country_code)] id=$($r.id)" }
    else                { Write-Warning "  '$($h.name)' may already exist" }
}
# Build lookup of non-imputable global/FR holiday dates for seed presence filtering
$nonImputedSet = @{}
foreach ($h in $holidays) {
    $codes = @($h.country_code -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" })
    if (-not $h.allow_imputed -and ($codes.Count -eq 0 -or $codes -contains "FR")) {
        $nonImputedSet[$h.date] = $true
    }
}

function Get-WorkingDays ($year, $month) {
    $d = [DateTime]::new($year, $month, 1); $days = @()
    while ($d.Month -eq $month) {
        $ds = $d.ToString("yyyy-MM-dd")
        if ($d.DayOfWeek -notin 'Saturday','Sunday' -and -not $nonImputedSet[$ds]) { $days += $ds }
        $d = $d.AddDays(1)
    }
    return $days
}

# ── 3. Create sites & floorplans ──────────────────────────────────────────────

Write-Host "`nCreating sites..."
$siteItems = @(
    @{ name="Siège Social Paris";        country_code="FR"; not_corporate_site=$false },
    @{ name="Site Logistique Troyes";    country_code="FR"; not_corporate_site=$false },
    @{ name="Hub Régional Casablanca";   country_code="MA"; not_corporate_site=$false },
    @{ name="Espace Coworking Lyon";     country_code="FR"; not_corporate_site=$true }
)
$siteIDs = @{}
foreach ($si in $siteItems) {
    $r = PostJSON "$Base/admin/sites" $si
    if ($r -and $r.id) {
        $siteIDs[$si.name] = [int]$r.id
        Write-Host "  Site '$($si.name)' [$($si.country_code)] id=$($r.id)"
    } else {
        Write-Warning "  Site '$($si.name)' may already exist"
    }
}
if ($siteIDs.Count -eq 0) {
    $existSites = Invoke-RestMethod "$Base/api/admin/sites" -Headers $jh -ErrorAction SilentlyContinue
    if ($existSites) {
        foreach ($es in $existSites) {
            $siteIDs[$es.name] = [int]$es.id
        }
    }
}

$parisSiteId = if ($siteIDs["Siège Social Paris"]) { $siteIDs["Siège Social Paris"] } else { 0 }

# Helper: generate a simple fallback PNG if no local image is provided
function New-FloorplanPNG {
    Add-Type -AssemblyName System.Drawing -ErrorAction Stop
    $W = 900; $H = 600
    $bmp = [System.Drawing.Bitmap]::new($W, $H)
    $g   = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.Clear([System.Drawing.Color]::FromArgb(248, 250, 252))
    $wallPen  = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(71, 85, 105), 4)
    $divPen   = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(148, 163, 184), 2)
    $zoneFill = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(226, 232, 240))
    $meetFill = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(219, 234, 254))
    $corrFill = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(241, 245, 249))
    $txtBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(71, 85, 105))
    $font     = [System.Drawing.Font]::new("Arial", 11, [System.Drawing.FontStyle]::Bold)
    $fontSm   = [System.Drawing.Font]::new("Arial",  9, [System.Drawing.FontStyle]::Regular)
    $g.DrawRectangle($wallPen, 10, 10, $W-20, $H-20)
    $g.FillRectangle($corrFill, 11, 270, $W-22, 60)
    $g.DrawLine($divPen, 11, 270, $W-11, 270); $g.DrawLine($divPen, 11, 330, $W-11, 330)
    $g.DrawString("Corridor", $fontSm, $txtBrush, 400, 285)
    $g.FillRectangle($zoneFill, 11, 11, 430, 258); $g.DrawRectangle($divPen, 11, 11, 430, 258)
    $g.DrawString("Zone A — Open Space", $font, $txtBrush, 110, 120)
    $g.FillRectangle($zoneFill, 442, 11, 230, 258); $g.DrawRectangle($divPen, 442, 11, 230, 258)
    $g.DrawString("Zone B", $font, $txtBrush, 510, 120)
    $g.FillRectangle($meetFill, 673, 11, $W-683, 258); $g.DrawRectangle($divPen, 673, 11, $W-683, 258)
    $g.DrawString("Meeting", $font, $txtBrush, 720, 120)
    foreach ($obj in @($g,$wallPen,$divPen,$zoneFill,$meetFill,$corrFill,$txtBrush,$font,$fontSm)) { $obj.Dispose() }
    $ms = [System.IO.MemoryStream]::new()
    $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose()
    $bytes = $ms.ToArray(); $ms.Dispose(); return $bytes
}

# Helper: upload image bytes as multipart/form-data
function Upload-FloorplanImage ($fpId, [byte[]]$imgBytes, [string]$filename = "floorplan.jpg", [string]$mime = "image/jpeg") {
    $boundary = [System.Guid]::NewGuid().ToString("N")
    $ms  = [System.IO.MemoryStream]::new()
    $sw  = [System.IO.StreamWriter]::new($ms, [System.Text.Encoding]::ASCII)
    $sw.Write("--$boundary`r`nContent-Disposition: form-data; name=`"image`"; filename=`"$filename`"`r`nContent-Type: $mime`r`n`r`n")
    $sw.Flush()
    $ms.Write($imgBytes, 0, $imgBytes.Length)
    $sw.Write("`r`n--$boundary--`r`n"); $sw.Flush()
    $body = $ms.ToArray(); $ms.Dispose()
    $hdrs = @{ Cookie = $sc; "Content-Type" = "multipart/form-data; boundary=$boundary" }
    try   { Invoke-RestMethod "$Base/admin/floorplans/$fpId/image" -Method POST -Headers $hdrs -Body $body | Out-Null; Write-Host "  Floorplan image uploaded ($filename)" }
    catch { Write-Warning "  Image upload failed: $_" }
}

Write-Host "`nCreating floorplan..."
$fp = PostJSON "$Base/admin/floorplans" @{ name="HQ Open Space"; site_id=$parisSiteId }
if ($fp -and $fp.id) {
    $fpID = [int]$fp.id
    Write-Host "  Floorplan id=$fpID"

    # Use provided demo image if available, otherwise generate a fallback PNG
    $localImg = Join-Path $PSScriptRoot "demo-floorplan.jpg"
    if (Test-Path $localImg) {
        Write-Host "  Using local image: $localImg"
        Upload-FloorplanImage $fpID ([System.IO.File]::ReadAllBytes($localImg)) "demo-floorplan.jpg" "image/jpeg"
    } else {
        Write-Host "  No demo-floorplan.jpg found — generating placeholder image"
        Write-Host "  (Save the floor plan image as scripts\demo-floorplan.jpg for a realistic plan)"
        try { Upload-FloorplanImage $fpID (New-FloorplanPNG) "floorplan.png" "image/png" }
        catch { Write-Warning "  Could not generate floor plan image: $_" }
    }

    # Seat positions calibrated for the 3D perspective office photo (demo-floorplan.jpg).
    # The image shows: left open space (Zone A), right open space (Zone B), meeting room (bottom-right).
    # x_pct = % from left edge, y_pct = % from top edge of the image.
    $seats = @(
        # Zone A — left open space, 4 rows of workstations visible in perspective
        @{ label="A1"; x_pct=7.0; y_pct=76.0 },   # front-left row, left seat
        @{ label="A2"; x_pct=13.0; y_pct=70.0 },   # front-left row, right seat
        @{ label="A3"; x_pct=7.0; y_pct=66.0 },   # 2nd row, left
        @{ label="A4"; x_pct=20.0; y_pct=58.0 },   # 2nd row, right
        @{ label="A5"; x_pct=16.0; y_pct=51.0 },   # 3rd row, left
        @{ label="A6"; x_pct=24.0; y_pct=50.0 },   # 3rd row, right
        @{ label="A7"; x_pct=26.0; y_pct=37.0 },   # back row, left
        @{ label="A8"; x_pct=33.0; y_pct=36.0 },   # back row, right

        # Zone B — right open space, workstations seen from back-right angle
        @{ label="B1"; x_pct=58.0; y_pct=41.0 },
        @{ label="B2"; x_pct=61.0; y_pct=35.0 },
        @{ label="B3"; x_pct=64.0; y_pct=45.0 },
        @{ label="B4"; x_pct=66.0; y_pct=39.0 },
        @{ label="B5"; x_pct=65.0; y_pct=28.0 }
    )
    $seatIDs = @{}
    foreach ($s in $seats) {
        $sr = PostJSON "$Base/admin/floorplans/$fpID/seats" $s
        if ($sr -and $sr.id) {
            $seatIDs[$s.label] = [int]$sr.id
            Write-Host "  Seat '$($s.label)' id=$($sr.id)"
        }
    }
} else {
    Write-Warning "  Failed to create floorplan (may already exist) — skipping seats"
    # Try to get existing floorplan
    $fpList = Invoke-RestMethod "$Base/api/floorplans" -Headers $jh -ErrorAction SilentlyContinue
    if ($fpList -and $fpList.Count -gt 0) {
        $fpID = [int]$fpList[0].id
        Write-Host "  Using existing floorplan id=$fpID"
        $existSeats = Invoke-RestMethod "$Base/api/seats?floorplan_id=$fpID" -Headers $jh -ErrorAction SilentlyContinue
        $seatIDs = @{}
        if ($existSeats) { foreach ($s in $existSeats) { $seatIDs[$s.label] = [int]$s.id } }
    }
}

# ── 4. Create teams ───────────────────────────────────────────────────────────
Write-Host "`nCreating teams..."
$teamDefs = @(
    @{ name="Engineering"; country_codes="FR, MA" },
    @{ name="Marketing";   country_codes="FR" },
    @{ name="Sales";       country_codes="FR, CZ" },
    @{ name="HR";          country_codes="FR" }
)
$teamIDs = @{}
foreach ($t in $teamDefs) {
    $r = PostJSON "$Base/admin/teams" @{ name=$t.name; country_codes=$t.country_codes }
    if ($r -and $r.id) {
        $teamIDs[$t.name] = [int]$r.id
        Write-Host "  '$($t.name)' [$($t.country_codes)] id=$($r.id)"
    } else {
        Write-Warning "  Failed to create team '$($t.name)' (may already exist)"
    }
}
# Resolve IDs for any teams that already existed
if ($teamIDs.Count -lt 4) {
    $existing = Invoke-RestMethod "$Base/api/teams" -Headers $jh -ErrorAction SilentlyContinue
    if ($existing) {
        foreach ($t in $existing) {
            if (-not $teamIDs[$t.name] -and (@("Engineering","Marketing","Sales","HR") -contains $t.name)) {
                $teamIDs[$t.name] = [int]$t.id
                Write-Host "  '$($t.name)' id=$($t.id) (existing)"
            }
        }
    }
}

# Enable "Timesheets managed manually" + Jira integration for Sales and HR:
# their members declare daily activities (Jira ticket / ServiceNow / other)
# instead of monthly per-project day allocations.
$manualTeams = @(
    @{ name="Sales"; jiraKey="SCRM"; country_codes="FR, CZ" },
    @{ name="HR";    jiraKey="HRXP"; country_codes="FR" }
)
foreach ($mt in $manualTeams) {
    $tid = $teamIDs[$mt.name]
    if (-not $tid) { Write-Warning "  Team '$($mt.name)' not found, cannot enable manual timesheets"; continue }
    PutJSON "$Base/admin/teams/$tid" @{ name=$mt.name; jira_space_key=$mt.jiraKey; timesheets_managed_manually=$true; country_codes=$mt.country_codes }
    Write-Host "  '$($mt.name)' -> timesheets managed manually (Jira space: $($mt.jiraKey), countries: $($mt.country_codes))"
}

# ── 5. Team members ───────────────────────────────────────────────────────────
Write-Host "`nAdding team members..."
$memberships = @(
    @{ team="Engineering"; keys=@("admin","alice","bob","claire","david","felix") },
    @{ team="Marketing";   keys=@("emma","grace","hugo") },
    @{ team="Sales";       keys=@("iris","julien","bob") },
    @{ team="HR";          keys=@("admin","alice","emma") }
)
foreach ($m in $memberships) {
    $tid = $teamIDs[$m.team]
    if (-not $tid) { Write-Warning "  Team '$($m.team)' not found"; continue }
    foreach ($key in $m.keys) {
        $uid = $U[$key]
        if ($uid) { PostJSON "$Base/admin/teams/$tid/members" @{user_id=[int]$uid} | Out-Null }
    }
    Write-Host "  $($m.team) (id=$tid): $($m.keys -join ',')"
}

# ── 5a. Team Leaders ───────────────────────────────────────────────────────────
Write-Host "`nAssigning team leaders..."
$teamLeaderDefs = @(
    @{ team="Engineering"; leaders=@("bob") },
    @{ team="Marketing";   leaders=@("emma") },
    @{ team="Sales";       leaders=@("bob") },
    @{ team="HR";          leaders=@("emma") }
)
foreach ($tl in $teamLeaderDefs) {
    $tid = $teamIDs[$tl.team]
    if (-not $tid) { continue }
    $leaderUserIDs = @($tl.leaders | ForEach-Object { [int]$U[$_] } | Where-Object { $_ -gt 0 })
    PutJSON "$Base/api/admin/teams/$tid/leaders" @{ user_ids=$leaderUserIDs } | Out-Null
    Write-Host "  $($tl.team): $($tl.leaders -join ',') ($($leaderUserIDs -join ','))"
}

# ── 5b. Domains ────────────────────────────────────────────────────────────────
# Domains group several teams under one or more managers for aggregated
# activity reporting (see /admin/activity and /admin/projects-report?view=activities).
Write-Host "`nCreating domains..."
$domains = @(
    @{ name="Go To Market"; managers=@("emma"); teams=@("Marketing","Sales") },
    @{ name="Corporate";    managers=@("alice"); teams=@("Engineering","HR") }
)
foreach ($d in $domains) {
    # NB: use distinct names from $teamIDs (the team name->id map) — PowerShell
    # variable names are case-insensitive, so a local $teamIds would silently
    # overwrite it after the first iteration, breaking every domain after that.
    $domainManagerIds = @($d.managers | ForEach-Object { [int]$U[$_] })
    $domainTeamIds     = @($d.teams    | ForEach-Object { [int]$teamIDs[$_] })
    $r = PostJSON "$Base/admin/domains" @{ name=$d.name; manager_ids=$domainManagerIds; team_ids=$domainTeamIds }
    if ($r -and $r.id) {
        Write-Host "  '$($d.name)' id=$($r.id) — managers: $($d.managers -join ','); teams: $($d.teams -join ',')"
    } else {
        Write-Warning "  Failed to create domain '$($d.name)' (may already exist)"
    }
}

# ── 6. Presences: M-3 and M-1 (all users — on-site except Wed=remote) ─────────
Write-Host "`nSeeding $($m3.ToString('MMMM yyyy')) and $($m1.ToString('MMMM yyyy')) presences..."
$allUIDs = @($U.admin, $U.alice, $U.bob, $U.claire, $U.david, $U.emma, $U.felix, $U.grace, $U.hugo, $U.iris, $U.julien)

foreach ($uid in $allUIDs) {
    $days3 = Get-WorkingDays $m3.Year $m3.Month
    SetPresences $uid @($days3 | Where-Object { ([DateTime]::Parse($_)).DayOfWeek -ne 'Wednesday' }) $SITE
    SetPresences $uid @($days3 | Where-Object { ([DateTime]::Parse($_)).DayOfWeek -eq 'Wednesday' }) $REMOTE
    $days1 = Get-WorkingDays $m1.Year $m1.Month
    SetPresences $uid @($days1 | Where-Object { ([DateTime]::Parse($_)).DayOfWeek -ne 'Wednesday' }) $SITE
    SetPresences $uid @($days1 | Where-Object { ([DateTime]::Parse($_)).DayOfWeek -eq 'Wednesday' }) $REMOTE
}
Write-Host "  $($m3.ToString('MMM yyyy')) + $($m1.ToString('MMM yyyy')) done for $($allUIDs.Count) users"

# ── 7. Presences: M-2 (unique per-user patterns) ──────────────────────────────
# Pattern: Wed always remote; an extra "remote day" per user for variety;
# 1-2 special-status days per user (LEAVE/SICK/TRIP/TRAINING).
Write-Host "`nSeeding $($m2.ToString('MMMM yyyy')) presences (per-user patterns)..."

# $remoteExtra: additional day-of-week that is remote (empty string = none)
# $specials: list of @{idx=N; st=statusId}  (negative idx = offset from end of working-day list)
function Seed-DetailedMonth ([int]$uid, [string]$remoteExtra, [array]$specials) {
    $days = Get-WorkingDays $m2.Year $m2.Month
    $n    = $days.Count
    $spec = @{}
    foreach ($s in $specials) {
        $i = if ($s.idx -lt 0) { $n + $s.idx } else { $s.idx }
        if ($i -ge 0 -and $i -lt $n) { $spec[$days[$i]] = $s.st }
    }
    $siteDays = @(); $remoteDays = @()
    foreach ($d in $days) {
        if ($spec.ContainsKey($d)) { continue }
        $dow = ([DateTime]::Parse($d)).DayOfWeek
        if ($dow -eq 'Wednesday' -or ($remoteExtra -and $dow -eq $remoteExtra)) { $remoteDays += $d }
        else { $siteDays += $d }
    }
    SetPresences $uid $siteDays   $SITE
    SetPresences $uid $remoteDays $REMOTE
    foreach ($kv in $spec.GetEnumerator()) { SetPresences $uid @($kv.Key) $kv.Value }
}

Seed-DetailedMonth $U.admin  ""        @(); Write-Host "  admin"
Seed-DetailedMonth $U.alice  "Friday"  @( @{idx=-1; st=$LEAVE} ); Write-Host "  Alice"
Seed-DetailedMonth $U.bob    "Monday"  @(); Write-Host "  Bob"
Seed-DetailedMonth $U.claire "Friday"  @(); Write-Host "  Claire"
Seed-DetailedMonth $U.david  "Monday"  @(); Write-Host "  David"
Seed-DetailedMonth $U.emma   "Friday"  @( @{idx=3;  st=$TRIP} );  Write-Host "  Emma"
Seed-DetailedMonth $U.felix  "Monday"  @( @{idx=0;  st=$SICK} );  Write-Host "  Felix"
Seed-DetailedMonth $U.grace  ""        @(); Write-Host "  Grace"
Seed-DetailedMonth $U.hugo   "Friday"  @( @{idx=-2; st=$LEAVE} ); Write-Host "  Hugo"
Seed-DetailedMonth $U.iris   "Monday"  @(); Write-Host "  Iris"
Seed-DetailedMonth $U.julien "Monday"  @( @{idx=1;  st=$SICK}, @{idx=0; st=$TRAINING} ); Write-Host "  Julien"
Write-Host "  $($m2.ToString('MMMM yyyy')) done"

# ── 8. Seat reservations (admin on seat A1, first 10 site days of M-2) ─────────
$seatA1 = if ($seatIDs -and $seatIDs["A1"]) { $seatIDs["A1"] } else { 1 }
Write-Host "`nBooking seat reservations for admin (seat A1, id=$seatA1)..."
$adminSiteDays = @(Get-WorkingDays $m2.Year $m2.Month | Where-Object {
    ([DateTime]::Parse($_)).DayOfWeek -notin 'Wednesday'
} | Select-Object -First 10)
$r = PostJSON "$Base/api/reservations/bulk" @{seat_id=$seatA1; dates=$adminSiteDays; half="full"}
if ($r) { Write-Host "  Seat A1 reserved: $($r.booked) days" }

# ── 9. Projects ───────────────────────────────────────────────────────────────
Write-Host "`nCreating projects..."
# Give alice projects_manager role (in addition to her team_manager role)
PutJSON "$Base/api/users/$($U.alice)/roles" @{ roles=@("team_manager","projects_manager") } | Out-Null
Write-Host "  alice: roles set to team_manager + projects_manager"

$projIDs = @{}
# Pre-load any projects that already exist so we never create duplicates
try {
    $existing = Invoke-RestMethod "$Base/api/admin/projects?active=" -Headers $jh -ErrorAction Stop
    if ($existing -and $existing.projects) {
        foreach ($p in $existing.projects) { $projIDs[$p.code] = [int]$p.id }
        Write-Host "  $($projIDs.Count) existing project(s) resolved"
    }
} catch { Write-Warning "  Could not pre-load existing projects" }

# Project dates are relative to the current year / month
$yearStart  = First-Day $CurYear 1
$yearEnd    = Last-Day  $CurYear 12
$campStart  = First-Day $m3.Year $m3.Month          # starts 3 months ago
$campEnd    = Last-Day  $Now.AddMonths(2).Year $Now.AddMonths(2).Month   # ends 2 months from now
$hrxpStart  = First-Day $Now.AddMonths(-5).Year $Now.AddMonths(-5).Month # starts 5 months ago
$hrxpEnd    = Last-Day  $m1.Year $m1.Month          # ends last month

$projectDefs = @(
    @{ name="Alpha Platform";    code="ALPHA"; team="Engineering"; active=$true; start_date=$yearStart; end_date=$yearEnd  },
    @{ name="Beta App";          code="BETA";  team="Engineering"; active=$true; start_date=$yearStart; end_date=$yearEnd  },
    @{ name="Campaign Spring";   code="CAMP";  team="Marketing";   active=$true; start_date=$campStart; end_date=$campEnd  },
    @{ name="Sales CRM";         code="SCRM";  team="Sales";       active=$true; start_date=$yearStart; end_date=$yearEnd  },
    @{ name="HR Transformation"; code="HRXP";  team="HR";          active=$true; start_date=$hrxpStart; end_date=$hrxpEnd  }
)
foreach ($p in $projectDefs) {
    if ($projIDs[$p.code]) { Write-Host "  '$($p.code)' already exists (id=$($projIDs[$p.code]))"; continue }
    $tid = $teamIDs[$p.team]
    $r = PostJSON "$Base/api/admin/projects" @{ name=$p.name; code=$p.code; team_id=[int]$tid; active=$p.active; start_date=$p.start_date; end_date=$p.end_date }
    if ($r -and $r.id) {
        $projIDs[$p.code] = [int]$r.id
        Write-Host "  '$($p.code)' '$($p.name)' id=$($r.id)"
    } else {
        Write-Warning "  Failed to create project '$($p.code)'"
    }
}
# Resolve IDs for any projects that still failed
if ($projIDs.Count -lt $projectDefs.Count) {
    try {
        $existing = Invoke-RestMethod "$Base/api/admin/projects?active=" -Headers $jh
        if ($existing -and $existing.projects) {
            foreach ($p in $existing.projects) {
                if (-not $projIDs[$p.code] -and (@("ALPHA","BETA","CAMP","SCRM","HRXP") -contains $p.code)) {
                    $projIDs[$p.code] = [int]$p.id
                    Write-Host "  '$($p.code)' id=$($p.id) (resolved)"
                }
            }
        }
    } catch { Write-Warning "  Could not resolve remaining project IDs" }
}

# ── 10. Project members ───────────────────────────────────────────────────────
Write-Host "`nAssigning project members..."
function SetMembers ($projCode, [int[]]$userKeys) {
    $projId = $projIDs[$projCode]
    if (-not $projId) { Write-Warning "  Project '$projCode' not found"; return }
    $uids = @($userKeys | ForEach-Object { [int]$_ })
    $body = @{ user_ids = $uids } | ConvertTo-Json -Compress
    try {
        Invoke-RestMethod "$Base/api/admin/projects/$projId/members" -Method PUT -Headers $jh -Body $body | Out-Null
        Write-Host "  $projCode (id=$projId): $($uids.Count) member(s)"
    } catch { Write-Warning "  SetMembers $projCode -> $($_.Exception.Message)" }
}

# ALPHA: engineering team (admin, alice, bob, claire, david, felix)
SetMembers "ALPHA" @($U.admin, $U.alice, $U.bob, $U.claire, $U.david, $U.felix)
# BETA:  engineering team (admin, alice, bob, claire, david, felix)
SetMembers "BETA"  @($U.admin, $U.alice, $U.bob, $U.claire, $U.david, $U.felix)
# CAMP:  marketing team (emma, grace, hugo)
SetMembers "CAMP"  @($U.emma, $U.grace, $U.hugo)
# SCRM:  sales team (bob, iris, julien)
SetMembers "SCRM"  @($U.bob, $U.iris, $U.julien)
# HRXP:  HR (admin, alice, emma)
SetMembers "HRXP"  @($U.admin, $U.alice, $U.emma)

# ── 11. Project time declarations ─────────────────────────────────────────────
Write-Host "`nDeclaring project time for users..."

function LoginAs ($email, $password) {
    $wr = [System.Net.WebRequest]::Create("$Base/login")
    $wr.Method = "POST"; $wr.ContentType = "application/x-www-form-urlencoded"; $wr.AllowAutoRedirect = $false
    $bd = [System.Text.Encoding]::UTF8.GetBytes("username=$email&password=$password")
    $wr.ContentLength = $bd.Length; $st = $wr.GetRequestStream(); $st.Write($bd,0,$bd.Length); $st.Close()
    try { $re = $wr.GetResponse() } catch { $re = $_.Exception.Response }
    $c = (($re.Headers["Set-Cookie"]) -split ";")[0]
    $re.Close()
    return @{ Cookie = $c; "Content-Type" = "application/json" }
}
function DeclareTime ($headers, $projectId, $year, $month, $days) {
    $body = @{ project_id=[int]$projectId; year=[int]$year; month=[int]$month; days=[double]$days } | ConvertTo-Json -Compress
    try { Invoke-RestMethod "$Base/api/project-time" -Method POST -Headers $headers -Body $body | Out-Null; return $true }
    catch { $m = $_.ErrorDetails.Message; if (-not $m) { $m = $_.Exception.Message }; Write-Warning "    project-time $projectId -> $m"; return $false }
}
function GetBillable ($headers, $year, $month) {
    try { $r = Invoke-RestMethod "$Base/api/projects?year=$year&month=$month" -Headers $headers; return [double]$r.billable_days }
    catch { return 0.0 }
}
function RoundHalf ($v) { return [Math]::Round($v * 2) / 2 }

# Fraction of billable days to allocate per project per user
# Totals are kept <= 0.90 so users always have some unset days for realism
$projectAlloc = @{
    "admin"  = @( @{c="ALPHA";f=0.55}, @{c="BETA";f=0.30}, @{c="HRXP";f=0.05} )
    "alice"  = @( @{c="ALPHA";f=0.60}, @{c="BETA";f=0.25}, @{c="HRXP";f=0.05} )
    "bob"    = @( @{c="ALPHA";f=0.45}, @{c="BETA";f=0.35}, @{c="SCRM";f=0.05} )
    "claire" = @( @{c="ALPHA";f=0.70}, @{c="BETA";f=0.20} )
    "david"  = @( @{c="ALPHA";f=0.50}, @{c="BETA";f=0.40} )
    "emma"   = @( @{c="CAMP";f=0.75},  @{c="HRXP";f=0.10} )
    "felix"  = @( @{c="BETA";f=0.80} )
    "grace"  = @( @{c="CAMP";f=0.85} )
    "hugo"   = @( @{c="CAMP";f=0.75} )
    "iris"   = @( @{c="SCRM";f=0.80} )
    "julien" = @( @{c="SCRM";f=0.85} )
}
$userCreds = @{
    "admin"  = @{ email="admin";                     password="admin" }
    "alice"  = @{ email="alice.martin@corp.local";   password="demo1234" }
    "bob"    = @{ email="bob.dupont@corp.local";     password="demo1234" }
    "claire" = @{ email="claire.leroy@corp.local";   password="demo1234" }
    "david"  = @{ email="david.simon@corp.local";    password="demo1234" }
    "emma"   = @{ email="emma.garcia@corp.local";    password="demo1234" }
    "felix"  = @{ email="felix.nguyen@corp.local";   password="demo1234" }
    "grace"  = @{ email="grace.chen@corp.local";     password="demo1234" }
    "hugo"   = @{ email="hugo.moreau@corp.local";    password="demo1234" }
    "iris"   = @{ email="iris.blanc@corp.local";     password="demo1234" }
    "julien" = @{ email="julien.roux@corp.local";    password="demo1234" }
}
# Seed the three most recently completed months
$seedMonths = @(
    @{ year=$m3.Year; month=$m3.Month },
    @{ year=$m2.Year; month=$m2.Month },
    @{ year=$m1.Year; month=$m1.Month }
)

foreach ($key in $projectAlloc.Keys) {
    $creds         = $userCreds[$key]
    $headers       = LoginAs $creds.email $creds.password
    $userAllocList = $projectAlloc[$key]
    Write-Host "  $key"
    foreach ($ym in $seedMonths) {
        $billable = GetBillable $headers $ym.year $ym.month
        if ($billable -le 0) { continue }
        $remaining = $billable
        foreach ($a in $userAllocList) {
            $projID = $projIDs[$a.c]
            if (-not $projID) { continue }
            $days = RoundHalf ($billable * $a.f)
            if ($days -gt $remaining) { $days = RoundHalf $remaining }
            if ($days -le 0) { continue }
            if (DeclareTime $headers $projID $ym.year $ym.month $days) {
                $remaining -= $days
                Write-Host "    $($ym.year)-$('{0:D2}' -f $ym.month) $($a.c): $days d (billable=$billable)"
            }
        }
    }
}

# ── 12. Team activities (Timesheets managed manually) ────────────────────────
# Members of the Sales and HR teams (manual mode, enabled above) declare daily
# activities instead of monthly project days: Jira tickets, ServiceNow
# requests, or other/administrative work.
Write-Host "`nDeclaring team activities (Sales & HR — manual timesheets)..."
function DeclareActivity ($headers, $date, $type, $jiraKey, $jiraTitle, $comment, $percentage) {
    $body = @{ date=$date; activity_type=$type; jira_key=$jiraKey; jira_title=$jiraTitle; comment=$comment; percentage=[double]$percentage } | ConvertTo-Json -Compress
    try { Invoke-RestMethod "$Base/api/project-activities" -Method POST -Headers $headers -Body $body | Out-Null; return $true }
    catch { $m = $_.ErrorDetails.Message; if (-not $m) { $m = $_.Exception.Message }; Write-Warning "    activity $date -> $m"; return $false }
}
$manualUsers = @(
    @{ key="bob";    jiraPrefix="SCRM" },
    @{ key="iris";   jiraPrefix="SCRM" },
    @{ key="julien"; jiraPrefix="SCRM" },
    @{ key="admin";  jiraPrefix="HRXP" },
    @{ key="alice";  jiraPrefix="HRXP" },
    @{ key="emma";   jiraPrefix="HRXP" }
)
foreach ($mu in $manualUsers) {
    $creds   = $userCreds[$mu.key]
    $headers = LoginAs $creds.email $creds.password
    $days    = Get-WorkingDays $m1.Year $m1.Month
    $declared = 0
    for ($i = 0; $i -lt $days.Count; $i++) {
        $d = $days[$i]
        switch ($i % 3) {
            0 {
                $num = 100 + $i
                if (DeclareActivity $headers $d "jira" "$($mu.jiraPrefix)-$num" "Investigate reported issue #$num" "" 100) { $declared++ }
            }
            1 {
                $num = 20000 + $i
                if (DeclareActivity $headers $d "servicenow" "" "" "INC$num - service request follow-up" 100) { $declared++ }
            }
            2 {
                if (DeclareActivity $headers $d "other" "" "" "Internal task / administrative work" 100) { $declared++ }
            }
        }
    }
    Write-Host "  $($mu.key): $declared activity/activities declared for $($m1.ToString('MMM yyyy'))"
}

Write-Host "`nSeed complete!"

# ── 13. News banners ──────────────────────────────────────────────────────────
Write-Host "`nCreating news banners..."
# Pre-load existing titles so we never create duplicates
$existingNewsTitles = @{}
try {
    $existingNews = Invoke-RestMethod "$Base/api/admin/news" -Headers $jh -ErrorAction Stop
    if ($existingNews) { foreach ($n in $existingNews) { $existingNewsTitles[$n.title] = $true } }
    Write-Host "  $($existingNewsTitles.Count) existing banner(s) found"
} catch { Write-Warning "  Could not pre-load existing news banners" }

# Dates relative to today
$curMonthStart  = First-Day $Now.Year $Now.Month
$curMonthEnd    = Last-Day  $Now.Year $Now.Month
$maintenanceDay = $Now.AddDays(14).ToString("yyyy-MM-dd")
$recurStart     = $Now.ToString("yyyy-MM") + "-20"
$recurEnd       = $Now.ToString("yyyy-MM") + "-25"

$newsItems = @(
    @{
        title      = "🎉 Bienvenue sur myPresence !"
        content    = "Cette démo illustre toutes les fonctionnalités de myPresence. Consultez la [documentation](https://github.com/matoy/mypresence) pour en savoir plus."
        start_date = "$CurYear-01-01"
        end_date   = "$CurYear-12-31"
        bg_color   = "#16a34a"
        recurring  = $false
    },
    @{
        title      = "📅 Rappel : saisie des présences du mois"
        content    = "Pensez à renseigner vos présences pour le mois en cours avant le dernier jour. Contact : [RH](mailto:rh@corp.local)"
        start_date = $curMonthStart
        end_date   = $curMonthEnd
        bg_color   = "#2563eb"
        recurring  = $false
    },
    @{
        title      = "⚠️ Maintenance planifiée"
        content    = "Une maintenance du système est prévue prochainement. myPresence sera indisponible pendant une courte période."
        start_date = $maintenanceDay
        end_date   = $maintenanceDay
        bg_color   = "#dc2626"
        recurring  = $false
    },
    @{
        title      = "📋 Rappel mensuel : saisie des présences"
        content    = "Rappel : pensez à saisir vos présences avant le 25 de chaque mois. Questions ? Contactez [les RH](mailto:rh@corp.local)."
        start_date = $recurStart
        end_date   = $recurEnd
        bg_color   = "#7c3aed"
        recurring  = $true
    }
)
foreach ($n in $newsItems) {
    if ($existingNewsTitles[$n.title]) { Write-Host "  '$($n.title)' already exists, skipping"; continue }
    $r = PostJSON "$Base/api/admin/news" $n
    if ($r -and $r.id) { Write-Host "  '$($n.title)' id=$($r.id)" }
    else                { Write-Warning "  Failed to create news '$($n.title)'" }
}

Write-Host "`nSeed complete (with news banners)!"
