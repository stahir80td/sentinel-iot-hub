# HomeGuard IoT Platform - Uninstall Script
# WARNING: This will delete all HomeGuard resources including data!

param(
    [switch]$Force,
    [switch]$KeepData
)

$ErrorActionPreference = "Continue"

Write-Host @"

  HomeGuard IoT Platform - Uninstaller
  =====================================

"@ -ForegroundColor Red

if (-not $Force) {
    Write-Host "WARNING: This will delete ALL HomeGuard resources!" -ForegroundColor Yellow
    if (-not $KeepData) {
        Write-Host "WARNING: All data will be permanently deleted!" -ForegroundColor Red
    }
    Write-Host ""
    $confirm = Read-Host "Type 'DELETE' to confirm"
    if ($confirm -ne "DELETE") {
        Write-Host "Aborted." -ForegroundColor Gray
        exit 0
    }
}

Write-Host "`nUninstalling HomeGuard IoT Platform..." -ForegroundColor Yellow

# Uninstall Helm releases
Write-Host "`n--- Removing Helm Releases ---" -ForegroundColor Cyan

$helmReleases = @(
    @{Name="promtail"; Namespace="homeguard-observability"},
    @{Name="loki"; Namespace="homeguard-observability"},
    @{Name="prometheus"; Namespace="homeguard-observability"},
    @{Name="redis"; Namespace="homeguard-data"},
    @{Name="mongodb"; Namespace="homeguard-data"},
    @{Name="postgresql"; Namespace="homeguard-data"},
    @{Name="strimzi-kafka-operator"; Namespace="strimzi-system"},
    @{Name="ingress-nginx"; Namespace="ingress-nginx"}
)

foreach ($release in $helmReleases) {
    Write-Host "  Removing $($release.Name)..." -ForegroundColor Gray
    helm uninstall $release.Name -n $release.Namespace 2>$null
}

# Remove custom resources
Write-Host "`n--- Removing Custom Resources ---" -ForegroundColor Cyan

Write-Host "  Removing Kafka cluster..." -ForegroundColor Gray
kubectl delete kafka homeguard-kafka -n homeguard-messaging 2>$null

Write-Host "  Removing Kafka topics..." -ForegroundColor Gray
kubectl delete kafkatopics --all -n homeguard-messaging 2>$null

Write-Host "  Removing TimescaleDB..." -ForegroundColor Gray
kubectl delete -f $PSScriptRoot\timescaledb.yaml 2>$null

Write-Host "  Removing ScyllaDB..." -ForegroundColor Gray
kubectl delete -f $PSScriptRoot\scylladb.yaml 2>$null

Write-Host "  Removing n8n..." -ForegroundColor Gray
kubectl delete -f $PSScriptRoot\n8n.yaml 2>$null

Write-Host "  Removing secrets..." -ForegroundColor Gray
kubectl delete -f $PSScriptRoot\secrets.yaml 2>$null

# Remove PVCs if not keeping data
if (-not $KeepData) {
    Write-Host "`n--- Removing Persistent Volume Claims ---" -ForegroundColor Cyan
    $namespaces = @("homeguard-data", "homeguard-messaging", "homeguard-automation", "homeguard-observability")
    foreach ($ns in $namespaces) {
        Write-Host "  Removing PVCs in $ns..." -ForegroundColor Gray
        kubectl delete pvc --all -n $ns 2>$null
    }
}

# Remove namespaces
Write-Host "`n--- Removing Namespaces ---" -ForegroundColor Cyan

$namespaces = @(
    "homeguard-apps",
    "homeguard-data",
    "homeguard-messaging",
    "homeguard-ai",
    "homeguard-observability",
    "homeguard-automation"
)

foreach ($ns in $namespaces) {
    Write-Host "  Removing namespace $ns..." -ForegroundColor Gray
    kubectl delete namespace $ns --wait=false 2>$null
}

# Optionally remove strimzi
$removeStrimzi = Read-Host "`nRemove Strimzi operator? (y/n)"
if ($removeStrimzi -eq "y") {
    Write-Host "  Removing Strimzi..." -ForegroundColor Gray
    kubectl delete namespace strimzi-system 2>$null
}

# Clean up hosts file
Write-Host "`n--- Cleaning Hosts File ---" -ForegroundColor Cyan
$hostsPath = "C:\Windows\System32\drivers\etc\hosts"
try {
    $content = Get-Content $hostsPath | Where-Object { $_ -notmatch "homeguard|grafana\.localhost|n8n\.localhost" }
    $content | Set-Content $hostsPath
    Write-Host "  Hosts file cleaned" -ForegroundColor Green
} catch {
    Write-Host "  Could not clean hosts file (run as Administrator)" -ForegroundColor Yellow
}

Write-Host @"

============================================
 UNINSTALLATION COMPLETE
============================================

All HomeGuard IoT Platform resources have been removed.

To reinstall, run:
  .\install-all.ps1

"@ -ForegroundColor Green
