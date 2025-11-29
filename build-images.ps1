# HomeGuard IoT Platform - Docker Image Build Script
# This script builds all Docker images for the HomeGuard platform

param(
    [string]$Registry = "homeguard",
    [string]$Tag = "latest",
    [switch]$Push = $false
)

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "HomeGuard Docker Image Builder" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Define services to build
$services = @(
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

$baseDir = $PSScriptRoot

foreach ($service in $services) {
    $imageName = "$Registry/$($service.Name):$Tag"
    $servicePath = Join-Path $baseDir $service.Path

    Write-Host ""
    Write-Host "Building $imageName..." -ForegroundColor Yellow
    Write-Host "  Path: $servicePath"

    if (-not (Test-Path $servicePath)) {
        Write-Host "  ERROR: Path not found!" -ForegroundColor Red
        continue
    }

    try {
        docker build -t $imageName $servicePath
        Write-Host "  SUCCESS: Built $imageName" -ForegroundColor Green

        if ($Push) {
            Write-Host "  Pushing $imageName..." -ForegroundColor Yellow
            docker push $imageName
            Write-Host "  SUCCESS: Pushed $imageName" -ForegroundColor Green
        }
    }
    catch {
        Write-Host "  ERROR: Failed to build $imageName" -ForegroundColor Red
        Write-Host "  $($_.Exception.Message)" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Build Complete!" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "To deploy to Kubernetes:" -ForegroundColor White
Write-Host "  1. Load images into K3s (if using local registry):" -ForegroundColor Gray
Write-Host "     For each image: docker save <image> | nerdctl -n k8s.io load" -ForegroundColor Gray
Write-Host ""
Write-Host "  2. Apply Kubernetes manifests:" -ForegroundColor Gray
Write-Host "     kubectl apply -k deploy/k8s/apps/" -ForegroundColor Gray
Write-Host ""
