#
# IoT HomeGuard - Smoke Tests (DV1 Environment)
# Tests all layers: infrastructure, services, and APIs via external URLs
#

param(
    [string]$Namespace = "sandbox",
    [string]$BaseUrl = "https://homeguard.dv1.example.com",
    [string]$ApiUrl = "https://homeguard.dv1.example.com/api",
    [switch]$Verbose,
    [switch]$SkipKubectl
)

$ErrorActionPreference = "Continue"
$global:TestsPassed = 0
$global:TestsFailed = 0
$global:TestResults = @()

function Write-TestHeader {
    param([string]$Message)
    Write-Host ""
    Write-Host "=" * 70 -ForegroundColor Cyan
    Write-Host "  $Message" -ForegroundColor Cyan
    Write-Host "=" * 70 -ForegroundColor Cyan
}

function Write-TestResult {
    param([string]$TestName, [bool]$Passed, [string]$Details = "")

    $global:TestResults += @{
        Name = $TestName
        Passed = $Passed
        Details = $Details
    }

    if ($Passed) {
        $global:TestsPassed++
        Write-Host "[PASS] $TestName" -ForegroundColor Green
    } else {
        $global:TestsFailed++
        Write-Host "[FAIL] $TestName" -ForegroundColor Red
        if ($Details) {
            Write-Host "       $Details" -ForegroundColor Yellow
        }
    }
}

function Test-HttpEndpoint {
    param(
        [string]$Url,
        [string]$TestName,
        [string]$ExpectedContent = $null,
        [int]$ExpectedStatus = 200
    )

    try {
        $response = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
        $passed = $response.StatusCode -eq $ExpectedStatus

        if ($passed -and $ExpectedContent) {
            $passed = $response.Content -match $ExpectedContent
        }

        Write-TestResult $TestName $passed "Status: $($response.StatusCode)"
    } catch {
        $statusCode = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
        Write-TestResult $TestName $false "Error: $($_.Exception.Message), Status: $statusCode"
    }
}

function Test-PodRunning {
    param([string]$AppLabel, [string]$DisplayName)

    if ($SkipKubectl) {
        Write-Host "[SKIP] $DisplayName Pod Running (kubectl skipped)" -ForegroundColor Yellow
        return
    }

    $pod = kubectl get pods -n $Namespace -l "app=$AppLabel" -o jsonpath='{.items[0].status.phase}' 2>$null
    $ready = kubectl get pods -n $Namespace -l "app=$AppLabel" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>$null

    $passed = ($pod -eq "Running") -and ($ready -eq "True")
    Write-TestResult "$DisplayName Pod Running" $passed "Status: $pod, Ready: $ready"
}

Write-Host ""
Write-Host "######################################################################"
Write-Host "#                                                                    #"
Write-Host "#            IoT HomeGuard Smoke Tests - DV1 Environment             #"
Write-Host "#                                                                    #"
Write-Host "######################################################################"
Write-Host ""
Write-Host "Namespace:  $Namespace"
Write-Host "Base URL:   $BaseUrl"
Write-Host "API URL:    $ApiUrl"
Write-Host "Date:       $(Get-Date)"
Write-Host ""

# =============================================================================
# INFRASTRUCTURE TESTS (via kubectl if available)
# =============================================================================

if (-not $SkipKubectl) {
    Write-TestHeader "INFRASTRUCTURE - Pod Status"

    # Database Pods
    Test-PodRunning "iot-postgresql" "PostgreSQL"
    Test-PodRunning "iot-mongodb" "MongoDB"
    Test-PodRunning "iot-redis" "Redis"
    Test-PodRunning "iot-timescaledb" "TimescaleDB"
    Test-PodRunning "iot-scylladb" "ScyllaDB"

    # Messaging
    $kafkaReady = kubectl get pods -n $Namespace -l "app=iot-kafka" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>$null
    Write-TestResult "Kafka Pod Running" ($kafkaReady -eq "True") ""

    # Monitoring
    Test-PodRunning "iot-prometheus" "Prometheus"
    Test-PodRunning "iot-grafana" "Grafana"

    Write-TestHeader "APPLICATION - Pod Status"

    Test-PodRunning "iot-api-gateway" "API Gateway"
    Test-PodRunning "iot-user-service" "User Service"
    Test-PodRunning "iot-device-service" "Device Service"
    Test-PodRunning "iot-device-ingest" "Device Ingest"
    Test-PodRunning "iot-event-processor" "Event Processor"
    Test-PodRunning "iot-notification-service" "Notification Service"
    Test-PodRunning "iot-scenario-engine" "Scenario Engine"
    Test-PodRunning "iot-agentic-ai" "Agentic AI"
    Test-PodRunning "iot-frontend" "Frontend"
}

