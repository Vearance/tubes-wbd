[CmdletBinding()]
param(
    [ValidateRange(1, 2147483647)]
    [int]$Rows = 1000000,

    [ValidateRange(1, 10000)]
    [int]$BatchSize = 5000,

    [switch]$SkipSeed,

    [ValidateRange(1, 86400)]
    [int]$Timeout = 1800
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Push-Location $Root
$Work = Join-Path ([System.IO.Path]::GetTempPath()) ("bench-batch-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Work | Out-Null

function Get-DotEnvValue([string]$Key, [string]$Default) {
    $envFile = Join-Path $Root '.env'
    if (Test-Path $envFile) {
        $line = Get-Content $envFile | Where-Object { $_ -match "^\s*$([regex]::Escape($Key))=" } | Select-Object -Last 1
        if ($line) {
            $value = ($line -replace "^\s*$([regex]::Escape($Key))=", '').Trim().Trim('"').Trim("'")
            if ($value) { return $value }
        }
    }
    return $Default
}

function Get-Setting([string]$EnvironmentName, [string]$DotEnvName, [string]$Default) {
    $value = [Environment]::GetEnvironmentVariable($EnvironmentName)
    if (-not $value -and $DotEnvName) { $value = Get-DotEnvValue $DotEnvName $Default }
    if (-not $value) { $value = $Default }
    return $value
}

$AppPort = Get-Setting 'APP_PORT' 'APP_PORT' '8080'
$ApiUrl = Get-Setting 'API_URL' '' "http://localhost:$AppPort"
$AdminUser = Get-Setting 'APP_ADMIN_USERNAME' 'APP_ADMIN_USERNAME' 'admin'
$AdminPass = Get-Setting 'APP_ADMIN_PASSWORD' 'APP_ADMIN_PASSWORD' 'password'
$DbUser = Get-Setting 'TEST_DB_USER' 'TEST_DB_USER' 'target'
$DbPass = Get-Setting 'TEST_DB_PASSWORD' 'TEST_DB_PASSWORD' 'target_password'
$BenchDb = Get-Setting 'BENCH_DB' '' 'benchdb'
$TargetDbHost = Get-Setting 'TARGET_DB_HOST' '' 'test-db'
$TargetDbPort = [int](Get-Setting 'TARGET_DB_PORT' '' '5432')
$BenchUser = Get-Setting 'BENCH_USER' '' 'bench_runner'
$BenchPass = Get-Setting 'BENCH_PASS' '' 'bench-password-123'
$BenchGroup = Get-Setting 'BENCH_GROUP' '' 'bench-team'
$BenchDbName = Get-Setting 'BENCH_DB_NAME' '' 'bench-target'

function Write-Step([string]$Message) { Write-Host "`n==> $Message" }
function Write-Info([string]$Message) { Write-Host "  $Message" }
function Fail([string]$Message) { throw "[FAIL] $Message" }

function Invoke-DockerCompose([string[]]$Arguments) {
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $output = & docker compose @Arguments 2>&1
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($exitCode -ne 0) {
        throw "docker compose $($Arguments -join ' ') failed (exit $exitCode):`n$($output -join "`n")"
    }
    return ($output -join "`n").TrimEnd()
}

function Copy-ToTestDb([string]$LocalPath, [string]$ContainerPath) {
    Invoke-DockerCompose @('cp', $LocalPath, "test-db:$ContainerPath") | Out-Null
}

function Invoke-Psql([string]$Database, [string[]]$Arguments = @()) {
    $psqlArgs = @('exec', '-T', 'test-db', 'psql', '-U', $DbUser, '-d', $Database, '-X', '-v', 'ON_ERROR_STOP=1') + $Arguments
    return Invoke-DockerCompose $psqlArgs
}

function Invoke-PsqlBench([string[]]$Arguments = @()) {
    return Invoke-Psql $BenchDb $Arguments
}

function Save-SqlFile([string]$Name, [string]$Sql) {
    $path = Join-Path $Work $Name
    [System.IO.File]::WriteAllText($path, $Sql, [System.Text.UTF8Encoding]::new($false))
    return $path
}

function Invoke-Api([string]$Method, [string]$Path, $Session, [string]$Csrf = '', $Body = $null) {
    $headers = @{ Accept = 'application/json' }
    if ($Csrf) { $headers['X-CSRF-Token'] = $Csrf }
    $parameters = @{
        Uri = "$ApiUrl$Path"
        Method = $Method
        WebSession = $Session
        Headers = $headers
        UseBasicParsing = $true
    }
    if ($null -ne $Body) {
        $parameters.ContentType = 'application/json'
        $parameters.Body = ConvertTo-Json -InputObject $Body -Depth 20 -Compress
    }
    $statusCode = 0
    $content = ''
    try {
        $response = Invoke-WebRequest @parameters
        $statusCode = [int]$response.StatusCode
        $content = [string]$response.Content
    }
    catch {
        $httpResponse = $_.Exception.Response
        if (-not $httpResponse) { throw "Request to $ApiUrl$Path failed: $($_.Exception.Message)" }
        $statusCode = [int]$httpResponse.StatusCode
        $reader = [System.IO.StreamReader]::new($httpResponse.GetResponseStream())
        try { $content = $reader.ReadToEnd() } finally { $reader.Dispose() }
    }
    $data = $null
    if ($content) {
        try { $data = ConvertFrom-Json -InputObject $content }
        catch { $data = $content }
    }
    return [pscustomobject]@{ Status = $statusCode; Data = $data }
}

function Assert-Status($Response, [int[]]$Allowed, [string]$Description) {
    if ($Response.Status -notin $Allowed) {
        $message = if ($Response.Data -and $Response.Data.message) { $Response.Data.message } else { $Response.Data }
        Fail "$Description`: HTTP $($Response.Status) $message"
    }
}

function New-ApiSession([string]$Username, [string]$Password) {
    $session = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
    $response = Invoke-Api 'POST' '/auth/login' $session '' @{ username = $Username; password = $Password }
    Assert-Status $response @(200) "login as $Username"
    return [pscustomobject]@{ Session = $session; Csrf = [string]$response.Data.csrf_token }
}

function Format-Seconds([double]$Milliseconds) { return ('{0:N2} s' -f ($Milliseconds / 1000)) }
function Format-Integer([long]$Number) { return $Number.ToString('N0', [Globalization.CultureInfo]::InvariantCulture) }
function Get-Milliseconds([string]$Timestamp) {
    return ([DateTimeOffset]::Parse($Timestamp).ToUnixTimeMilliseconds())
}

function Submit-Draft($UserSession, [string]$Csrf, [int]$DatabaseId, [hashtable]$Payload, [string]$Label) {
    $response = Invoke-Api 'POST' "/databases/$DatabaseId/migrations/drafts" $UserSession $Csrf $Payload
    Assert-Status $response @(201) "create draft $Label"
    $draftId = $response.Data.id
    $response = Invoke-Api 'POST' "/migrations/drafts/$draftId/execute" $UserSession $Csrf
    Assert-Status $response @(202) "submit draft $Label"
    return [int]$response.Data.id
}

function Approve-Enqueue-Wait($UserSession, [string]$Csrf, [int]$ExecutionId, [string]$Label) {
    $response = Invoke-Api 'POST' "/migrations/executions/$ExecutionId/approve" $UserSession $Csrf @{ note = 'bench-batch.ps1' }
    Assert-Status $response @(200) "approve $Label"
    if ($response.Data.execution_status -ne 'approved') {
        Fail "${Label}: still '$($response.Data.execution_status)' after approval; another group with access to $BenchDbName must approve too"
    }

    $response = Invoke-Api 'POST' "/migrations/executions/$ExecutionId/enqueue" $UserSession $Csrf
    Assert-Status $response @(202) "enqueue $Label"
    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($Timeout)
    $lastStatus = ''
    while ([DateTimeOffset]::UtcNow -le $deadline) {
        $response = Invoke-Api 'GET' "/migrations/executions/$ExecutionId" $UserSession
        Assert-Status $response @(200) "poll $Label"
        $execution = $response.Data
        $lastStatus = [string]$execution.status
        if ($null -ne $execution.batch) {
            Write-Host -NoNewline "`r  ${Label}: $($lastStatus.PadRight(10)) batches=$($execution.batch_count) rows=$($execution.rows_processed)   "
        }
        if ($lastStatus -eq 'success') {
            if ($null -ne $execution.batch) { Write-Host '' }
            return $execution
        }
        if ($lastStatus -in @('failed', 'partial', 'rejected', 'compensated', 'compensation_pending', 'compensating')) {
            Write-Host ''
            Fail "${Label}: execution #$ExecutionId ended as '$lastStatus': $($execution.error_message)"
        }
        Start-Sleep -Seconds 1
    }
    Fail "${Label}: timed out after $Timeout seconds (last status '$lastStatus')"
}

function Get-HistoryDurationMs($UserSession, [int]$DatabaseId, [int]$ExecutionId) {
    $response = Invoke-Api 'GET' "/databases/$DatabaseId/migrations/history?page_size=100" $UserSession
    Assert-Status $response @(200) 'read migration history'
    $entry = @($response.Data.entries | Where-Object { $_.execution_id -eq $ExecutionId -and $_.status -eq 'success' } | Select-Object -First 1)
    if ($entry.Count -eq 0) { return 0 }
    return [long]$entry[0].duration_ms
}

function Save-Result($Results, [string]$Key, [int]$ExecutionId, $Execution, $UserSession, [int]$DatabaseId) {
    $Results["${Key}_id"] = $ExecutionId
    $Results["${Key}_sql"] = Get-HistoryDurationMs $UserSession $DatabaseId $ExecutionId
    $Results["${Key}_wall"] = (Get-Milliseconds $Execution.finished_at) - (Get-Milliseconds $Execution.started_at)
    $Results["${Key}_queue"] = (Get-Milliseconds $Execution.started_at) - (Get-Milliseconds $Execution.queued_at)
    $Results["${Key}_batches"] = [long]$Execution.batch_count
    $Results["${Key}_rows"] = [long]$Execution.rows_processed
    Write-Info "execution #$ExecutionId`: SQL $(Format-Seconds $Results["${Key}_sql"]), worker $(Format-Seconds $Results["${Key}_wall"]), queue wait $($Results["${Key}_queue"]) ms"
}

try {
    if (-not (Get-Command 'docker' -ErrorAction SilentlyContinue)) { Fail "required command 'docker' is not installed" }
    $null = Invoke-DockerCompose @('version')

    Write-Step 'Checking services'
    $anonymous = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
    $health = Invoke-Api 'GET' '/health' $anonymous
    Assert-Status $health @(200) "backend health at $ApiUrl"
    Write-Info "backend reachable at $ApiUrl"
    $ready = Invoke-DockerCompose @('exec', '-T', 'test-db', 'pg_isready', '-U', $DbUser, '-q')
    Write-Info 'test-db reachable'

    if (-not $SkipSeed) {
        Write-Step "Seeding Tracen data and $Rows training sessions into $BenchDb"
        $seedStart = [DateTimeOffset]::UtcNow
        $createSql = @'
SET client_min_messages = warning;
SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', :'bench_db') \gexec
SELECT format('CREATE DATABASE %I', :'bench_db') \gexec
'@
        $sqlPath = Save-SqlFile 'recreate-bench-db.sql' $createSql
        Copy-ToTestDb $sqlPath '/tmp/recreate-bench-db.sql'
        $null = Invoke-Psql 'postgres' @('-q', '-v', "bench_db=$BenchDb", '-f', '/tmp/recreate-bench-db.sql')

        foreach ($seed in @('seed-target.sql', 'seed-bulk.sql')) {
            $localSeed = Join-Path $PSScriptRoot $seed
            if (-not (Test-Path $localSeed)) { Fail "seed file not found: $localSeed" }
            $containerSeed = "/tmp/$seed"
            Copy-ToTestDb $localSeed $containerSeed
            $seedArgs = @('-q', '-f', $containerSeed)
            if ($seed -eq 'seed-bulk.sql') { $seedArgs += @('-v', "rows=$Rows") }
            $null = Invoke-PsqlBench $seedArgs
        }
        Write-Info "seeded in $(Format-Seconds (([DateTimeOffset]::UtcNow - $seedStart).TotalMilliseconds))"
    }

    $counts = (Invoke-PsqlBench @('-At', '-F', ' ', '-c', "SELECT count(*), count(DISTINCT trainee_id), replace(pg_size_pretty(pg_total_relation_size('public.training_sessions')), ' ', '') FROM public.training_sessions")).Trim()
    $countParts = $counts -split '\s+'
    if ($countParts.Count -lt 3) { Fail "could not read training_sessions stats: $counts" }
    $rowCount = [long]$countParts[0]
    $traineeCount = [long]$countParts[1]
    $tableSize = $countParts[2]
    $pgVersion = (Invoke-PsqlBench @('-At', '-c', 'SHOW server_version')).Trim()
    Write-Info "training_sessions=$(Format-Integer $rowCount) trainees=$(Format-Integer $traineeCount) size=$tableSize postgres=$pgVersion"

    Write-Step 'Baseline: full-table UPDATE without batching (psql, BEGIN ... ROLLBACK)'
    $baselineSql = @'
\timing on
BEGIN;
UPDATE public.training_sessions SET report_status = 'published', updated_at = now();
ROLLBACK;
'@
    $sqlPath = Save-SqlFile 'baseline.sql' $baselineSql
    Copy-ToTestDb $sqlPath '/tmp/baseline.sql'
    $baselineOutput = Invoke-PsqlBench @('-q', '-f', '/tmp/baseline.sql')
    $timings = [regex]::Matches($baselineOutput, 'Time:\s*([0-9]+(?:\.[0-9]+)?)\s*ms')
    if ($timings.Count -lt 2) { Fail "could not read UPDATE timing from psql output: $baselineOutput" }
    $baselineMs = [double]::Parse($timings[1].Groups[1].Value, [Globalization.CultureInfo]::InvariantCulture)
    Write-Info "UPDATE $(Format-Integer $rowCount) rows: $(Format-Seconds $baselineMs)"
    if ($baselineMs -le 1000) {
        Write-Warning 'Baseline is not over 1 second; increase -Rows and rerun to satisfy the benchmark requirement.'
    }

    Write-Step 'Bootstrapping bench user, group, managed database, and permission'
    $admin = New-ApiSession $AdminUser $AdminPass
    $response = Invoke-Api 'GET' '/admin/users' $admin.Session
    Assert-Status $response @(200) 'list users'
    $benchUserRecord = @($response.Data | Where-Object { $_.username -eq $BenchUser } | Select-Object -First 1)
    if ($benchUserRecord.Count -eq 0) {
        $response = Invoke-Api 'POST' '/admin/users' $admin.Session $admin.Csrf @{ username = $BenchUser; password = $BenchPass }
        Assert-Status $response @(201) "create user $BenchUser"
        $benchUserId = [int]$response.Data.id
    } else { $benchUserId = [int]$benchUserRecord[0].id }
    Write-Info "user $BenchUser (id=$benchUserId)"

    $response = Invoke-Api 'GET' '/admin/groups' $admin.Session
    Assert-Status $response @(200) 'list groups'
    $benchGroupRecord = @($response.Data | Where-Object { $_.name -eq $BenchGroup } | Select-Object -First 1)
    if ($benchGroupRecord.Count -eq 0) {
        $response = Invoke-Api 'POST' '/admin/groups' $admin.Session $admin.Csrf @{ name = $BenchGroup }
        Assert-Status $response @(201) "create group $BenchGroup"
        $benchGroupId = [int]$response.Data.id
    } else { $benchGroupId = [int]$benchGroupRecord[0].id }
    $response = Invoke-Api 'POST' "/admin/groups/$benchGroupId/members" $admin.Session $admin.Csrf @{ user_id = $benchUserId }
    Assert-Status $response @(204, 409) "add $BenchUser to $BenchGroup"
    $response = Invoke-Api 'PUT' "/admin/groups/$benchGroupId/leader" $admin.Session $admin.Csrf @{ user_id = $benchUserId }
    Assert-Status $response @(200) "set $BenchUser as leader of $BenchGroup"
    Write-Info "group $BenchGroup (id=$benchGroupId), leader $BenchUser"

    $response = Invoke-Api 'GET' '/admin/databases' $admin.Session
    Assert-Status $response @(200) 'list managed databases'
    $benchDbRecord = @($response.Data | Where-Object { $_.name -eq $BenchDbName } | Select-Object -First 1)
    if ($benchDbRecord.Count -eq 0) {
        $databasePayload = @{
            name = $BenchDbName; host = $TargetDbHost; port = $TargetDbPort
            username = $DbUser; password = $DbPass; db_name = $BenchDb; ssl_mode = 'disable'
        }
        $response = Invoke-Api 'POST' '/admin/databases' $admin.Session $admin.Csrf $databasePayload
        Assert-Status $response @(201) "register managed database $BenchDbName"
        $databaseId = [int]$response.Data.id
    } else { $databaseId = [int]$benchDbRecord[0].id }
    $response = Invoke-Api 'PUT' "/admin/groups/$benchGroupId/databases/$databaseId" $admin.Session $admin.Csrf @{ permission = 'view_edit' }
    Assert-Status $response @(200) "grant view_edit on $BenchDbName"
    Write-Info "managed database $BenchDbName (id=$databaseId) -> ${TargetDbHost}:${TargetDbPort}/$BenchDb"

    $user = New-ApiSession $BenchUser $BenchPass
    $results = @{}
    $runTag = Get-Date -Format 'yyyyMMdd-HHmmss'
    $upSql = 'UPDATE public.training_sessions SET report_status = ''published'', updated_at = now()'
    $downSql = 'UPDATE public.training_sessions SET report_status = ''draft'', updated_at = now()'

    Write-Step 'Normal migration (no batching) through queue + worker'
    $payload = @{ name = "bench ${runTag}: publish all training reports (no batching)"; sql = $upSql; down_sql = $downSql }
    $executionId = Submit-Draft $user.Session $user.Csrf $databaseId $payload 'normal migration'
    $execution = Approve-Enqueue-Wait $user.Session $user.Csrf $executionId 'normal migration'
    Save-Result $results 'normal' $executionId $execution $user.Session $databaseId
    $publishedCount = (Invoke-PsqlBench @('-At', '-c', "SELECT count(*) FILTER (WHERE report_status = 'published') FROM public.training_sessions")).Trim()
    Write-Info "published training reports: $publishedCount"

    Write-Step 'Rollback of the normal migration (restores the dataset)'
    $response = Invoke-Api 'POST' "/databases/$databaseId/migrations/rollback" $user.Session $user.Csrf
    Assert-Status $response @(202) 'submit rollback'
    $executionId = [int]$response.Data.id
    $execution = Approve-Enqueue-Wait $user.Session $user.Csrf $executionId 'rollback'
    Save-Result $results 'rollback' $executionId $execution $user.Session $databaseId
    $publishedCount = (Invoke-PsqlBench @('-At', '-c', "SELECT count(*) FILTER (WHERE report_status = 'published') FROM public.training_sessions")).Trim()
    Write-Info "published training reports: $publishedCount"

    Write-Step "Per-batch migration on the same dataset (batch size $BatchSize)"
    $batchPayload = @{
        name = "bench ${runTag}: publish training reports (${BatchSize}/batch)"
        sql = "$upSql WHERE id = ANY(`$1::bigint[])"
        down_sql = "$downSql WHERE id = ANY(`$1::bigint[])"
        batch = @{ schema = 'public'; table = 'training_sessions'; key_column = 'id'; scope_sql = ''; batch_size = $BatchSize }
    }
    $executionId = Submit-Draft $user.Session $user.Csrf $databaseId $batchPayload 'batch migration'
    $execution = Approve-Enqueue-Wait $user.Session $user.Csrf $executionId 'batch migration'
    Save-Result $results 'batch' $executionId $execution $user.Session $databaseId
    $processedAfterBatch = (Invoke-PsqlBench @('-At', '-c', "SELECT count(*) FILTER (WHERE report_status = 'published') FROM public.training_sessions")).Trim()
    Write-Info "published training reports: $processedAfterBatch"
    if ([long]$processedAfterBatch -ne $rowCount) { Fail "batch migration processed $processedAfterBatch of $rowCount rows" }

    $throughput = if ($results.batch_wall -gt 0) { [long][math]::Floor($results.batch_rows / ($results.batch_wall / 1000)) } else { 0 }

    $cpuCount = (& docker info --format '{{.NCPU}}' 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { $cpuCount = '?' }
    $memoryBytes = (& docker info --format '{{.MemTotal}}' 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -eq 0 -and $memoryBytes -match '^\d+$') { $memoryGb = [math]::Round(([double]$memoryBytes / 1GB), 1) } else { $memoryGb = '?' }

    Write-Step 'Results (Markdown, paste into README)'
    @"

Environment: Docker $cpuCount CPU / $memoryGb GB RAM, PostgreSQL $pgVersion, tabel ``public.training_sessions`` ($(Format-Integer $traineeCount) trainee Tracen) di database ``$BenchDb``.

| Skenario | Execution | Durasi SQL | Durasi worker | Batch | Row |
| --- | --- | --- | --- | --- | --- |
| UPDATE tanpa batch (psql, ``BEGIN ... ROLLBACK``) | - | $(Format-Seconds $baselineMs) | - | - | $(Format-Integer $rowCount) |
| Migration biasa (up, tanpa batch) | #$($results.normal_id) | $(Format-Seconds $results.normal_sql) | $(Format-Seconds $results.normal_wall) | - | $(Format-Integer $rowCount) |
| Rollback migration biasa (down) | #$($results.rollback_id) | $(Format-Seconds $results.rollback_sql) | $(Format-Seconds $results.rollback_wall) | - | $(Format-Integer $rowCount) |
| Migration per batch ($BatchSize row/batch) | #$($results.batch_id) | $(Format-Seconds $results.batch_sql) | $(Format-Seconds $results.batch_wall) | $(Format-Integer $results.batch_batches) | $(Format-Integer $results.batch_rows) |

- Jumlah row: $(Format-Integer $rowCount), ukuran tabel: $tableSize.
- Throughput migration per batch: $(Format-Integer $throughput) row/detik.
- Waktu tunggu queue: biasa $($results.normal_queue) ms, rollback $($results.rollback_queue) ms, batch $($results.batch_queue) ms.
- Durasi SQL diambil dari ``duration_ms`` migration history; durasi worker dari ``started_at`` sampai ``finished_at`` execution.
"@
}
finally {
    Remove-Item -Recurse -Force $Work -ErrorAction SilentlyContinue
    Pop-Location
}
