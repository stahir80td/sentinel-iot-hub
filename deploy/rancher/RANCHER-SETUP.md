# HomeGuard IoT Platform - Rancher Local Setup Guide

This guide walks you through setting up a local Rancher Kubernetes cluster with all required infrastructure for the HomeGuard IoT Platform.

## Prerequisites

### Hardware Requirements
- **CPU**: 4+ cores (8 recommended)
- **RAM**: 16GB minimum (32GB recommended)
- **Disk**: 100GB free space
- **OS**: Windows 10/11 Pro or Enterprise (for Hyper-V) or Windows with WSL2

### Software Requirements
- Docker Desktop for Windows (with WSL2 backend)
- kubectl CLI
- Helm 3.x
- Git

---

## Part 1: Install Docker Desktop

### Step 1: Enable WSL2

Open PowerShell as Administrator:

```powershell
# Enable WSL
wsl --install

# Set WSL2 as default
wsl --set-default-version 2

# Restart your computer
```

### Step 2: Install Docker Desktop

1. Download Docker Desktop from: https://www.docker.com/products/docker-desktop/
2. Run the installer
3. During installation, ensure "Use WSL 2 instead of Hyper-V" is checked
4. After installation, open Docker Desktop
5. Go to Settings > Resources > WSL Integration > Enable for your distro
6. Go to Settings > Kubernetes > Enable Kubernetes (we'll use this as fallback)

### Step 3: Configure Docker Resources

Go to Docker Desktop > Settings > Resources:
- CPUs: 4+
- Memory: 8GB+ (12GB recommended)
- Swap: 2GB
- Disk image size: 100GB

---

## Part 2: Install Rancher Desktop (Alternative to Docker Desktop)

If you prefer Rancher Desktop (fully free, no licensing concerns):

### Step 1: Download Rancher Desktop

1. Go to: https://rancherdesktop.io/
2. Download the Windows installer
3. Run the installer

### Step 2: Configure Rancher Desktop

1. Open Rancher Desktop
2. Choose container runtime: **dockerd (moby)** for Docker compatibility
3. Enable Kubernetes
4. Set Kubernetes version: **v1.28.x** (stable)
5. Allocate resources:
   - CPUs: 4+
   - Memory: 8GB+

---

## Part 3: Install Rancher Manager (Web UI)

Rancher Manager provides a web UI for managing your Kubernetes cluster.

### Option A: Quick Install with Docker (Development)

```powershell
# Create a directory for Rancher data
mkdir C:\rancher-data

# Run Rancher Manager container
docker run -d --restart=unless-stopped `
  -p 80:80 -p 443:443 `
  -v C:\rancher-data:/var/lib/rancher `
  --privileged `
  --name rancher `
  rancher/rancher:latest
```

Wait 2-3 minutes, then access: https://localhost

Get the bootstrap password:
```powershell
docker logs rancher 2>&1 | Select-String "Bootstrap Password"
```

### Option B: Install on Kubernetes with Helm (Production-like)

```powershell
# Add Helm repos
helm repo add rancher-latest https://releases.rancher.com/server-charts/latest
helm repo add jetstack https://charts.jetstack.io
helm repo update

# Create namespace
kubectl create namespace cattle-system

# Install cert-manager (required for Rancher)
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.3/cert-manager.crds.yaml

helm install cert-manager jetstack/cert-manager `
  --namespace cert-manager `
  --create-namespace `
  --version v1.13.3

# Wait for cert-manager
kubectl -n cert-manager rollout status deploy/cert-manager

# Install Rancher
helm install rancher rancher-latest/rancher `
  --namespace cattle-system `
  --set hostname=rancher.localhost `
  --set bootstrapPassword=admin `
  --set replicas=1
```

Access at: https://rancher.localhost

---

## Part 4: Create Namespaces

```powershell
# Create all required namespaces
kubectl create namespace homeguard-apps
kubectl create namespace homeguard-data
kubectl create namespace homeguard-messaging
kubectl create namespace homeguard-ai
kubectl create namespace homeguard-observability
kubectl create namespace homeguard-automation

# Verify
kubectl get namespaces | Select-String "homeguard"
```

---

## Part 5: Install Ingress Controller (NGINX)

```powershell
# Add ingress-nginx repo
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update

# Install NGINX Ingress Controller
helm install ingress-nginx ingress-nginx/ingress-nginx `
  --namespace ingress-nginx `
  --create-namespace `
  --set controller.service.type=LoadBalancer `
  --set controller.watchIngressWithoutClass=true

# Wait for it to be ready
kubectl -n ingress-nginx rollout status deploy/ingress-nginx-controller

# Verify
kubectl -n ingress-nginx get svc
```

---

## Part 6: Install PostgreSQL (for User Service + TimescaleDB)

### PostgreSQL (Main - User Service)

```powershell
# Add Bitnami repo
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

# Create values file
@"
auth:
  postgresPassword: "homeguard-postgres-2024"
  database: "homeguard"

primary:
  persistence:
    size: 10Gi
  resources:
    requests:
      cpu: 250m
      memory: 512Mi
    limits:
      cpu: 500m
      memory: 1Gi

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
"@ | Out-File -FilePath postgres-values.yaml -Encoding UTF8

# Install PostgreSQL
helm install postgresql bitnami/postgresql `
  --namespace homeguard-data `
  -f postgres-values.yaml

# Get password (save this!)
kubectl get secret --namespace homeguard-data postgresql -o jsonpath="{.data.postgres-password}" |
  ForEach-Object { [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($_)) }
```

### TimescaleDB (Analytics Service)

```powershell
# Create TimescaleDB deployment
@"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: timescaledb
  namespace: homeguard-data
spec:
  replicas: 1
  selector:
    matchLabels:
      app: timescaledb
  template:
    metadata:
      labels:
        app: timescaledb
    spec:
      containers:
      - name: timescaledb
        image: timescale/timescaledb:latest-pg15
        ports:
        - containerPort: 5432
        env:
        - name: POSTGRES_PASSWORD
          value: "homeguard-timescale-2024"
        - name: POSTGRES_DB
          value: "analytics"
        volumeMounts:
        - name: data
          mountPath: /var/lib/postgresql/data
        resources:
          requests:
            cpu: 250m
            memory: 512Mi
          limits:
            cpu: 1000m
            memory: 2Gi
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: timescaledb-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: timescaledb
  namespace: homeguard-data
spec:
  ports:
  - port: 5432
    targetPort: 5432
  selector:
    app: timescaledb
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: timescaledb-pvc
  namespace: homeguard-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 20Gi
"@ | Out-File -FilePath timescaledb.yaml -Encoding UTF8

kubectl apply -f timescaledb.yaml
```

---

## Part 7: Install MongoDB

```powershell
# Create values file
@"
auth:
  rootPassword: "homeguard-mongo-2024"

persistence:
  size: 10Gi

resources:
  requests:
    cpu: 250m
    memory: 512Mi
  limits:
    cpu: 500m
    memory: 1Gi

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
"@ | Out-File -FilePath mongodb-values.yaml -Encoding UTF8

# Install MongoDB
helm install mongodb bitnami/mongodb `
  --namespace homeguard-data `
  -f mongodb-values.yaml

# Get password
kubectl get secret --namespace homeguard-data mongodb -o jsonpath="{.data.mongodb-root-password}" |
  ForEach-Object { [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($_)) }
```

---

## Part 8: Install Redis

```powershell
# Create values file
@"
auth:
  password: "homeguard-redis-2024"

master:
  persistence:
    size: 5Gi
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 250m
      memory: 512Mi

replica:
  replicaCount: 0  # Single node for local dev

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
"@ | Out-File -FilePath redis-values.yaml -Encoding UTF8

# Install Redis
helm install redis bitnami/redis `
  --namespace homeguard-data `
  -f redis-values.yaml
```

---

## Part 9: Install ScyllaDB

```powershell
# ScyllaDB requires more setup - using simple deployment for local dev
@"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: scylladb
  namespace: homeguard-data
spec:
  replicas: 1
  selector:
    matchLabels:
      app: scylladb
  template:
    metadata:
      labels:
        app: scylladb
    spec:
      containers:
      - name: scylladb
        image: scylladb/scylla:5.4
        ports:
        - containerPort: 9042
          name: cql
        - containerPort: 9160
          name: thrift
        args:
        - --smp=2
        - --memory=1G
        - --overprovisioned=1
        - --developer-mode=1
        volumeMounts:
        - name: data
          mountPath: /var/lib/scylla
        resources:
          requests:
            cpu: 500m
            memory: 1Gi
          limits:
            cpu: 2000m
            memory: 2Gi
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: scylladb-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: scylladb
  namespace: homeguard-data
spec:
  ports:
  - port: 9042
    targetPort: 9042
    name: cql
  selector:
    app: scylladb
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: scylladb-pvc
  namespace: homeguard-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 20Gi
"@ | Out-File -FilePath scylladb.yaml -Encoding UTF8

kubectl apply -f scylladb.yaml
```

---

## Part 10: Install Kafka (Strimzi Operator)

```powershell
# Install Strimzi Operator
kubectl create namespace strimzi-system

helm repo add strimzi https://strimzi.io/charts/
helm repo update

helm install strimzi-kafka-operator strimzi/strimzi-kafka-operator `
  --namespace strimzi-system `
  --set watchNamespaces="{homeguard-messaging}"

# Wait for operator
kubectl -n strimzi-system rollout status deploy/strimzi-cluster-operator

# Create Kafka cluster
@"
apiVersion: kafka.strimzi.io/v1beta2
kind: Kafka
metadata:
  name: homeguard-kafka
  namespace: homeguard-messaging
spec:
  kafka:
    version: 3.6.1
    replicas: 1
    listeners:
      - name: plain
        port: 9092
        type: internal
        tls: false
      - name: tls
        port: 9093
        type: internal
        tls: true
    config:
      offsets.topic.replication.factor: 1
      transaction.state.log.replication.factor: 1
      transaction.state.log.min.isr: 1
      default.replication.factor: 1
      min.insync.replicas: 1
      inter.broker.protocol.version: "3.6"
    storage:
      type: persistent-claim
      size: 20Gi
    resources:
      requests:
        cpu: 500m
        memory: 1Gi
      limits:
        cpu: 1000m
        memory: 2Gi
  zookeeper:
    replicas: 1
    storage:
      type: persistent-claim
      size: 10Gi
    resources:
      requests:
        cpu: 250m
        memory: 512Mi
      limits:
        cpu: 500m
        memory: 1Gi
  entityOperator:
    topicOperator: {}
    userOperator: {}
"@ | Out-File -FilePath kafka-cluster.yaml -Encoding UTF8

kubectl apply -f kafka-cluster.yaml

# Wait for Kafka to be ready (this takes a few minutes)
kubectl -n homeguard-messaging wait kafka/homeguard-kafka --for=condition=Ready --timeout=300s

# Create Kafka topics
@"
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: device-events
  namespace: homeguard-messaging
  labels:
    strimzi.io/cluster: homeguard-kafka
spec:
  partitions: 3
  replicas: 1
  config:
    retention.ms: 604800000  # 7 days
---
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: device-commands
  namespace: homeguard-messaging
  labels:
    strimzi.io/cluster: homeguard-kafka
spec:
  partitions: 3
  replicas: 1
---
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: device-heartbeats
  namespace: homeguard-messaging
  labels:
    strimzi.io/cluster: homeguard-kafka
spec:
  partitions: 3
  replicas: 1
---
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: device-alerts
  namespace: homeguard-messaging
  labels:
    strimzi.io/cluster: homeguard-kafka
spec:
  partitions: 3
  replicas: 1
"@ | Out-File -FilePath kafka-topics.yaml -Encoding UTF8

kubectl apply -f kafka-topics.yaml
```

---

## Part 11: Install Prometheus & Grafana (Observability)

```powershell
# Add Prometheus community repo
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Create values file for kube-prometheus-stack
@"
grafana:
  enabled: true
  adminPassword: "homeguard-grafana-2024"
  persistence:
    enabled: true
    size: 5Gi
  ingress:
    enabled: true
    ingressClassName: nginx
    hosts:
      - grafana.localhost

prometheus:
  prometheusSpec:
    retention: 15d
    storageSpec:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 20Gi
    resources:
      requests:
        cpu: 250m
        memory: 512Mi
      limits:
        cpu: 1000m
        memory: 2Gi

alertmanager:
  enabled: true
  alertmanagerSpec:
    storage:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 5Gi
"@ | Out-File -FilePath prometheus-stack-values.yaml -Encoding UTF8

# Install kube-prometheus-stack
helm install prometheus prometheus-community/kube-prometheus-stack `
  --namespace homeguard-observability `
  -f prometheus-stack-values.yaml

# Get Grafana password
kubectl get secret --namespace homeguard-observability prometheus-grafana -o jsonpath="{.data.admin-password}" |
  ForEach-Object { [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($_)) }
```

---

## Part 12: Install Loki (Log Aggregation)

```powershell
# Add Grafana repo
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

# Create values file
@"
loki:
  auth_enabled: false
  storage:
    type: filesystem
  commonConfig:
    replication_factor: 1
  schemaConfig:
    configs:
      - from: 2024-01-01
        store: tsdb
        object_store: filesystem
        schema: v13
        index:
          prefix: loki_index_
          period: 24h

singleBinary:
  replicas: 1
  persistence:
    size: 20Gi
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 1Gi

monitoring:
  selfMonitoring:
    enabled: false
  lokiCanary:
    enabled: false

test:
  enabled: false
"@ | Out-File -FilePath loki-values.yaml -Encoding UTF8

# Install Loki
helm install loki grafana/loki `
  --namespace homeguard-observability `
  -f loki-values.yaml
```

---

## Part 13: Install Promtail (Log Shipper)

```powershell
@"
config:
  clients:
    - url: http://loki:3100/loki/api/v1/push

  snippets:
    pipelineStages:
      - cri: {}
      - json:
          expressions:
            level: level
            service: service
            trace_id: trace_id
      - labels:
          level:
          service:
"@ | Out-File -FilePath promtail-values.yaml -Encoding UTF8

helm install promtail grafana/promtail `
  --namespace homeguard-observability `
  -f promtail-values.yaml
```

---

## Part 14: Install n8n (Workflow Automation)

```powershell
# Create n8n deployment
@"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: n8n
  namespace: homeguard-automation
spec:
  replicas: 1
  selector:
    matchLabels:
      app: n8n
  template:
    metadata:
      labels:
        app: n8n
    spec:
      containers:
      - name: n8n
        image: n8nio/n8n:latest
        ports:
        - containerPort: 5678
        env:
        - name: N8N_BASIC_AUTH_ACTIVE
          value: "true"
        - name: N8N_BASIC_AUTH_USER
          value: "admin"
        - name: N8N_BASIC_AUTH_PASSWORD
          value: "homeguard-n8n-2024"
        - name: N8N_HOST
          value: "n8n.localhost"
        - name: N8N_PORT
          value: "5678"
        - name: N8N_PROTOCOL
          value: "http"
        - name: WEBHOOK_URL
          value: "http://n8n.localhost/"
        - name: GENERIC_TIMEZONE
          value: "America/New_York"
        volumeMounts:
        - name: data
          mountPath: /home/node/.n8n
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 500m
            memory: 512Mi
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: n8n-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: n8n
  namespace: homeguard-automation
spec:
  ports:
  - port: 5678
    targetPort: 5678
  selector:
    app: n8n
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: n8n-pvc
  namespace: homeguard-automation
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: n8n-ingress
  namespace: homeguard-automation
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "50m"
spec:
  ingressClassName: nginx
  rules:
  - host: n8n.localhost
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: n8n
            port:
              number: 5678
"@ | Out-File -FilePath n8n.yaml -Encoding UTF8

kubectl apply -f n8n.yaml
```

---

## Part 15: Create Secrets for Services

```powershell
# Create secrets for all services
@"
apiVersion: v1
kind: Secret
metadata:
  name: homeguard-secrets
  namespace: homeguard-apps
type: Opaque
stringData:
  # Database credentials
  POSTGRES_URL: "postgresql://postgres:homeguard-postgres-2024@postgresql.homeguard-data:5432/homeguard"
  TIMESCALE_URL: "postgresql://postgres:homeguard-timescale-2024@timescaledb.homeguard-data:5432/analytics"
  MONGO_URL: "mongodb://root:homeguard-mongo-2024@mongodb.homeguard-data:27017/homeguard?authSource=admin"
  REDIS_URL: "redis://:homeguard-redis-2024@redis-master.homeguard-data:6379"
  SCYLLA_HOSTS: "scylladb.homeguard-data"

  # Kafka
  KAFKA_BROKERS: "homeguard-kafka-kafka-bootstrap.homeguard-messaging:9092"

  # Service URLs
  MCP_SERVER_URL: "http://mcp-server.homeguard-ai:8080"
  ANOMALY_SERVICE_URL: "http://anomaly-ml.homeguard-ai:8080"
  N8N_WEBHOOK_BASE: "http://n8n.homeguard-automation:5678/webhook"

  # JWT Secret
  JWT_SECRET: "homeguard-jwt-secret-change-in-production-2024"

  # Gemini API Key (placeholder - user must provide)
  GEMINI_API_KEY: "your-gemini-api-key-here"
"@ | Out-File -FilePath secrets.yaml -Encoding UTF8

kubectl apply -f secrets.yaml

# Copy secrets to AI namespace
kubectl get secret homeguard-secrets -n homeguard-apps -o yaml | `
  ForEach-Object { $_ -replace 'namespace: homeguard-apps', 'namespace: homeguard-ai' } | `
  kubectl apply -f -
```

---

## Part 16: Configure Local DNS (Windows hosts file)

Run PowerShell as Administrator:

```powershell
# Add entries to hosts file
$hostsPath = "C:\Windows\System32\drivers\etc\hosts"
$entries = @"

# HomeGuard IoT Platform
127.0.0.1 rancher.localhost
127.0.0.1 grafana.localhost
127.0.0.1 n8n.localhost
127.0.0.1 homeguard.localhost
127.0.0.1 api.homeguard.localhost
"@

Add-Content -Path $hostsPath -Value $entries
```

---

## Part 17: Verify Installation

```powershell
# Check all pods are running
kubectl get pods -A | Select-String "homeguard"

# Check services
kubectl get svc -A | Select-String "homeguard"

# Check persistent volumes
kubectl get pvc -A | Select-String "homeguard"

# Test database connections
# PostgreSQL
kubectl run pg-test --rm -it --restart=Never --namespace=homeguard-data `
  --image=postgres:15 -- psql -h postgresql -U postgres -c "SELECT version();"

# MongoDB
kubectl run mongo-test --rm -it --restart=Never --namespace=homeguard-data `
  --image=mongo:7 -- mongosh --host mongodb --eval "db.version()"

# Redis
kubectl run redis-test --rm -it --restart=Never --namespace=homeguard-data `
  --image=redis:7 -- redis-cli -h redis-master -a homeguard-redis-2024 ping
```

---

## Part 18: Access URLs

After everything is installed:

| Service | URL | Credentials |
|---------|-----|-------------|
| Rancher | https://localhost | admin / (from bootstrap) |
| Grafana | http://grafana.localhost | admin / homeguard-grafana-2024 |
| n8n | http://n8n.localhost | admin / homeguard-n8n-2024 |
| HomeGuard UI | http://homeguard.localhost | john@demo.com / demo123 |
| HomeGuard API | http://api.homeguard.localhost | JWT token |

---

## Quick Start Script

Save this as `install-all.ps1` and run as Administrator:

```powershell
# install-all.ps1 - Complete installation script

Write-Host "Starting HomeGuard IoT Platform Infrastructure Setup..." -ForegroundColor Green

# Add Helm repos
Write-Host "Adding Helm repositories..." -ForegroundColor Yellow
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add strimzi https://strimzi.io/charts/
helm repo update

# Create namespaces
Write-Host "Creating namespaces..." -ForegroundColor Yellow
kubectl create namespace homeguard-apps --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace homeguard-data --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace homeguard-messaging --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace homeguard-ai --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace homeguard-observability --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace homeguard-automation --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace strimzi-system --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace ingress-nginx --dry-run=client -o yaml | kubectl apply -f -

# Install Ingress
Write-Host "Installing NGINX Ingress Controller..." -ForegroundColor Yellow
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx `
  --namespace ingress-nginx `
  --set controller.service.type=LoadBalancer `
  --wait

# Install databases
Write-Host "Installing PostgreSQL..." -ForegroundColor Yellow
helm upgrade --install postgresql bitnami/postgresql `
  --namespace homeguard-data `
  --set auth.postgresPassword=homeguard-postgres-2024 `
  --set auth.database=homeguard `
  --set primary.persistence.size=10Gi `
  --wait

Write-Host "Installing MongoDB..." -ForegroundColor Yellow
helm upgrade --install mongodb bitnami/mongodb `
  --namespace homeguard-data `
  --set auth.rootPassword=homeguard-mongo-2024 `
  --set persistence.size=10Gi `
  --wait

Write-Host "Installing Redis..." -ForegroundColor Yellow
helm upgrade --install redis bitnami/redis `
  --namespace homeguard-data `
  --set auth.password=homeguard-redis-2024 `
  --set master.persistence.size=5Gi `
  --set replica.replicaCount=0 `
  --wait

# Apply custom manifests
Write-Host "Installing TimescaleDB..." -ForegroundColor Yellow
kubectl apply -f timescaledb.yaml

Write-Host "Installing ScyllaDB..." -ForegroundColor Yellow
kubectl apply -f scylladb.yaml

# Install Kafka
Write-Host "Installing Strimzi Kafka Operator..." -ForegroundColor Yellow
helm upgrade --install strimzi-kafka-operator strimzi/strimzi-kafka-operator `
  --namespace strimzi-system `
  --set watchNamespaces="{homeguard-messaging}" `
  --wait

Write-Host "Creating Kafka cluster (this takes ~3 minutes)..." -ForegroundColor Yellow
kubectl apply -f kafka-cluster.yaml
Start-Sleep -Seconds 30
kubectl apply -f kafka-topics.yaml

# Install observability
Write-Host "Installing Prometheus + Grafana stack..." -ForegroundColor Yellow
helm upgrade --install prometheus prometheus-community/kube-prometheus-stack `
  --namespace homeguard-observability `
  --set grafana.adminPassword=homeguard-grafana-2024 `
  --set grafana.persistence.enabled=true `
  --wait

Write-Host "Installing Loki..." -ForegroundColor Yellow
helm upgrade --install loki grafana/loki `
  --namespace homeguard-observability `
  --set loki.auth_enabled=false `
  --set singleBinary.replicas=1 `
  --wait

Write-Host "Installing Promtail..." -ForegroundColor Yellow
helm upgrade --install promtail grafana/promtail `
  --namespace homeguard-observability `
  --set "config.clients[0].url=http://loki:3100/loki/api/v1/push" `
  --wait

# Install n8n
Write-Host "Installing n8n..." -ForegroundColor Yellow
kubectl apply -f n8n.yaml

# Create secrets
Write-Host "Creating secrets..." -ForegroundColor Yellow
kubectl apply -f secrets.yaml

Write-Host ""
Write-Host "============================================" -ForegroundColor Green
Write-Host "Installation Complete!" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green
Write-Host ""
Write-Host "Access URLs:" -ForegroundColor Cyan
Write-Host "  Grafana: http://grafana.localhost (admin / homeguard-grafana-2024)"
Write-Host "  n8n: http://n8n.localhost (admin / homeguard-n8n-2024)"
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "  1. Update C:\Windows\System32\drivers\etc\hosts with local domains"
Write-Host "  2. Update GEMINI_API_KEY in secrets.yaml with your API key"
Write-Host "  3. Wait for Kafka cluster to be ready: kubectl -n homeguard-messaging get kafka"
Write-Host ""
```

---

## Troubleshooting

### Pod stuck in Pending
```powershell
kubectl describe pod <pod-name> -n <namespace>
# Usually a PVC or resource issue
```

### Check logs
```powershell
kubectl logs <pod-name> -n <namespace> -f
```

### Reset everything
```powershell
# Delete all HomeGuard resources
kubectl delete namespace homeguard-apps homeguard-data homeguard-messaging homeguard-ai homeguard-observability homeguard-automation

# Recreate namespaces and start over
```

### Low resources
If pods are getting OOMKilled, reduce resource requests in the values files or increase Docker Desktop memory allocation.

---

## Resource Summary

| Component | CPU Request | Memory Request | Storage |
|-----------|-------------|----------------|---------|
| PostgreSQL | 250m | 512Mi | 10Gi |
| TimescaleDB | 250m | 512Mi | 20Gi |
| MongoDB | 250m | 512Mi | 10Gi |
| Redis | 100m | 256Mi | 5Gi |
| ScyllaDB | 500m | 1Gi | 20Gi |
| Kafka + ZK | 750m | 1.5Gi | 30Gi |
| Prometheus | 250m | 512Mi | 20Gi |
| Grafana | 100m | 128Mi | 5Gi |
| Loki | 100m | 256Mi | 20Gi |
| n8n | 100m | 256Mi | 5Gi |
| **Total** | **~2.7 CPU** | **~5.5Gi** | **~145Gi** |

Plus headroom for application services (~1 CPU, ~2Gi RAM).

**Recommended minimum**: 4 CPU cores, 12GB RAM allocated to Docker/Rancher Desktop.