# =============================================================================
# EXTERNAL ENDPOINT TESTS
# =============================================================================

Write-TestHeader "EXTERNAL ENDPOINTS - Frontend"

Test-HttpEndpoint -Url $BaseUrl -TestName "Frontend Home Page" -ExpectedContent "html"

Write-TestHeader "EXTERNAL ENDPOINTS - API Health"

Test-HttpEndpoint -Url "$ApiUrl/health" -TestName "API Gateway Health" -ExpectedContent "healthy"

# These may require authentication, so we test they respond (even with 401)
Write-Host ""
Write-Host "Testing API endpoints (may require auth)..." -ForegroundColor Yellow

# Test API endpoints exist
try {
    $response = Invoke-WebRequest -Uri "$ApiUrl/users/health" -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
    Write-TestResult "User Service via API Gateway" $true ""
} catch {
    $statusCode = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
    # 401/403 means the endpoint exists but requires auth
    $passed = $statusCode -in @(200, 401, 403)
    Write-TestResult "User Service via API Gateway" $passed "Status: $statusCode"
}

try {
    $response = Invoke-WebRequest -Uri "$ApiUrl/devices/health" -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
    Write-TestResult "Device Service via API Gateway" $true ""
} catch {
    $statusCode = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
    $passed = $statusCode -in @(200, 401, 403)
    Write-TestResult "Device Service via API Gateway" $passed "Status: $statusCode"
}

# =============================================================================
# API FUNCTIONALITY TESTS
# =============================================================================

Write-TestHeader "API FUNCTIONALITY - Auth Flow"

# Test registration endpoint accepts POST
try {
    $body = @{
        email = "smoke-test-$(Get-Random)@example.com"
        password = "SmokeTest123!"
        name = "Smoke Test User"
    } | ConvertTo-Json

    $response = Invoke-WebRequest -Uri "$ApiUrl/auth/register" `
        -Method POST `
        -Body $body `
        -ContentType "application/json" `
        -UseBasicParsing `
        -TimeoutSec 30 `
        -ErrorAction Stop

    Write-TestResult "User Registration Endpoint" $true ""
} catch {
    $statusCode = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
    # 400 might mean validation error (email format, etc) - endpoint works
    # 409 means user already exists - endpoint works
    $passed = $statusCode -in @(200, 201, 400, 409)
    Write-TestResult "User Registration Endpoint" $passed "Status: $statusCode"
}

# Test login endpoint
try {
    $body = @{
        email = "admin@homeguard.local"
        password = "admin123"
    } | ConvertTo-Json

    $response = Invoke-WebRequest -Uri "$ApiUrl/auth/login" `
        -Method POST `
        -Body $body `
        -ContentType "application/json" `
        -UseBasicParsing `
        -TimeoutSec 30 `
        -ErrorAction Stop

    Write-TestResult "User Login Endpoint" $true ""
} catch {
    $statusCode = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
    # 401 means invalid credentials but endpoint works
    $passed = $statusCode -in @(200, 401)
    Write-TestResult "User Login Endpoint" $passed "Status: $statusCode"
}

# =============================================================================
# SUMMARY
# =============================================================================

Write-Host ""
Write-Host "######################################################################"
Write-Host "#                         TEST SUMMARY                               #"
Write-Host "######################################################################"
Write-Host ""
Write-Host "Total Tests:  $($global:TestsPassed + $global:TestsFailed)"
Write-Host "Passed:       $($global:TestsPassed)" -ForegroundColor Green
Write-Host "Failed:       $($global:TestsFailed)" -ForegroundColor $(if ($global:TestsFailed -gt 0) { "Red" } else { "Green" })
Write-Host ""

if ($global:TestsFailed -gt 0) {
    Write-Host "Failed Tests:" -ForegroundColor Red
    $global:TestResults | Where-Object { -not $_.Passed } | ForEach-Object {
        Write-Host "  - $($_.Name)" -ForegroundColor Red
    }
    Write-Host ""
    exit 1
} else {
    Write-Host "All tests passed!" -ForegroundColor Green
    Write-Host ""
    exit 0
}
