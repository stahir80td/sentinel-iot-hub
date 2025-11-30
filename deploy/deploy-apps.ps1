#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Deploys HomeGuard IoT Platform applications using Helm charts.

.DESCRIPTION
    This script deploys the iot-homeguard-apps Helm chart which contains all
    application microservices (API Gateway, User Service, Device Service, etc.).

    Prerequisites:
    - Infrastructure chart (iot-homeguard) must be deployed first
    - Docker images must be built (use build-images.ps1)
    - .env file with GEMINI_TEXT_API_KEY for AI Assistant feature

.PARAMETER Namespace
    Kubernetes namespace to deploy to. Default: sandbox

.PARAMETER Environment
    Environment configuration to use (local, dv1, production). Default: local

.PARAMETER BuildFirst
    Build Docker images before deploying. Default: false

.PARAMETER SkipWait
    Skip waiting for pods to be ready. Default: false

.PARAMETER DryRun
    Show what would be deployed without actually deploying. Default: false

.PARAMETER GeminiApiKey
    Gemini API key for AI Assistant. Can also be set via .env file or GEMINI_TEXT_API_KEY env var.

.EXAMPLE
    .\deploy-apps.ps1
    Deploy apps to sandbox namespace with local values

.EXAMPLE
    .\deploy-apps.ps1 -Environment dv1 -Namespace staging
    Deploy apps to staging namespace with dv1 values

.EXAMPLE
    .\deploy-apps.ps1 -BuildFirst
    Build images first, then deploy

.EXAMPLE
    .\deploy-apps.ps1 -GeminiApiKey "your-api-key"
    Deploy with explicit Gemini API key
#>

param(
    [string]$Namespace = "sandbox",
    [ValidateSet("local", "dv1", "production")]
    [string]$Environment = "local",
    [switch]$BuildFirst = $false,
    [switch]$SkipWait = $false,
    [switch]$DryRun = $false,
    [string]$GeminiApiKey = ""
)

$ErrorActionPreference = "Stop"

# Configuration
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = Split-Path -Parent $ScriptDir
$ChartPath = Join-Path $ScriptDir "helm\iot-homeguard-apps"
$InfraChartPath = Join-Path $ScriptDir "helm\iot-homeguard"
$ReleaseName = "iot-homeguard-apps"
$InfraReleaseName = "iot-homeguard"
$ValuesFile = "values-$Environment.yaml"
$EnvFile = Join-Path $RootDir ".env"

# Load .env file if it exists
function Load-EnvFile {
    if (Test-Path $EnvFile) {
        Write-Host "  Loading .env file..." -ForegroundColor Gray
        Get-Content $EnvFile | ForEach-Object {
            if ($_ -match "^\s*([^#][^=]+)=(.*)$") {
                $name = $matches[1].Trim()
                $value = $matches[2].Trim()
                [Environment]::SetEnvironmentVariable($name, $value, "Process")
            }
        }
        return $true
    }
    return $false
}

# Colors and logging
function Write-Header {
    param([string]$Text)
    Write-Host ""
    Write-Host "═══════════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host "  $Text" -ForegroundColor Cyan
    Write-Host "═══════════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host ""
}

function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[OK] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "[ERROR] $args" -ForegroundColor Red }

# Check prerequisites
function Test-Prerequisites {
    Write-Info "Checking prerequisites..."

    # Check kubectl
    if (-not (Get-Command kubectl -ErrorAction SilentlyContinue)) {
        Write-Err "kubectl is not installed"
        Write-Host "  Install: https://kubernetes.io/docs/tasks/tools/" -ForegroundColor Gray
        return $false
    }
    Write-Host "  kubectl: " -NoNewline; Write-Host "OK" -ForegroundColor Green

    # Check helm
    if (-not (Get-Command helm -ErrorAction SilentlyContinue)) {
        Write-Err "helm is not installed"
        Write-Host "  Install: https://helm.sh/docs/intro/install/" -ForegroundColor Gray
        return $false
    }
    Write-Host "  helm: " -NoNewline; Write-Host "OK" -ForegroundColor Green

    # Check cluster connectivity
    $clusterInfo = kubectl cluster-info 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Err "Cannot connect to Kubernetes cluster"
        Write-Host "  Ensure Rancher Desktop or Docker Desktop is running" -ForegroundColor Gray
        return $false
    }
    Write-Host "  cluster: " -NoNewline; Write-Host "OK" -ForegroundColor Green

    # Check values file exists
    $valuesPath = Join-Path $ChartPath $ValuesFile
    if (-not (Test-Path $valuesPath)) {
        Write-Err "Values file not found: $valuesPath"
        return $false
    }
    Write-Host "  values file: " -NoNewline; Write-Host "OK" -ForegroundColor Green

    return $true
}

