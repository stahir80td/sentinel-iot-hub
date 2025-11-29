#
# IoT HomeGuard Platform - Deployment Script (PowerShell)
# Deploys the Helm chart and ensures all pods are running correctly
#

param(
    [string]$Namespace = "sandbox",
    [string]$ReleaseName = "iot-homeguard",
    [string]$ValuesFile = "values-local.yaml",
    [int]$Timeout = 300
)

$ErrorActionPreference = "Stop"

# Configuration
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ChartPath = Join-Path $ScriptDir "helm\iot-homeguard"

# Logging functions
function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[SUCCESS] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "[ERROR] $args" -ForegroundColor Red }

# Check prerequisites
function Test-Prerequisites {
    Write-Info "Checking prerequisites..."

    if (-not (Get-Command kubectl -ErrorAction SilentlyContinue)) {
        Write-Err "kubectl is not installed"
        exit 1
    }

    if (-not (Get-Command helm -ErrorAction SilentlyContinue)) {
        Write-Err "helm is not installed"
        exit 1
    }

    $clusterInfo = kubectl cluster-info 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Err "Cannot connect to Kubernetes cluster"
        exit 1
    }

    Write-Success "Prerequisites check passed"
}

# Wait for a specific pod to be ready
function Wait-ForPod {
    param(
        [string]$AppLabel,
        [int]$TimeoutSeconds = 120
    )

    Write-Info "Waiting for pod with label app=$AppLabel..."
    $startTime = Get-Date

    while ($true) {
        $elapsed = ((Get-Date) - $startTime).TotalSeconds
        if ($elapsed -ge $TimeoutSeconds) {
            Write-Err "Timeout waiting for $AppLabel (${TimeoutSeconds}s)"
            return $false
        }

        $ready = kubectl get pods -n $Namespace -l "app=$AppLabel" -o jsonpath='{.items[0].status.conditions[?(@.type==\"Ready\")].status}' 2>$null
        if ($ready -eq "True") {
            Write-Success "$AppLabel is ready"
            return $true
        }

        Start-Sleep -Seconds 5
    }
}

# Wait for StatefulSet pod to be ready
function Wait-ForStatefulSetPod {
    param(
        [string]$Name,
        [int]$TimeoutSeconds = 180
    )

    Write-Info "Waiting for StatefulSet pod $Name..."
    $startTime = Get-Date

    while ($true) {
        $elapsed = ((Get-Date) - $startTime).TotalSeconds
        if ($elapsed -ge $TimeoutSeconds) {
            Write-Err "Timeout waiting for $Name (${TimeoutSeconds}s)"
            return $false
        }

        $ready = kubectl get pod $Name -n $Namespace -o jsonpath='{.items[0].status.conditions[?(@.type==\"Ready\")].status}' 2>$null
        if ($ready -eq "True") {
            Write-Success "$Name is ready"
            return $true
        }

        Start-Sleep -Seconds 5
    }
}

# Delete pods for an app and wait for new ones
function Restart-AndWait {
    param(
        [string]$AppLabel,
        [int]$TimeoutSeconds = 120
    )

    Write-Info "Restarting $AppLabel..."
    kubectl delete pods -n $Namespace -l "app=$AppLabel" --force --grace-period=0 2>$null
    Start-Sleep -Seconds 10
    Wait-ForPod -AppLabel $AppLabel -TimeoutSeconds $TimeoutSeconds
}

