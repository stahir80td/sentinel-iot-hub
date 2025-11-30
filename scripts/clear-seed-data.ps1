#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Clears all seed data from HomeGuard IoT Platform databases.

.DESCRIPTION
    This script removes all user-created data from PostgreSQL, MongoDB, Redis,
    TimescaleDB, and ScyllaDB. Use this before re-running the seed script.

.PARAMETER Namespace
    Kubernetes namespace where services are deployed. Default: sandbox

.EXAMPLE
    .\clear-seed-data.ps1
    .\clear-seed-data.ps1 -Namespace production
#>

param(
    [string]$Namespace = "sandbox"
)

$ErrorActionPreference = "Stop"

Write-Host "=============================================" -ForegroundColor Cyan
Write-Host "  HomeGuard IoT Platform - Clear Seed Data" -ForegroundColor Cyan
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host ""

# =============================================================================
# HELPER FUNCTIONS
# =============================================================================

function Get-PodName {
    param([string]$AppLabel)
    $pod = kubectl get pods -n $Namespace -l "app=$AppLabel" -o jsonpath='{.items[0].metadata.name}' 2>$null
    if (-not $pod) {
        throw "Pod with label app=$AppLabel not found in namespace $Namespace"
    }
    return $pod
}

function Test-PodReady {
    param([string]$AppLabel)
    $status = kubectl get pods -n $Namespace -l "app=$AppLabel" -o jsonpath='{.items[0].status.phase}' 2>$null
    return $status -eq "Running"
}

# =============================================================================
# CHECK INFRASTRUCTURE
# =============================================================================

Write-Host "[1/6] Checking infrastructure pods..." -ForegroundColor Yellow

$requiredPods = @("iot-postgresql", "iot-mongodb", "iot-redis")
foreach ($pod in $requiredPods) {
    if (-not (Test-PodReady -AppLabel $pod)) {
        Write-Host "  ERROR: $pod is not running!" -ForegroundColor Red
        exit 1
    }
    Write-Host "  [OK] $pod is running" -ForegroundColor Green
}

# =============================================================================
# CLEAR POSTGRESQL
# =============================================================================

Write-Host ""
Write-Host "[2/6] Clearing PostgreSQL data..." -ForegroundColor Yellow

$pgPod = Get-PodName -AppLabel "iot-postgresql"

$clearPgSQL = @"
DELETE FROM refresh_tokens;
DELETE FROM users;
SELECT 'Users deleted: ' || COUNT(*) FROM users;
"@

$clearPgSQL | kubectl exec -i -n $Namespace $pgPod -- psql -U homeguard -d homeguard 2>$null

if ($LASTEXITCODE -eq 0) {
    Write-Host "  [OK] PostgreSQL cleared" -ForegroundColor Green
} else {
    Write-Host "  [WARN] PostgreSQL clear may have issues" -ForegroundColor Yellow
}

# =============================================================================
# CLEAR MONGODB
# =============================================================================

Write-Host ""
Write-Host "[3/6] Clearing MongoDB data..." -ForegroundColor Yellow

$mongoPod = Get-PodName -AppLabel "iot-mongodb"

$clearMongoJS = @'
db = db.getSiblingDB('homeguard');
var devCount = db.devices.countDocuments();
var cmdCount = db.device_commands.countDocuments();
db.devices.deleteMany({});
db.device_commands.deleteMany({});
print("Deleted " + devCount + " devices and " + cmdCount + " commands");
'@

$clearMongoJS | kubectl exec -i -n $Namespace $mongoPod -- mongosh --username root --password homeguard-mongo-2024 --authenticationDatabase admin --quiet 2>$null

if ($LASTEXITCODE -eq 0) {
    Write-Host "  [OK] MongoDB cleared" -ForegroundColor Green
} else {
    Write-Host "  [WARN] MongoDB clear may have issues" -ForegroundColor Yellow
}

# =============================================================================
# CLEAR REDIS
# =============================================================================

Write-Host ""
Write-Host "[4/6] Clearing Redis data..." -ForegroundColor Yellow

$redisPod = Get-PodName -AppLabel "iot-redis"

# Clear scenarios, notifications, and device status
kubectl exec -n $Namespace $redisPod -- redis-cli KEYS "scenarios:*" 2>$null | ForEach-Object {
    if ($_ -and $_ -ne "") {
        kubectl exec -n $Namespace $redisPod -- redis-cli DEL $_ 2>$null | Out-Null
    }
}
kubectl exec -n $Namespace $redisPod -- redis-cli KEYS "notifications:*" 2>$null | ForEach-Object {
    if ($_ -and $_ -ne "") {
        kubectl exec -n $Namespace $redisPod -- redis-cli DEL $_ 2>$null | Out-Null
    }
}
kubectl exec -n $Namespace $redisPod -- redis-cli KEYS "device:*" 2>$null | ForEach-Object {
    if ($_ -and $_ -ne "") {
        kubectl exec -n $Namespace $redisPod -- redis-cli DEL $_ 2>$null | Out-Null
    }
}

Write-Host "  [OK] Redis cleared" -ForegroundColor Green

# =============================================================================
# CLEAR TIMESCALEDB
# =============================================================================

Write-Host ""
Write-Host "[5/6] Clearing TimescaleDB data..." -ForegroundColor Yellow

$tsPodExists = kubectl get pods -n $Namespace -l "app=iot-timescaledb" -o name 2>$null

if ($tsPodExists) {
    $tsPod = Get-PodName -AppLabel "iot-timescaledb"

    $clearTsSQL = @"
TRUNCATE TABLE device_metrics;
SELECT 'Metrics remaining: ' || COUNT(*) FROM device_metrics;
"@

    $clearTsSQL | kubectl exec -i -n $Namespace $tsPod -- psql -U homeguard -d homeguard_analytics 2>$null

    if ($LASTEXITCODE -eq 0) {
        Write-Host "  [OK] TimescaleDB cleared" -ForegroundColor Green
    } else {
        Write-Host "  [WARN] TimescaleDB clear may have issues" -ForegroundColor Yellow
    }
} else {
    Write-Host "  [SKIP] TimescaleDB pod not found" -ForegroundColor Yellow
}

# =============================================================================
# CLEAR SCYLLADB
# =============================================================================

Write-Host ""
Write-Host "[6/6] Clearing ScyllaDB data..." -ForegroundColor Yellow

$scyllaPodExists = kubectl get pods -n $Namespace -l "app=iot-scylladb" -o name 2>$null

if ($scyllaPodExists) {
    $scyllaPod = Get-PodName -AppLabel "iot-scylladb"

    $clearScyllaCQL = @"
USE homeguard;
TRUNCATE device_events;
TRUNCATE events_by_user;
SELECT COUNT(*) FROM device_events;
"@

    $clearScyllaCQL | kubectl exec -i -n $Namespace $scyllaPod -- cqlsh 2>$null

    if ($LASTEXITCODE -eq 0) {
        Write-Host "  [OK] ScyllaDB cleared" -ForegroundColor Green
    } else {
        Write-Host "  [WARN] ScyllaDB clear may have issues" -ForegroundColor Yellow
    }
} else {
    Write-Host "  [SKIP] ScyllaDB pod not found" -ForegroundColor Yellow
}

# =============================================================================
# SUMMARY
# =============================================================================

Write-Host ""
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host "           Seed Data Cleared!" -ForegroundColor Green
Write-Host "=============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "All databases have been cleared. Run seed-data.ps1 to repopulate:" -ForegroundColor White
Write-Host "  powershell -ExecutionPolicy Bypass -File scripts/seed-data.ps1" -ForegroundColor Cyan
Write-Host ""