# Check if infrastructure is deployed
function Test-Infrastructure {
    Write-Info "Checking infrastructure deployment..."

    # Check if infra release exists
    $ErrorActionPreference = "SilentlyContinue"
    helm status $InfraReleaseName -n $Namespace 2>&1 | Out-Null
    $infraExists = ($LASTEXITCODE -eq 0)
    $ErrorActionPreference = "Stop"

    if (-not $infraExists) {
        Write-Err "Infrastructure chart ($InfraReleaseName) is not deployed"
        Write-Host ""
        Write-Host "  Deploy infrastructure first:" -ForegroundColor Yellow
        Write-Host "    cd $ScriptDir" -ForegroundColor Gray
        Write-Host "    .\deploy.ps1 -Namespace $Namespace" -ForegroundColor Gray
        Write-Host ""
        return $false
    }

    # Check key infrastructure pods
    $infraPods = @("iot-postgresql", "iot-mongodb", "iot-redis", "iot-kafka")
    $allReady = $true

    foreach ($pod in $infraPods) {
        $phase = kubectl get pods -n $Namespace -l "app=$pod" -o jsonpath='{.items[0].status.phase}' 2>$null
        if ($phase -ne "Running") {
            Write-Warn "$pod is not running (status: $phase)"
            $allReady = $false
        }
    }

    if (-not $allReady) {
        Write-Warn "Some infrastructure pods are not ready"
        Write-Host "  Wait for infrastructure to be ready or run:" -ForegroundColor Gray
        Write-Host "    kubectl get pods -n $Namespace -w" -ForegroundColor Gray
        return $false
    }

    Write-Success "Infrastructure is ready"
    return $true
}

# Build Docker images
function Build-Images {
    Write-Info "Building Docker images..."

    $buildScript = Join-Path $RootDir "build-images.ps1"
    if (Test-Path $buildScript) {
        & $buildScript
        if ($LASTEXITCODE -ne 0) {
            Write-Err "Image build failed"
            return $false
        }
        Write-Success "Images built successfully"
    } else {
        Write-Warn "Build script not found: $buildScript"
        Write-Host "  Assuming images are already available" -ForegroundColor Gray
    }

    return $true
}

# Deploy Helm chart
function Deploy-HelmChart {
    param([string]$ApiKey)

    Write-Info "Deploying applications..."
    Write-Host "  Release:    $ReleaseName" -ForegroundColor Gray
    Write-Host "  Namespace:  $Namespace" -ForegroundColor Gray
    Write-Host "  Values:     $ValuesFile" -ForegroundColor Gray
    Write-Host ""

    $valuesPath = Join-Path $ChartPath $ValuesFile

    # Check if release exists
    $ErrorActionPreference = "SilentlyContinue"
    helm status $ReleaseName -n $Namespace 2>&1 | Out-Null
    $releaseExists = ($LASTEXITCODE -eq 0)
    $ErrorActionPreference = "Stop"

    $helmArgs = @()
    if ($DryRun) {
        $helmArgs += "--dry-run"
        Write-Warn "DRY RUN - No changes will be made"
    }

    # Add Gemini API key if provided
    if ($ApiKey) {
        $helmArgs += "--set"
        $helmArgs += "agenticAi.geminiApiKey=$ApiKey"
        Write-Host "  Gemini API: " -NoNewline; Write-Host "configured" -ForegroundColor Green
    } else {
        Write-Warn "Gemini API key not set - AI Assistant will not work"
        Write-Host "  Set via: -GeminiApiKey, .env file, or GEMINI_TEXT_API_KEY env var" -ForegroundColor Gray
    }

    if ($releaseExists) {
        Write-Info "Upgrading existing release..."
        helm upgrade $ReleaseName $ChartPath -f $valuesPath -n $Namespace --timeout 10m @helmArgs
    } else {
        Write-Info "Installing new release..."
        helm install $ReleaseName $ChartPath -f $valuesPath -n $Namespace --timeout 10m @helmArgs
    }

    if ($LASTEXITCODE -ne 0) {
        Write-Err "Helm deployment failed"
        return $false
    }

    Write-Success "Helm chart deployed"
    return $true
}

# Wait for pods to be ready
function Wait-ForPods {
    Write-Info "Waiting for application pods to be ready..."

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

    $timeout = 180
    $allReady = $true

    foreach ($app in $appLabels) {
        $startTime = Get-Date
        $ready = $false

        while (-not $ready) {
            $elapsed = ((Get-Date) - $startTime).TotalSeconds
            if ($elapsed -ge $timeout) {
                Write-Warn "Timeout waiting for $app"
                $allReady = $false
                break
            }

            $status = kubectl get pods -n $Namespace -l "app=$app" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>$null
            if ($status -eq "True") {
                Write-Host "  $app" -NoNewline; Write-Host " ready" -ForegroundColor Green
                $ready = $true
            } else {
                Start-Sleep -Seconds 5
            }
        }
    }

    return $allReady
}