# Deploy or upgrade Helm chart
function Deploy-HelmChart {
    Write-Info "Deploying Helm chart..."
    Write-Info "  Release: $ReleaseName"
    Write-Info "  Namespace: $Namespace"
    Write-Info "  Values file: $ValuesFile"

    $valuesPath = Join-Path $ChartPath $ValuesFile

    # Check if release exists (suppress error)
    $ErrorActionPreference = "SilentlyContinue"
    helm status $ReleaseName -n $Namespace 2>&1 | Out-Null
    $releaseExists = ($LASTEXITCODE -eq 0)
    $ErrorActionPreference = "Stop"

    if ($releaseExists) {
        Write-Info "Upgrading existing release..."
        helm upgrade $ReleaseName $ChartPath -f $valuesPath -n $Namespace --timeout 10m
    } else {
        Write-Info "Installing new release..."
        helm install $ReleaseName $ChartPath -f $valuesPath -n $Namespace --create-namespace --timeout 10m
    }

    if ($LASTEXITCODE -ne 0) {
        Write-Err "Helm deployment failed"
        exit 1
    }

    Write-Success "Helm chart deployed"
}

# Wait for infrastructure components
function Wait-ForInfrastructure {
    Write-Info "Waiting for infrastructure components..."

    Write-Info "=== Tier 1: Core Databases ==="
    $jobs = @()
    $jobs += Start-Job -ScriptBlock {
        param($ns)
        kubectl wait --for=condition=ready pod -l app=iot-postgresql -n $ns --timeout=120s 2>$null
    } -ArgumentList $Namespace
    $jobs += Start-Job -ScriptBlock {
        param($ns)
        kubectl wait --for=condition=ready pod -l app=iot-redis -n $ns --timeout=60s 2>$null
    } -ArgumentList $Namespace
    $jobs += Start-Job -ScriptBlock {
        param($ns)
        kubectl wait --for=condition=ready pod -l app=iot-timescaledb -n $ns --timeout=120s 2>$null
    } -ArgumentList $Namespace
    $jobs += Start-Job -ScriptBlock {
        param($ns)
        kubectl wait --for=condition=ready pod -l app=iot-scylladb -n $ns --timeout=180s 2>$null
    } -ArgumentList $Namespace

    $jobs | Wait-Job | Out-Null
    $jobs | Remove-Job

    # MongoDB needs more time
    Wait-ForPod -AppLabel "iot-mongodb" -TimeoutSeconds 180

    Write-Info "=== Tier 2: Messaging ==="
    Wait-ForStatefulSetPod -Name "iot-kafka-0" -TimeoutSeconds 180

    Write-Success "Infrastructure is ready"
}

# Wait for application components
function Wait-ForApplications {
    Write-Info "Waiting for application components..."

    Write-Info "=== Tier 3: Core Services ==="
    kubectl wait --for=condition=ready pod -l app=iot-user-service -n $Namespace --timeout=120s 2>$null
    kubectl wait --for=condition=ready pod -l app=iot-device-service -n $Namespace --timeout=120s 2>$null
    kubectl wait --for=condition=ready pod -l app=iot-notification-service -n $Namespace --timeout=120s 2>$null

    Write-Info "=== Tier 4: Event-Driven Services ==="
    Wait-ForPod -AppLabel "iot-device-ingest" -TimeoutSeconds 120
    Wait-ForPod -AppLabel "iot-event-processor" -TimeoutSeconds 120
    Wait-ForPod -AppLabel "iot-scenario-engine" -TimeoutSeconds 120

    Write-Info "=== Tier 5: API & Frontend ==="
    kubectl wait --for=condition=ready pod -l app=iot-api-gateway -n $Namespace --timeout=60s 2>$null
    kubectl wait --for=condition=ready pod -l app=iot-frontend -n $Namespace --timeout=60s 2>$null
    kubectl wait --for=condition=ready pod -l app=iot-agentic-ai -n $Namespace --timeout=60s 2>$null

    Write-Success "Applications are ready"
}

# Wait for monitoring components
function Wait-ForMonitoring {
    Write-Info "Waiting for monitoring components..."

    kubectl wait --for=condition=ready pod -l app=iot-prometheus -n $Namespace --timeout=120s 2>$null
    kubectl wait --for=condition=ready pod -l app=iot-grafana -n $Namespace --timeout=120s 2>$null
    Wait-ForPod -AppLabel "iot-n8n" -TimeoutSeconds 180

    Write-Success "Monitoring is ready"
}

