#
# IoT HomeGuard Apps - Deployment Script (PowerShell)
# Deploys the apps Helm chart
#

param(
    [string]$Namespace = "sandbox",
    [string]$ReleaseName = "iot-homeguard-apps",
    [string]$ValuesFile = "values-local.yaml",
    [int]$Timeout = 300
)

$ErrorActionPreference = "Stop"

# Configuration
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ChartPath = $ScriptDir

# Logging functions
function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[SUCCESS] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "[ERROR] $args" -ForegroundColor Red }

Write-Host ""
Write-Host "======================================================================="
Write-Host "  IoT HomeGuard Apps - Deployment Script"
Write-Host "======================================================================="
Write-Host ""

# Check prerequisites
Write-Info "Checking prerequisites..."
if (-not (Get-Command kubectl -ErrorAction SilentlyContinue)) {
    Write-Err "kubectl is not installed"
    exit 1
}
if (-not (Get-Command helm -ErrorAction SilentlyContinue)) {
    Write-Err "helm is not installed"
    exit 1
}
Write-Success "Prerequisites check passed"

# Check if infra chart is deployed
Write-Info "Checking if infrastructure is deployed..."
$infraPods = kubectl get pods -n $Namespace -l "app=iot-postgresql" --no-headers 2>$null
if (-not $infraPods) {
    Write-Err "Infrastructure not deployed. Please deploy iot-homeguard (infra) chart first."
    Write-Err "Run: helm install iot-homeguard ../iot-homeguard -f ../iot-homeguard/values-local.yaml -n $Namespace"
    exit 1
}
Write-Success "Infrastructure is deployed"

# Deploy Helm chart
Write-Info "Deploying Helm chart..."
Write-Info "  Release: $ReleaseName"
Write-Info "  Namespace: $Namespace"
Write-Info "  Values file: $ValuesFile"

$valuesPath = Join-Path $ChartPath $ValuesFile

# Check if release exists
$ErrorActionPreference = "SilentlyContinue"
helm status $ReleaseName -n $Namespace 2>&1 | Out-Null
$releaseExists = ($LASTEXITCODE -eq 0)
$ErrorActionPreference = "Stop"

if ($releaseExists) {
    Write-Info "Upgrading existing release..."
    helm upgrade $ReleaseName $ChartPath -f $valuesPath -n $Namespace --timeout 10m
} else {
    Write-Info "Installing new release..."
    helm install $ReleaseName $ChartPath -f $valuesPath -n $Namespace --timeout 10m
}

if ($LASTEXITCODE -ne 0) {
    Write-Err "Helm deployment failed"
    exit 1
}

Write-Success "Helm chart deployed"

# Wait for pods to start
Write-Info "Waiting for pods to start..."
Start-Sleep -Seconds 15

# Wait for app pods
$appLabels = @(
    "iot-api-gateway",
    "iot-user-service",
    "iot-device-service",
    "iot-device-ingest",
    "iot-event-processor",
    "iot-notification-service",
    "iot-scenario-engine",
    "iot-agentic-ai",
    "iot-frontend"
)

foreach ($app in $appLabels) {
    Write-Info "Waiting for $app..."
    $timeout = 120
    $startTime = Get-Date

    while ($true) {
        $elapsed = ((Get-Date) - $startTime).TotalSeconds
        if ($elapsed -ge $timeout) {
            Write-Warn "Timeout waiting for $app (${timeout}s)"
            break
        }

        $ready = kubectl get pods -n $Namespace -l "app=$app" -o jsonpath='{.items[0].status.conditions[?(@.type==\"Ready\")].status}' 2>$null
        if ($ready -eq "True") {
            Write-Success "$app is ready"
            break
        }

        Start-Sleep -Seconds 5
    }
}

# Verify deployment
Write-Info "Verifying deployment..."
$pods = kubectl get pods -n $Namespace --no-headers 2>$null | Where-Object { $_ -match "^iot-" }
$notReady = $pods | Where-Object { $_ -notmatch "Running|Completed" }

if ($notReady) {
    Write-Warn "Some pods are not ready:"
    $notReady | ForEach-Object { Write-Host $_ }
}

# Print summary
Write-Host ""
Write-Host "======================================================================="
Write-Host "  IoT HomeGuard Apps - Deployment Complete"
Write-Host "======================================================================="
Write-Host ""
kubectl get pods -n $Namespace | Where-Object { $_ -match "^iot-|^NAME" }
Write-Host ""
Write-Host "-----------------------------------------------------------------------"
Write-Host "  Useful Commands"
Write-Host "-----------------------------------------------------------------------"
Write-Host "  kubectl get pods -n $Namespace"
Write-Host "  kubectl logs -n $Namespace -l app=iot-api-gateway"
Write-Host "  helm status $ReleaseName -n $Namespace"
Write-Host "======================================================================="
Write-Host ""

Write-Success "Deployment completed!"
