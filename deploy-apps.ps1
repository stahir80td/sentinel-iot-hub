# HomeGuard IoT Platform - Application Deployment Script
# Deploys all HomeGuard application services to Kubernetes

param(
    [switch]$BuildFirst = $false
)

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "HomeGuard Application Deployer" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

$baseDir = $PSScriptRoot

# Check prerequisites
Write-Host "Checking prerequisites..." -ForegroundColor Yellow

# Check kubectl
try {
    kubectl version --client | Out-Null
    Write-Host "  kubectl: OK" -ForegroundColor Green
}
catch {
    Write-Host "  kubectl: NOT FOUND" -ForegroundColor Red
    exit 1
}

# Check if cluster is accessible
try {
    kubectl cluster-info | Out-Null
    Write-Host "  cluster: OK" -ForegroundColor Green
}
catch {
    Write-Host "  cluster: NOT ACCESSIBLE" -ForegroundColor Red
    Write-Host "  Please ensure Rancher Desktop is running" -ForegroundColor Yellow
    exit 1
}

# Build images if requested
if ($BuildFirst) {
    Write-Host ""
    Write-Host "Building Docker images..." -ForegroundColor Yellow
    & "$baseDir\build-images.ps1"
}

# Load environment variables
$envFile = Join-Path $baseDir ".env"
if (Test-Path $envFile) {
    Write-Host ""
    Write-Host "Loading environment variables from .env..." -ForegroundColor Yellow
    Get-Content $envFile | ForEach-Object {
        if ($_ -match "^\s*([^#][^=]+)=(.*)$") {
            $name = $matches[1].Trim()
            $value = $matches[2].Trim()
            [Environment]::SetEnvironmentVariable($name, $value, "Process")
        }
    }
    Write-Host "  Environment loaded" -ForegroundColor Green
}
else {
    Write-Host ""
    Write-Host "WARNING: .env file not found!" -ForegroundColor Yellow
    Write-Host "  Creating secrets with placeholder values" -ForegroundColor Yellow
}

# Ensure namespace exists
Write-Host ""
Write-Host "Ensuring namespace exists..." -ForegroundColor Yellow
kubectl create namespace homeguard-apps --dry-run=client -o yaml | kubectl apply -f -

# Process secrets with environment variable substitution
Write-Host ""
Write-Host "Creating secrets..." -ForegroundColor Yellow
$secretsPath = Join-Path $baseDir "deploy\k8s\apps\secrets.yaml"
$secretsContent = Get-Content $secretsPath -Raw

# Substitute environment variables
$JWT_SECRET = if ($env:JWT_SECRET) { $env:JWT_SECRET } else { "change-me-in-production-$(Get-Random)" }
$GEMINI_TEXT_API_KEY = if ($env:GEMINI_TEXT_API_KEY) { $env:GEMINI_TEXT_API_KEY } else { "your-gemini-text-api-key" }
$GEMINI_VISION_API_KEY = if ($env:GEMINI_VISION_API_KEY) { $env:GEMINI_VISION_API_KEY } else { "your-gemini-vision-api-key" }

$secretsContent = $secretsContent -replace '\$\{JWT_SECRET\}', $JWT_SECRET
$secretsContent = $secretsContent -replace '\$\{GEMINI_TEXT_API_KEY\}', $GEMINI_TEXT_API_KEY
$secretsContent = $secretsContent -replace '\$\{GEMINI_VISION_API_KEY\}', $GEMINI_VISION_API_KEY

$secretsContent | kubectl apply -f -
Write-Host "  Secrets created" -ForegroundColor Green

# Deploy applications
Write-Host ""
Write-Host "Deploying applications..." -ForegroundColor Yellow

$manifests = @(
    "api-gateway.yaml",
    "user-service.yaml",
    "device-service.yaml",
    "device-ingest.yaml",
    "event-processor.yaml",
    "notification-service.yaml",
    "scenario-engine.yaml",
    "agentic-ai.yaml",
    "frontend.yaml"
)

$manifestsDir = Join-Path $baseDir "deploy\k8s\apps"

foreach ($manifest in $manifests) {
    $manifestPath = Join-Path $manifestsDir $manifest
    if (Test-Path $manifestPath) {
        Write-Host "  Applying $manifest..." -ForegroundColor Gray
        kubectl apply -f $manifestPath
    }
}

Write-Host ""
Write-Host "Waiting for deployments to be ready..." -ForegroundColor Yellow
Start-Sleep -Seconds 5

# Check deployment status
Write-Host ""
Write-Host "Deployment Status:" -ForegroundColor Cyan
kubectl get deployments -n homeguard-apps

Write-Host ""
Write-Host "Pod Status:" -ForegroundColor Cyan
kubectl get pods -n homeguard-apps

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Deployment Complete!" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Access URLs (add to hosts file if not already done):" -ForegroundColor White
Write-Host "  Frontend:    http://homeguard.localhost" -ForegroundColor Green
Write-Host "  API Gateway: http://api.homeguard.localhost" -ForegroundColor Green
Write-Host ""
Write-Host "To watch pod status:" -ForegroundColor Gray
Write-Host "  kubectl get pods -n homeguard-apps -w" -ForegroundColor Gray
Write-Host ""
Write-Host "To view logs:" -ForegroundColor Gray
Write-Host "  kubectl logs -n homeguard-apps -l app=<service-name> -f" -ForegroundColor Gray
Write-Host ""