# Print deployment summary
function Write-Summary {
    Write-Host ""
    Write-Host "═══════════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host "  Deployment Summary" -ForegroundColor Cyan
    Write-Host "═══════════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host ""

    # Show pod status
    Write-Host "Application Pods:" -ForegroundColor Yellow
    kubectl get pods -n $Namespace -l "app.kubernetes.io/instance=$ReleaseName" --no-headers 2>$null | ForEach-Object {
        Write-Host "  $_"
    }

    # If no pods found with that label, try by prefix
    $pods = kubectl get pods -n $Namespace --no-headers 2>$null | Where-Object { $_ -match "^iot-(api|user|device|event|notification|scenario|agentic|frontend)" }
    if ($pods) {
        $pods | ForEach-Object { Write-Host "  $_" }
    }

    Write-Host ""
    Write-Host "───────────────────────────────────────────────────────────────────────" -ForegroundColor Gray
    Write-Host "  Access URLs" -ForegroundColor Yellow
    Write-Host "───────────────────────────────────────────────────────────────────────" -ForegroundColor Gray
    Write-Host "  Frontend:     http://homeguard.localhost" -ForegroundColor White
    Write-Host "  API:          http://homeguard.localhost/api" -ForegroundColor White
    Write-Host "  Grafana:      http://grafana.homeguard.localhost" -ForegroundColor Gray
    Write-Host "  Prometheus:   http://prometheus.homeguard.localhost" -ForegroundColor Gray
    Write-Host ""
    Write-Host "───────────────────────────────────────────────────────────────────────" -ForegroundColor Gray
    Write-Host "  Useful Commands" -ForegroundColor Yellow
    Write-Host "───────────────────────────────────────────────────────────────────────" -ForegroundColor Gray
    Write-Host "  kubectl get pods -n $Namespace" -ForegroundColor Gray
    Write-Host "  kubectl logs -n $Namespace -l app=iot-api-gateway -f" -ForegroundColor Gray
    Write-Host "  helm status $ReleaseName -n $Namespace" -ForegroundColor Gray
    Write-Host "  helm uninstall $ReleaseName -n $Namespace" -ForegroundColor Gray
    Write-Host ""
    Write-Host "───────────────────────────────────────────────────────────────────────" -ForegroundColor Gray
    Write-Host "  Seed Data" -ForegroundColor Yellow
    Write-Host "───────────────────────────────────────────────────────────────────────" -ForegroundColor Gray
    Write-Host "  Run after deployment to create demo users and devices:" -ForegroundColor Gray
    Write-Host "  powershell -ExecutionPolicy Bypass -File scripts/seed-data.ps1" -ForegroundColor White
    Write-Host ""
    Write-Host "═══════════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
}

# Main function
function Main {
    Write-Header "HomeGuard IoT Platform - Application Deployment"

    Write-Host "Configuration:" -ForegroundColor Yellow
    Write-Host "  Environment:  $Environment" -ForegroundColor Gray
    Write-Host "  Namespace:    $Namespace" -ForegroundColor Gray
    Write-Host "  Values File:  $ValuesFile" -ForegroundColor Gray
    Write-Host ""

    # Load .env file
    Load-EnvFile | Out-Null

    # Determine Gemini API key (priority: parameter > env var > .env file)
    $apiKey = $GeminiApiKey
    if (-not $apiKey) {
        $apiKey = $env:GEMINI_TEXT_API_KEY
    }

    # Prerequisites
    if (-not (Test-Prerequisites)) {
        exit 1
    }

    # Check infrastructure
    if (-not (Test-Infrastructure)) {
        exit 1
    }

    # Build images if requested
    if ($BuildFirst) {
        if (-not (Build-Images)) {
            exit 1
        }
    }

    # Deploy
    if (-not (Deploy-HelmChart -ApiKey $apiKey)) {
        exit 1
    }

    # Wait for pods unless skipped or dry run
    if (-not $SkipWait -and -not $DryRun) {
        Write-Host ""
        Start-Sleep -Seconds 10  # Give pods time to start

        if (-not (Wait-ForPods)) {
            Write-Warn "Some pods may need attention"
        }
    }

    # Summary
    if (-not $DryRun) {
        Write-Summary
    }

    Write-Success "Deployment complete!"
}

# Run
Main
