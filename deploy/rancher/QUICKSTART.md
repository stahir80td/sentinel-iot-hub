# HomeGuard IoT Platform - Quick Start Guide

## Prerequisites Checklist

- [ ] Windows 10/11 Pro (with WSL2 enabled)
- [ ] 8GB+ RAM available (16GB recommended)
- [ ] 20GB+ free disk space
- [ ] Rancher Desktop installed (https://rancherdesktop.io/)
- [ ] Google Gemini API key (get from https://makersuite.google.com/app/apikey)

**Note**: Rancher Desktop includes kubectl and Helm - no separate installation needed.

## Quick Installation

### Step 1: Start Rancher Desktop

1. Launch Rancher Desktop from Start Menu
2. Wait for Kubernetes to be ready (green checkmark in system tray)
3. Verify cluster is running:

```powershell
kubectl cluster-info
kubectl get nodes
```

### Step 2: Set up your environment

```powershell
# Navigate to deploy folder
cd C:\agents\iot\deploy\rancher

# Copy the example env file and add your Gemini API key
copy ..\..\env.example ..\..\env
notepad ..\..\env
# Edit the GEMINI_TEXT_API_KEY and GEMINI_VISION_API_KEY values
```

### Step 3: Run the installer

```powershell
# Open PowerShell as Administrator
# Run the installer
.\install-all.ps1

# Wait for all pods to be ready (~10-15 minutes)
# Watch progress with:
kubectl get pods -A -w | Select-String "homeguard"
```

### Step 4: Verify installation

```powershell
.\verify-installation.ps1
```

## Access Points

| Service | URL | Username | Password |
|---------|-----|----------|----------|
| Grafana | http://grafana.localhost | admin | homeguard-grafana-2024 |
| n8n | http://n8n.localhost | admin | homeguard-n8n-2024 |
| HomeGuard UI | http://homeguard.localhost | (see app) | (see app) |

**Important**: The installer adds required entries to your hosts file. If URLs don't work, check:
```powershell
type C:\Windows\System32\drivers\etc\hosts | Select-String "localhost"
```

## Resource Requirements (POC Mode)

| Component | CPU Request | Memory Request | Storage |
|-----------|-------------|----------------|---------|
| PostgreSQL | 50m | 128Mi | 1Gi |
| MongoDB | 100m | 256Mi | 1Gi |
| Redis | 25m | 64Mi | 512Mi |
| TimescaleDB | 100m | 256Mi | 1Gi |
| ScyllaDB | 200m | 384Mi | 1Gi |
| Kafka (KRaft) | 200m | 512Mi | 1Gi |
| Prometheus | 100m | 256Mi | 1Gi |
| Grafana | 50m | 128Mi | - |
| n8n | 100m | 256Mi | 5Gi |
| **Total** | **~1 CPU** | **~2.2Gi** | **~13Gi** |

**Minimum allocation**: 4 CPUs, 8GB RAM to Rancher Desktop

## Troubleshooting

### Pods stuck in Pending
```powershell
kubectl describe pod <pod-name> -n <namespace>
# Usually means PVC can't bind - check storage class
kubectl get pvc -A
kubectl get sc
```

### Pods in CrashLoopBackOff
```powershell
# Check logs
kubectl logs <pod-name> -n <namespace> --tail=50

# Common fixes:
# - Increase memory limits in deployment
# - Check if dependent services are ready
# - Delete PVC and let it recreate (data loss!)
```

### Can't access localhost URLs
```powershell
# 1. Check hosts file has entries
type C:\Windows\System32\drivers\etc\hosts | Select-String "localhost"

# 2. Check ingress is working
kubectl get ingress -A

# 3. Check Traefik is running
kubectl get pods -n kube-system | Select-String traefik
```

### Kafka not starting
```powershell
# Kafka with KRaft takes 3-5 minutes to fully start
kubectl get kafka -n homeguard-messaging
kubectl describe kafka homeguard-kafka -n homeguard-messaging

# Check for version errors - must use Kafka 4.x with Strimzi 0.49+
```

### MongoDB crashing
```powershell
# MongoDB needs more memory than default
# Check if it's OOMKilled:
kubectl describe pod -l app.kubernetes.io/name=mongodb -n homeguard-data | Select-String -A5 "State:"
```

### Certificate expired errors
```powershell
# If you see "certificate has expired" errors, reset Rancher Desktop:
# 1. Close Rancher Desktop
# 2. Delete ~/.kube/config or reset in Rancher Desktop settings
# 3. Restart Rancher Desktop
```

## Uninstall

```powershell
.\uninstall-all.ps1
```

## Key Files Reference

```
deploy/rancher/
├── RANCHER-SETUP.md      # Detailed setup guide
├── QUICKSTART.md         # This file
├── install-all.ps1       # Main installer script
├── verify-installation.ps1 # Verification script
├── uninstall-all.ps1     # Cleanup script
├── timescaledb.yaml      # TimescaleDB deployment
├── scylladb.yaml         # ScyllaDB deployment
├── kafka-cluster.yaml    # Kafka cluster (KRaft mode)
├── kafka-topics.yaml     # 8 Kafka topics
├── n8n.yaml              # n8n automation platform
├── secrets.yaml          # Application secrets (uses .env)
└── grafana-config.yaml   # Grafana datasources & dashboards

Root files:
├── .env.example          # Template for environment variables
├── .env                  # Your actual secrets (git-ignored)
└── .gitignore            # Prevents secrets from being committed
```

## Architecture Notes

- **Ingress**: Uses K3s built-in Traefik (not NGINX)
- **Kafka**: Uses KRaft mode (no ZooKeeper) with Strimzi operator
- **Observability**: Prometheus + Grafana (Loki skipped for POC)
- **Secrets**: Loaded from .env file at install time
