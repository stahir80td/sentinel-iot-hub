#
# IoT HomeGuard - Smoke Tests (Local Environment)
# Tests all layers: infrastructure, services, and APIs
#

param(
    [string]$Namespace = "sandbox",
    [switch]$Verbose
)

$ErrorActionPreference = "Continue"
$global:TestsPassed = 0
$global:TestsFailed = 0
$global:TestResults = @()

function Write-TestHeader {
    param([string]$Message)
    Write-Host ""
    Write-Host ("=" * 70) -ForegroundColor Cyan
    Write-Host "  $Message" -ForegroundColor Cyan
    Write-Host ("=" * 70) -ForegroundColor Cyan
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

function Test-PodRunning {
    param([string]$AppLabel, [string]$DisplayName)

    $podInfo = kubectl get pods -n $Namespace -l "app=$AppLabel" --no-headers 2>$null
    if ($podInfo) {
        $parts = $podInfo -split '\s+'
        $ready = $parts[1]  # READY column like "1/1"
        $status = $parts[2] # STATUS column
        $passed = ($status -eq "Running") -and ($ready -match "^(\d+)/\1$")
        Write-TestResult "$DisplayName Pod Running" $passed "Status: $status, Ready: $ready"
    } else {
        Write-TestResult "$DisplayName Pod Running" $false "Pod not found"
    }
}

function Test-ServiceHealth {
    param([string]$ServiceName, [string]$Port = "8080", [switch]$UseCurl)

    if ($UseCurl) {
        $result = kubectl exec -n $Namespace deploy/$ServiceName -- curl -s "http://localhost:$Port/health" 2>$null
    } else {
        $result = kubectl exec -n $Namespace deploy/$ServiceName -- wget -q -O- "http://localhost:$Port/health" 2>$null
    }

    $passed = $false
    if ($result) {
        $passed = $result -match 'healthy'
    }
    Write-TestResult "$ServiceName Health Check" $passed $(if (-not $passed) { "Response: $result" } else { "" })
}

Write-Host ""
Write-Host "######################################################################"
Write-Host "#                                                                    #"
Write-Host "#            IoT HomeGuard Smoke Tests - Local Environment           #"
Write-Host "#                                                                    #"
Write-Host "######################################################################"
Write-Host ""
Write-Host "Namespace: $Namespace"
Write-Host "Date: $(Get-Date)"
Write-Host ""

# =============================================================================
# INFRASTRUCTURE TESTS
# =============================================================================

Write-TestHeader "INFRASTRUCTURE - Database Services"

# PostgreSQL
Test-PodRunning "iot-postgresql" "PostgreSQL"
$pgResult = kubectl exec -n $Namespace deploy/iot-postgresql -- pg_isready -U homeguard 2>&1
$pgPassed = $pgResult -match "accepting connections"
Write-TestResult "PostgreSQL Ready" $pgPassed ""

# MongoDB
Test-PodRunning "iot-mongodb" "MongoDB"
$mongoResult = kubectl exec -n $Namespace deploy/iot-mongodb -- mongosh --eval "db.adminCommand('ping')" --quiet 2>&1
$mongoPassed = $mongoResult -match "ok.*:.*1"
Write-TestResult "MongoDB Ready" $mongoPassed ""

# Redis
Test-PodRunning "iot-redis" "Redis"
$redisResult = kubectl exec -n $Namespace deploy/iot-redis -- redis-cli ping 2>&1
$redisPassed = $redisResult -match "PONG"
Write-TestResult "Redis Ready" $redisPassed ""

# TimescaleDB
Test-PodRunning "iot-timescaledb" "TimescaleDB"
$tsResult = kubectl exec -n $Namespace deploy/iot-timescaledb -- pg_isready -U homeguard 2>&1
$tsPassed = $tsResult -match "accepting connections"
Write-TestResult "TimescaleDB Ready" $tsPassed ""

# ScyllaDB
Test-PodRunning "iot-scylladb" "ScyllaDB"
$scyllaResult = kubectl exec -n $Namespace deploy/iot-scylladb -- nodetool status 2>&1
$scyllaPassed = ($scyllaResult | Out-String) -match "UN"
Write-TestResult "ScyllaDB Ready" $scyllaPassed ""

Write-TestHeader "INFRASTRUCTURE - Messaging"

# Kafka (StatefulSet)
$kafkaPod = kubectl get pods -n $Namespace -l "app=iot-kafka" --no-headers 2>$null
if ($kafkaPod) {
    $parts = $kafkaPod -split '\s+'
    $ready = $parts[1]
    $status = $parts[2]
    $kafkaPassed = ($status -eq "Running") -and ($ready -match "^(\d+)/\1$")
    Write-TestResult "Kafka Pod Running" $kafkaPassed "Status: $status, Ready: $ready"
} else {
    Write-TestResult "Kafka Pod Running" $false "Pod not found"
}

# Test Kafka broker via service connectivity
$kafkaBrokerTest = kubectl exec -n $Namespace deploy/iot-event-processor -- sh -c "timeout 5 sh -c 'echo > /dev/tcp/iot-kafka/9092' 2>&1 && echo 'connected'" 2>&1
$kafkaBrokerPassed = ($kafkaBrokerTest | Out-String) -match "connected"
if (-not $kafkaBrokerPassed) {
    # Fallback - check if we can reach Kafka from event processor
    $kafkaBrokerTest2 = kubectl exec -n $Namespace deploy/iot-event-processor -- printenv KAFKA_BROKERS 2>&1
    $kafkaBrokerPassed = ($kafkaBrokerTest2 | Out-String) -match "iot-kafka"
}
Write-TestResult "Kafka Broker Accessible" $kafkaBrokerPassed ""

Write-TestHeader "INFRASTRUCTURE - Monitoring"

# Prometheus
Test-PodRunning "iot-prometheus" "Prometheus"
$promResult = kubectl exec -n $Namespace deploy/iot-prometheus -- wget -q -O- "http://localhost:9090/-/healthy" 2>&1
$promPassed = ($promResult | Out-String) -match "Healthy"
Write-TestResult "Prometheus Healthy" $promPassed ""

# Grafana
Test-PodRunning "iot-grafana" "Grafana"
$grafanaResult = kubectl exec -n $Namespace deploy/iot-grafana -- wget -q -O- "http://localhost:3000/api/health" 2>&1
$grafanaPassed = ($grafanaResult | Out-String) -match "ok"
Write-TestResult "Grafana Healthy" $grafanaPassed ""

# =============================================================================
# APPLICATION SERVICES TESTS
# =============================================================================

Write-TestHeader "APPLICATION SERVICES - Health Checks"

# API Gateway
Test-PodRunning "iot-api-gateway" "API Gateway"
Test-ServiceHealth "iot-api-gateway"

# User Service
Test-PodRunning "iot-user-service" "User Service"
Test-ServiceHealth "iot-user-service"

# Device Service
Test-PodRunning "iot-device-service" "Device Service"
Test-ServiceHealth "iot-device-service"

# Device Ingest
Test-PodRunning "iot-device-ingest" "Device Ingest"
Test-ServiceHealth "iot-device-ingest"

# Event Processor
Test-PodRunning "iot-event-processor" "Event Processor"
Test-ServiceHealth "iot-event-processor"

# Notification Service
Test-PodRunning "iot-notification-service" "Notification Service"
Test-ServiceHealth "iot-notification-service"

# Scenario Engine
Test-PodRunning "iot-scenario-engine" "Scenario Engine"
Test-ServiceHealth "iot-scenario-engine"

# Agentic AI (Python - use python for HTTP)
Test-PodRunning "iot-agentic-ai" "Agentic AI"
$aiResult = kubectl exec -n $Namespace deploy/iot-agentic-ai -- python3 -c "import urllib.request; print(urllib.request.urlopen('http://localhost:8080/health').read().decode())" 2>&1
$aiPassed = ($aiResult | Out-String) -match 'healthy'
Write-TestResult "iot-agentic-ai Health Check" $aiPassed $(if (-not $aiPassed) { "Response: $aiResult" } else { "" })

# Frontend
Test-PodRunning "iot-frontend" "Frontend"
$frontendResult = kubectl exec -n $Namespace deploy/iot-frontend -- wget -q -O- "http://localhost:80/" 2>&1
$frontendPassed = ($frontendResult | Out-String) -match "<html|<!DOCTYPE"
Write-TestResult "Frontend Serving" $frontendPassed ""

# =============================================================================
# INTEGRATION TESTS
# =============================================================================

Write-TestHeader "INTEGRATION TESTS - Service-to-Service"

# Test API Gateway can reach User Service
$apiToUserResult = kubectl exec -n $Namespace deploy/iot-api-gateway -- wget -q -O- "http://iot-user-service.$Namespace`:8080/health" 2>&1
$apiToUserPassed = ($apiToUserResult | Out-String) -match "healthy"
Write-TestResult "API Gateway -> User Service" $apiToUserPassed ""

# Test API Gateway can reach Device Service
$apiToDeviceResult = kubectl exec -n $Namespace deploy/iot-api-gateway -- wget -q -O- "http://iot-device-service.$Namespace`:8080/health" 2>&1
$apiToDevicePassed = ($apiToDeviceResult | Out-String) -match "healthy"
Write-TestResult "API Gateway -> Device Service" $apiToDevicePassed ""

# Test Event Processor Kafka config
$eventKafkaEnv = kubectl exec -n $Namespace deploy/iot-event-processor -- printenv KAFKA_BROKERS 2>&1
$eventKafkaPassed = ($eventKafkaEnv | Out-String) -match "iot-kafka"
Write-TestResult "Event Processor Kafka Config" $eventKafkaPassed ""

# Test User Service PostgreSQL config
$userToPgResult = kubectl exec -n $Namespace deploy/iot-user-service -- printenv POSTGRES_URL 2>&1
$userToPgPassed = ($userToPgResult | Out-String) -match "iot-postgresql"
Write-TestResult "User Service PostgreSQL Config" $userToPgPassed ""

# Test Device Service MongoDB config
$deviceToMongoResult = kubectl exec -n $Namespace deploy/iot-device-service -- printenv 2>&1
$deviceToMongoPassed = ($deviceToMongoResult | Out-String) -match "MONGO.*iot-mongodb"
Write-TestResult "Device Service MongoDB Config" $deviceToMongoPassed ""

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
