#
# IoT HomeGuard Platform - Uninstall Script (PowerShell)
# Removes the Helm chart and cleans up all resources
#

param(
    [string]$Namespace = "sandbox",
    [string]$ReleaseName = "iot-homeguard",
    [switch]$Force
)

function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[SUCCESS] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }

Write-Host ""
Write-Host "======================================================================="
Write-Host "  IoT HomeGuard Platform - Uninstall Script"
Write-Host "======================================================================="
Write-Host ""

# Confirm uninstall
if (-not $Force) {
    $confirm = Read-Host "This will remove all IoT HomeGuard resources. Continue? (y/N)"
    if ($confirm -ne "y" -and $confirm -ne "Y") {
        Write-Warn "Uninstall cancelled"
        exit 0
    }
}

Write-Info "Uninstalling Helm release..."
helm uninstall $ReleaseName -n $Namespace 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Warn "Release not found or already uninstalled"
}

Write-Info "Deleting PVCs..."
kubectl delete pvc --all -n $Namespace 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Warn "No PVCs to delete"
}

Write-Info "Waiting for pods to terminate..."
kubectl wait --for=delete pod --all -n $Namespace --timeout=60s 2>$null

Write-Info "Cleaning up any remaining resources..."
kubectl delete all -l "app.kubernetes.io/instance=$ReleaseName" -n $Namespace 2>$null

Write-Host ""
Write-Success "Uninstall complete!"
Write-Host ""
kubectl get pods -n $Namespace 2>$null
