# HomeGuard IoT Platform - Installation Verification Script
# Run this to verify all components are properly installed and running

$ErrorActionPreference = "Continue"

function Write-Check {
    param($Name, $Status, $Details = "")
    if ($Status) {
        Write-Host "[PASS] " -ForegroundColor Green -NoNewline
    } else {
        Write-Host "[FAIL] " -ForegroundColor Red -NoNewline
    }
    Write-Host "$Name" -NoNewline
    if ($Details) {
        Write-Host " - $Details" -ForegroundColor Gray
    } else {
        Write-Host ""
    }
    return $Status
}

Write-Host @"

  HomeGuard IoT Platform - Installation Verification
  ===================================================

"@ -ForegroundColor Cyan

$allPassed = $true

# Check namespaces
Write-Host "`n--- Namespaces ---" -ForegroundColor Yellow
$namespaces = @("homeguard-apps", "homeguard-data", "homeguard-messaging", "homeguard-ai", "homeguard-observability", "homeguard-automation")
foreach ($ns in $namespaces) {
    $exists = kubectl get namespace $ns 2>$null
    $result = Write-Check $ns ($null -ne $exists)
    $allPassed = $allPassed -and $result
}

# Check PostgreSQL
Write-Host "`n--- PostgreSQL ---" -ForegroundColor Yellow
$pgPod = kubectl get pods -n homeguard-data -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "PostgreSQL Pod" ($pgPod -eq "Running") $pgPod
$allPassed = $allPassed -and $result

# Check MongoDB
Write-Host "`n--- MongoDB ---" -ForegroundColor Yellow
$mongoPod = kubectl get pods -n homeguard-data -l app.kubernetes.io/name=mongodb -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "MongoDB Pod" ($mongoPod -eq "Running") $mongoPod
$allPassed = $allPassed -and $result

# Check Redis
Write-Host "`n--- Redis ---" -ForegroundColor Yellow
$redisPod = kubectl get pods -n homeguard-data -l app.kubernetes.io/name=redis -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "Redis Pod" ($redisPod -eq "Running") $redisPod
$allPassed = $allPassed -and $result

# Check TimescaleDB
Write-Host "`n--- TimescaleDB ---" -ForegroundColor Yellow
$timescalePod = kubectl get pods -n homeguard-data -l app=timescaledb -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "TimescaleDB Pod" ($timescalePod -eq "Running") $timescalePod
$allPassed = $allPassed -and $result

# Check ScyllaDB
Write-Host "`n--- ScyllaDB ---" -ForegroundColor Yellow
$scyllaPod = kubectl get pods -n homeguard-data -l app=scylladb -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "ScyllaDB Pod" ($scyllaPod -eq "Running") $scyllaPod
$allPassed = $allPassed -and $result

# Check Kafka
Write-Host "`n--- Kafka ---" -ForegroundColor Yellow
$kafkaReady = kubectl get kafka homeguard-kafka -n homeguard-messaging -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>$null
$result = Write-Check "Kafka Cluster" ($kafkaReady -eq "True") "Ready: $kafkaReady"
$allPassed = $allPassed -and $result

$kafkaTopics = kubectl get kafkatopics -n homeguard-messaging -o jsonpath='{.items[*].metadata.name}' 2>$null
$topicCount = if ($kafkaTopics) { ($kafkaTopics -split " ").Count } else { 0 }
$result = Write-Check "Kafka Topics" ($topicCount -ge 6) "$topicCount topics created"
$allPassed = $allPassed -and $result

# Check Prometheus
Write-Host "`n--- Observability ---" -ForegroundColor Yellow
$promPod = kubectl get pods -n homeguard-observability -l app.kubernetes.io/name=prometheus -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "Prometheus Pod" ($promPod -eq "Running") $promPod
$allPassed = $allPassed -and $result

$grafanaPod = kubectl get pods -n homeguard-observability -l app.kubernetes.io/name=grafana -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "Grafana Pod" ($grafanaPod -eq "Running") $grafanaPod
$allPassed = $allPassed -and $result

$lokiPod = kubectl get pods -n homeguard-observability -l app.kubernetes.io/name=loki -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "Loki Pod" ($lokiPod -eq "Running") $lokiPod
$allPassed = $allPassed -and $result

# Check n8n
Write-Host "`n--- Automation ---" -ForegroundColor Yellow
$n8nPod = kubectl get pods -n homeguard-automation -l app=n8n -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "n8n Pod" ($n8nPod -eq "Running") $n8nPod
$allPassed = $allPassed -and $result

# Check Ingress
Write-Host "`n--- Ingress ---" -ForegroundColor Yellow
$ingressPod = kubectl get pods -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx -o jsonpath='{.items[0].status.phase}' 2>$null
$result = Write-Check "NGINX Ingress" ($ingressPod -eq "Running") $ingressPod
$allPassed = $allPassed -and $result

# Check Secrets
Write-Host "`n--- Secrets ---" -ForegroundColor Yellow
$appSecrets = kubectl get secret homeguard-secrets -n homeguard-apps 2>$null
$result = Write-Check "App Secrets" ($null -ne $appSecrets)
$allPassed = $allPassed -and $result

$aiSecrets = kubectl get secret homeguard-secrets -n homeguard-ai 2>$null
$result = Write-Check "AI Secrets" ($null -ne $aiSecrets)
$allPassed = $allPassed -and $result

# Check PVCs
Write-Host "`n--- Persistent Volume Claims ---" -ForegroundColor Yellow
$pvcs = kubectl get pvc -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}:{.status.phase}{"\n"}{end}' 2>$null
$pvcLines = $pvcs -split "`n" | Where-Object { $_ -match "homeguard" }
$boundCount = ($pvcLines | Where-Object { $_ -match "Bound" }).Count
$totalCount = $pvcLines.Count
$result = Write-Check "PVCs Bound" ($boundCount -eq $totalCount) "$boundCount/$totalCount bound"
$allPassed = $allPassed -and $result

# Test connectivity
Write-Host "`n--- Connectivity Tests ---" -ForegroundColor Yellow

# Test PostgreSQL
$pgTest = kubectl run pg-test --rm -i --restart=Never --namespace=homeguard-data `
    --image=postgres:15 --command -- pg_isready -h postgresql -U postgres 2>$null
$result = Write-Check "PostgreSQL Connection" ($LASTEXITCODE -eq 0)

# Summary
Write-Host "`n============================================" -ForegroundColor Cyan
if ($allPassed) {
    Write-Host "ALL CHECKS PASSED!" -ForegroundColor Green
    Write-Host "`nYour infrastructure is ready for application deployment."
} else {
    Write-Host "SOME CHECKS FAILED" -ForegroundColor Red
    Write-Host "`nPlease review the failed checks above and troubleshoot."
    Write-Host "`nCommon issues:"
    Write-Host "  - Pods in Pending: Check PVC status and storage class"
    Write-Host "  - Pods in CrashLoopBackOff: Check logs with 'kubectl logs <pod> -n <namespace>'"
    Write-Host "  - Kafka not ready: Wait a few more minutes, Kafka takes time to start"
}
Write-Host "============================================`n" -ForegroundColor Cyan

# Show resource usage
Write-Host "Resource Usage:" -ForegroundColor Yellow
kubectl top nodes 2>$null
Write-Host ""
