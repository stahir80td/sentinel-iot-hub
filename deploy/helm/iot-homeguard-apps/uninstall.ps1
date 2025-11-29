#
# IoT HomeGuard Apps - Uninstall Script (PowerShell)
# Removes the apps Helm chart
#

param(
    [string]$Namespace = "sandbox",
    [string]$ReleaseName = "iot-homeguard-apps",
    [switch]$Force
)

function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[SUCCESS] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }

Write-Host ""
Write-Host "======================================================================="
Write-Host "  IoT HomeGuard Apps - Uninstall Script"
Write-Host "======================================================================="
Write-Host ""

# Confirm uninstall
if (-not $Force) {
    $confirm = Read-Host "This will remove all IoT HomeGuard Apps. Continue? (y/N)"
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

Write-Info "Waiting for pods to terminate..."
kubectl wait --for=delete pod -l "app.kubernetes.io/instance=$ReleaseName" -n $Namespace --timeout=60s 2>$null

Write-Host ""
Write-Success "Uninstall complete!"
Write-Host ""
Write-Info "Remaining pods in $Namespace namespace:"
kubectl get pods -n $Namespace 2>$null