# Restart Kafka-dependent services if needed
function Restart-KafkaDependents {
    Write-Info "Checking Kafka-dependent services..."

    $services = @("iot-device-ingest", "iot-event-processor", "iot-scenario-engine")

    foreach ($svc in $services) {
        $ready = kubectl get pods -n $Namespace -l "app=$svc" -o jsonpath='{.items[0].status.conditions[?(@.type==\"Ready\")].status}' 2>$null
        if ($ready -ne "True") {
            Restart-AndWait -AppLabel $svc -TimeoutSeconds 120
        }
    }

    Write-Success "Kafka-dependent services verified"
}

# Verify all pods are running
function Test-Deployment {
    Write-Info "Verifying deployment..."

    $pods = kubectl get pods -n $Namespace --no-headers 2>$null
    $notReady = $pods | Where-Object { $_ -notmatch "Running|Completed" }

    if ($notReady) {
        Write-Warn "Some pods are not ready:"
        $notReady | ForEach-Object { Write-Host $_ }
        return $false
    }

    $notFullyReady = $pods | Where-Object { $_ -match "0/[0-9]+" }
    if ($notFullyReady) {
        Write-Warn "Some pods are not fully ready:"
        $notFullyReady | ForEach-Object { Write-Host $_ }
        return $false
    }

    Write-Success "All pods are running and ready!"
    return $true
}

# Print deployment summary
function Write-Summary {
    Write-Host ""
    Write-Host "======================================================================="
    Write-Host "  IoT HomeGuard Platform - Deployment Complete"
    Write-Host "======================================================================="
    Write-Host ""
    kubectl get pods -n $Namespace -o wide
    Write-Host ""
    Write-Host "-----------------------------------------------------------------------"
    Write-Host "  Access URLs (configure hosts file for local development)"
    Write-Host "-----------------------------------------------------------------------"
    Write-Host "  Frontend:   http://homeguard.localhost"
    Write-Host "  API:        http://homeguard.localhost/api"
    Write-Host "  Grafana:    http://grafana.homeguard.localhost"
    Write-Host "  Prometheus: http://prometheus.homeguard.localhost"
    Write-Host "  n8n:        http://n8n.homeguard.localhost"
    Write-Host ""
    Write-Host "-----------------------------------------------------------------------"
    Write-Host "  Useful Commands"
    Write-Host "-----------------------------------------------------------------------"
    Write-Host "  kubectl get pods -n $Namespace"
    Write-Host "  kubectl logs -n $Namespace -l app=iot-api-gateway"
    Write-Host "  helm status $ReleaseName -n $Namespace"
    Write-Host "======================================================================="
}

# Main function
function Main {
    Write-Host ""
    Write-Host "======================================================================="
    Write-Host "  IoT HomeGuard Platform - Deployment Script"
    Write-Host "======================================================================="
    Write-Host ""

    Test-Prerequisites
    Deploy-HelmChart

    Write-Host ""
    Write-Info "Waiting for pods to start (this may take a few minutes)..."
    Start-Sleep -Seconds 30

    Wait-ForInfrastructure
    Wait-ForApplications
    Wait-ForMonitoring

    # Give everything a moment to stabilize
    Start-Sleep -Seconds 10

    # Restart any services that might have failed due to dependency timing
    Restart-KafkaDependents

    # Final wait for everything to stabilize
    Start-Sleep -Seconds 15

    # Verify deployment with retries
    $retry = 0
    $maxRetries = 3
    $success = $false

    while ($retry -lt $maxRetries) {
        if (Test-Deployment) {
            $success = $true
            break
        }

        $retry++
        if ($retry -lt $maxRetries) {
            Write-Warn "Retrying verification in 30 seconds... (attempt $($retry + 1)/$maxRetries)"
            Start-Sleep -Seconds 30
        }
    }

    Write-Summary

    if (-not $success) {
        Write-Warn "Deployment completed but some pods may need attention"
        exit 1
    }

    Write-Success "Deployment completed successfully!"
}

# Run main function
Main
