# HomeGuard IoT Platform - Docker Image Build Script
# This script builds Docker images for the HomeGuard platform
# Image naming matches Helm chart expectations: {prefix}-{service}:{tag}

param(
    [string]$Prefix = "iot",
    [string]$Tag = "latest",
    [string]$Service = "",  # Optional: build specific service (e.g., "api-gateway", "frontend")
    [switch]$Push = $false,
    [switch]$NoCache = $false,
    [switch]$List = $false
)

$ErrorActionPreference = "Stop"

# Define all services with their build paths
$allServices = @(
    @{ Name = "api-gateway"; Path = "services/go/api-gateway" },
    @{ Name = "user-service"; Path = "services/go/user-service" },
    @{ Name = "device-service"; Path = "services/go/device-service" },
    @{ Name = "device-ingest"; Path = "services/go/device-ingest" },
    @{ Name = "event-processor"; Path = "services/go/event-processor" },
    @{ Name = "notification-service"; Path = "services/go/notification-service" },
    @{ Name = "scenario-engine"; Path = "services/go/scenario-engine" },
    @{ Name = "agentic-ai"; Path = "services/python/agentic-ai" },
    @{ Name = "frontend"; Path = "frontend" }
)

# Show available services if -List flag is used
if ($List) {
    Write-Host "Available services:" -ForegroundColor Cyan
    foreach ($svc in $allServices) {
        Write-Host "  - $($svc.Name)" -ForegroundColor White
    }
    exit 0
}

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "HomeGuard Docker Image Builder" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Prefix: $Prefix" -ForegroundColor Gray
Write-Host "Tag: $Tag" -ForegroundColor Gray
if ($Service) {
    Write-Host "Building: $Service" -ForegroundColor Gray
} else {
    Write-Host "Building: ALL services" -ForegroundColor Gray
}
Write-Host ""

$baseDir = $PSScriptRoot

# Filter services if specific one is requested
$servicesToBuild = $allServices
if ($Service) {
    $servicesToBuild = $allServices | Where-Object { $_.Name -eq $Service }
    if ($servicesToBuild.Count -eq 0) {
        Write-Host "ERROR: Unknown service '$Service'" -ForegroundColor Red
        Write-Host "Use -List to see available services" -ForegroundColor Yellow
        exit 1
    }
}

$successCount = 0
$failCount = 0

foreach ($svc in $servicesToBuild) {
    # Image name format: {prefix}-{service}:{tag} (e.g., homeguard-api-gateway:latest)
    $imageName = "$Prefix-$($svc.Name):$Tag"
    $servicePath = Join-Path $baseDir $svc.Path

    Write-Host ""
    Write-Host "Building $imageName..." -ForegroundColor Yellow
    Write-Host "  Path: $servicePath"

    if (-not (Test-Path $servicePath)) {
        Write-Host "  ERROR: Path not found!" -ForegroundColor Red
        $failCount++
        continue
    }

    try {
        $buildArgs = @("-t", $imageName)
        if ($NoCache) {
            $buildArgs += "--no-cache"
        }
        $buildArgs += $servicePath

        docker build @buildArgs
        if ($LASTEXITCODE -ne 0) {
            throw "Docker build failed with exit code $LASTEXITCODE"
        }
        Write-Host "  SUCCESS: Built $imageName" -ForegroundColor Green
        $successCount++

        if ($Push) {
            Write-Host "  Pushing $imageName..." -ForegroundColor Yellow
            docker push $imageName
            if ($LASTEXITCODE -ne 0) {
                throw "Docker push failed"
            }
            Write-Host "  SUCCESS: Pushed $imageName" -ForegroundColor Green
        }
    }
    catch {
        Write-Host "  ERROR: Failed to build $imageName" -ForegroundColor Red
        Write-Host "  $($_.Exception.Message)" -ForegroundColor Red
        $failCount++
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Build Complete!" -ForegroundColor Cyan
Write-Host "  Success: $successCount" -ForegroundColor Green
if ($failCount -gt 0) {
    Write-Host "  Failed: $failCount" -ForegroundColor Red
}
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Usage Examples:" -ForegroundColor White
Write-Host "  Build all:        .\build-images.ps1" -ForegroundColor Gray
Write-Host "  Build one:        .\build-images.ps1 -Service api-gateway" -ForegroundColor Gray
Write-Host "  Build no cache:   .\build-images.ps1 -Service frontend -NoCache" -ForegroundColor Gray
Write-Host "  List services:    .\build-images.ps1 -List" -ForegroundColor Gray
Write-Host ""
Write-Host "Deploy with Helm:" -ForegroundColor White
Write-Host "  helm upgrade iot-homeguard-apps ./deploy/helm/iot-homeguard-apps -n sandbox" -ForegroundColor Gray
Write-Host ""
