# HomeGuard IoT Platform - Complete Infrastructure Installation Script
# Run this script as Administrator in PowerShell

param(
    [switch]$SkipDatabases,
    [switch]$SkipKafka,
    [switch]$SkipObservability,
    [switch]$SkipN8n,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

# Colors for output
function Write-Step { param($Message) Write-Host "`n=== $Message ===" -ForegroundColor Cyan }
function Write-Success { param($Message) Write-Host "[OK] $Message" -ForegroundColor Green }
function Write-Warn { param($Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-Err { param($Message) Write-Host "[ERROR] $Message" -ForegroundColor Red }

# Banner
Write-Host @"

  _   _                       ____                     _
 | | | | ___  _ __ ___   ___ / ___|_   _  __ _ _ __ __| |
 | |_| |/ _ \| '_ ` _ \ / _ \ |  _| | | |/ _` | '__/ _` |
 |  _  | (_) | | | | | |  __/ |_| | |_| | (_| | | | (_| |
 |_| |_|\___/|_| |_| |_|\___|\____|\__,_|\__,_|_|  \__,_|

 IoT Platform - Infrastructure Installer (POC Mode)

"@ -ForegroundColor Magenta

# Check prerequisites
Write-Step "Checking Prerequisites"

# Check kubectl
try {
    $kubectlVersion = kubectl version --client --short 2>$null
    if (-not $kubectlVersion) {
        $kubectlVersion = kubectl version --client -o json 2>$null | ConvertFrom-Json | Select-Object -ExpandProperty clientVersion | Select-Object -ExpandProperty gitVersion
    }
    Write-Success "kubectl found: $kubectlVersion"
} catch {
    Write-Err "kubectl not found. Please install Rancher Desktop or kubectl."
    exit 1
}

# Check helm
try {
    $helmVersion = helm version --short 2>$null
    Write-Success "Helm found: $helmVersion"
} catch {
    Write-Err "Helm not found. Please install Rancher Desktop or Helm."
    exit 1
}

# Check cluster connection
try {
    $context = kubectl config current-context 2>$null
    $nodeStatus = kubectl get nodes --no-headers 2>$null
    if (-not $nodeStatus) {
        throw "No nodes found"
    }
    Write-Success "Connected to Kubernetes cluster: $context"
} catch {
    Write-Err "Cannot connect to Kubernetes cluster. Please ensure Rancher Desktop is running."
    Write-Host "  Start Rancher Desktop and wait for Kubernetes to be ready." -ForegroundColor Yellow
    exit 1
}

# Check if using K3s (Rancher Desktop)
$ingressClass = kubectl get ingressclass -o name 2>$null
$useTraefik = $ingressClass -match "traefik"
if ($useTraefik) {
    Write-Success "Detected K3s with Traefik ingress (Rancher Desktop)"
} else {
    Write-Warn "Traefik not detected - will install NGINX ingress"
}

if ($DryRun) {
    Write-Warn "DRY RUN MODE - No changes will be made"
}

# Add Helm repositories
Write-Step "Adding Helm Repositories"

$repos = @(
    @{Name="bitnami"; URL="https://charts.bitnami.com/bitnami"},
    @{Name="prometheus-community"; URL="https://prometheus-community.github.io/helm-charts"},
    @{Name="strimzi"; URL="https://strimzi.io/charts/"}
)

# Only add nginx if not using Traefik
if (-not $useTraefik) {
    $repos += @{Name="ingress-nginx"; URL="https://kubernetes.github.io/ingress-nginx"}
}

foreach ($repo in $repos) {
    if (-not $DryRun) {
        helm repo add $repo.Name $repo.URL 2>$null
    }
    Write-Success "Added repo: $($repo.Name)"
}

if (-not $DryRun) {
    helm repo update 2>$null
}
Write-Success "Helm repos updated"

# Create namespaces
Write-Step "Creating Namespaces"

$namespaces = @(
    "homeguard-apps",
    "homeguard-data",
    "homeguard-messaging",
    "homeguard-ai",
    "homeguard-observability",
    "homeguard-automation",
    "strimzi-system"
)

foreach ($ns in $namespaces) {
    if (-not $DryRun) {
        kubectl create namespace $ns --dry-run=client -o yaml 2>$null | kubectl apply -f - 2>$null
    }
    Write-Success "Namespace: $ns"
}

# Install Ingress Controller (only if not using Traefik)
if (-not $useTraefik) {
    Write-Step "Installing NGINX Ingress Controller"
    if (-not $DryRun) {
        kubectl create namespace ingress-nginx --dry-run=client -o yaml 2>$null | kubectl apply -f - 2>$null
        helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx `
            --namespace ingress-nginx `
            --set controller.service.type=LoadBalancer `
            --set controller.watchIngressWithoutClass=true `
            --wait --timeout 5m
    }
    Write-Success "NGINX Ingress Controller installed"
} else {
    Write-Success "Using existing Traefik ingress from K3s"
}

# Install Databases
if (-not $SkipDatabases) {
    Write-Step "Installing Databases"

    # PostgreSQL
    Write-Host "  Installing PostgreSQL..." -ForegroundColor Gray
    if (-not $DryRun) {
        helm upgrade --install postgresql bitnami/postgresql `
            --namespace homeguard-data `
            --set auth.postgresPassword=homeguard-postgres-2024 `
            --set auth.database=homeguard `
            --set primary.persistence.size=1Gi `
            --set primary.resources.requests.cpu=50m `
            --set primary.resources.requests.memory=128Mi `
            --set primary.resources.limits.cpu=250m `
            --set primary.resources.limits.memory=256Mi `
            --set metrics.enabled=false `
            --wait --timeout 5m
    }
    Write-Success "PostgreSQL installed"

    # MongoDB (with adequate resources)
    Write-Host "  Installing MongoDB..." -ForegroundColor Gray
    if (-not $DryRun) {
        helm upgrade --install mongodb bitnami/mongodb `
            --namespace homeguard-data `
            --set auth.rootPassword=homeguard-mongo-2024 `
            --set persistence.size=1Gi `
            --set resources.requests.cpu=100m `
            --set resources.requests.memory=256Mi `
            --set resources.limits.cpu=500m `
            --set resources.limits.memory=512Mi `
            --set metrics.enabled=false `
            --timeout 5m 2>$null
        # Don't wait - MongoDB takes time to initialize
    }
    Write-Success "MongoDB installed (initializing in background)"

    # Redis
    Write-Host "  Installing Redis..." -ForegroundColor Gray
    if (-not $DryRun) {
        helm upgrade --install redis bitnami/redis `
            --namespace homeguard-data `
            --set auth.password=homeguard-redis-2024 `
            --set master.persistence.size=512Mi `
            --set replica.replicaCount=0 `
            --set master.resources.requests.cpu=25m `
            --set master.resources.requests.memory=64Mi `
            --set master.resources.limits.cpu=100m `
            --set master.resources.limits.memory=128Mi `
            --set metrics.enabled=false `
            --wait --timeout 5m
    }
    Write-Success "Redis installed"

    # TimescaleDB (custom manifest)
    Write-Host "  Installing TimescaleDB..." -ForegroundColor Gray
    if (-not $DryRun) {
        kubectl apply -f $PSScriptRoot\timescaledb.yaml
    }
    Write-Success "TimescaleDB installed"

    # ScyllaDB (custom manifest)
    Write-Host "  Installing ScyllaDB..." -ForegroundColor Gray
    if (-not $DryRun) {
        kubectl apply -f $PSScriptRoot\scylladb.yaml
    }
    Write-Success "ScyllaDB installed"
}

# Install Kafka
if (-not $SkipKafka) {
    Write-Step "Installing Kafka (Strimzi with KRaft)"

    # Install Strimzi Operator
    Write-Host "  Installing Strimzi Operator..." -ForegroundColor Gray
    if (-not $DryRun) {
        helm upgrade --install strimzi-kafka-operator strimzi/strimzi-kafka-operator `
            --namespace strimzi-system `
            --set watchNamespaces="{homeguard-messaging}" `
            --wait --timeout 5m
    }
    Write-Success "Strimzi Operator installed"

    # Wait for CRDs to be ready
    Write-Host "  Waiting for Kafka CRDs..." -ForegroundColor Gray
    Start-Sleep -Seconds 15

    # Create Kafka cluster (KRaft mode - no ZooKeeper)
    Write-Host "  Creating Kafka cluster with KRaft (this takes ~5 minutes)..." -ForegroundColor Gray
    if (-not $DryRun) {
        kubectl apply -f $PSScriptRoot\kafka-cluster.yaml

        # Wait for Kafka to be ready
        $timeout = 300
        $elapsed = 0
        while ($elapsed -lt $timeout) {
            $podStatus = kubectl get pods -n homeguard-messaging -l strimzi.io/cluster=homeguard-kafka --no-headers 2>$null
            if ($podStatus -match "Running") {
                break
            }
            Start-Sleep -Seconds 10
            $elapsed += 10
            Write-Host "    Waiting for Kafka pod... ($elapsed/$timeout seconds)" -ForegroundColor Gray
        }
    }
    Write-Success "Kafka cluster created"

    # Create topics
    Write-Host "  Creating Kafka topics..." -ForegroundColor Gray
    if (-not $DryRun) {
        kubectl apply -f $PSScriptRoot\kafka-topics.yaml
    }
    Write-Success "Kafka topics created"
}

# Install Observability Stack
if (-not $SkipObservability) {
    Write-Step "Installing Observability Stack"

    # Determine ingress class
    $ingressClassName = if ($useTraefik) { "traefik" } else { "nginx" }

    # Prometheus + Grafana
    Write-Host "  Installing Prometheus + Grafana..." -ForegroundColor Gray
    if (-not $DryRun) {
        helm upgrade --install prometheus prometheus-community/kube-prometheus-stack `
            --namespace homeguard-observability `
            --set grafana.adminPassword=homeguard-grafana-2024 `
            --set grafana.persistence.enabled=false `
            --set "grafana.ingress.enabled=true" `
            --set "grafana.ingress.ingressClassName=$ingressClassName" `
            --set "grafana.ingress.hosts[0]=grafana.localhost" `
            --set grafana.resources.requests.cpu=50m `
            --set grafana.resources.requests.memory=128Mi `
            --set grafana.resources.limits.cpu=200m `
            --set grafana.resources.limits.memory=256Mi `
            --set prometheus.prometheusSpec.retention=1d `
            --set prometheus.prometheusSpec.resources.requests.cpu=100m `
            --set prometheus.prometheusSpec.resources.requests.memory=256Mi `
            --set prometheus.prometheusSpec.resources.limits.cpu=500m `
            --set prometheus.prometheusSpec.resources.limits.memory=512Mi `
            --set prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage=1Gi `
            --set alertmanager.enabled=false `
            --set nodeExporter.enabled=false `
            --set kubeStateMetrics.enabled=false `
            --wait --timeout 10m
    }
    Write-Success "Prometheus + Grafana installed"

    # Skip Loki for POC (requires S3-compatible storage in recent versions)
    Write-Warn "Skipping Loki (requires S3 storage) - using Prometheus metrics only"
}

# Install n8n
if (-not $SkipN8n) {
    Write-Step "Installing n8n"

    if (-not $DryRun) {
        kubectl apply -f $PSScriptRoot\n8n.yaml
    }
    Write-Success "n8n installed"
}

# Create secrets from .env file
Write-Step "Creating Secrets"

$envFile = Join-Path (Split-Path $PSScriptRoot -Parent | Split-Path -Parent) ".env"
if (Test-Path $envFile) {
    Write-Host "  Loading secrets from .env file..." -ForegroundColor Gray

    # Load .env file
    $envVars = @{}
    Get-Content $envFile | ForEach-Object {
        if ($_ -match '^\s*([^#][^=]+)=(.*)$') {
            $envVars[$matches[1].Trim()] = $matches[2].Trim()
        }
    }

    # Replace placeholders in secrets.yaml
    $secretsContent = Get-Content $PSScriptRoot\secrets.yaml -Raw
    foreach ($key in $envVars.Keys) {
        $secretsContent = $secretsContent -replace "\`$\{$key\}", $envVars[$key]
    }
    $secretsContent | Out-File -FilePath "$PSScriptRoot\secrets-resolved.yaml" -Encoding UTF8

    if (-not $DryRun) {
        kubectl apply -f "$PSScriptRoot\secrets-resolved.yaml"
    }
    Remove-Item "$PSScriptRoot\secrets-resolved.yaml" -Force
    Write-Success "Secrets created from .env file"
} else {
    Write-Warn "No .env file found at $envFile"
    Write-Warn "Copy .env.example to .env and add your API keys"
    if (-not $DryRun) {
        kubectl apply -f $PSScriptRoot\secrets.yaml
    }
    Write-Success "Secrets created (with placeholders - update manually)"
}

# Update hosts file
Write-Step "Updating Windows Hosts File"

$hostsPath = "C:\Windows\System32\drivers\etc\hosts"
$hostsEntries = @"

# HomeGuard IoT Platform (added by installer)
127.0.0.1 grafana.localhost
127.0.0.1 n8n.localhost
127.0.0.1 homeguard.localhost
127.0.0.1 api.homeguard.localhost
"@

try {
    $currentHosts = Get-Content $hostsPath -Raw -ErrorAction SilentlyContinue
    if ($currentHosts -notmatch "homeguard.localhost") {
        if (-not $DryRun) {
            Add-Content -Path $hostsPath -Value $hostsEntries -ErrorAction Stop
        }
        Write-Success "Hosts file updated"
    } else {
        Write-Success "Hosts file already configured"
    }
} catch {
    Write-Warn "Could not update hosts file. Run as Administrator or add entries manually:"
    Write-Host $hostsEntries -ForegroundColor Yellow
}

# Summary
Write-Step "Installation Complete!"

Write-Host @"

============================================
 INSTALLATION SUMMARY
============================================

Namespaces created:
  - homeguard-apps (application services)
  - homeguard-data (databases)
  - homeguard-messaging (Kafka)
  - homeguard-ai (AI/ML services)
  - homeguard-observability (Prometheus, Grafana)
  - homeguard-automation (n8n)

Databases installed:
  - PostgreSQL: postgresql.homeguard-data:5432
  - MongoDB: mongodb.homeguard-data:27017
  - Redis: redis-master.homeguard-data:6379
  - TimescaleDB: timescaledb.homeguard-data:5432
  - ScyllaDB: scylladb.homeguard-data:9042

Messaging:
  - Kafka (KRaft): homeguard-kafka-kafka-bootstrap.homeguard-messaging:9092

Access URLs:
  - Grafana: http://grafana.localhost
    Username: admin
    Password: homeguard-grafana-2024

  - n8n: http://n8n.localhost
    Username: admin
    Password: homeguard-n8n-2024

Default Credentials (for development only!):
  - PostgreSQL: postgres / homeguard-postgres-2024
  - MongoDB: root / homeguard-mongo-2024
  - Redis: homeguard-redis-2024
  - TimescaleDB: postgres / homeguard-timescale-2024

============================================
 NEXT STEPS
============================================

1. Wait for all pods to be ready (~5-10 minutes):
   kubectl get pods -A | Select-String homeguard

2. Verify installation:
   .\verify-installation.ps1

3. Access Grafana to verify metrics:
   http://grafana.localhost

4. Deploy application services:
   kubectl apply -f deploy/k8s/

"@ -ForegroundColor White

# Show pod status
Write-Step "Current Pod Status"
kubectl get pods -A --no-headers 2>$null | Select-String "homeguard|strimzi"
