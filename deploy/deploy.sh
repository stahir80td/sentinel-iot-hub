#!/bin/bash
#
# IoT HomeGuard Platform - Deployment Script
# Deploys the Helm chart and ensures all pods are running correctly
#

set -e

# Configuration
NAMESPACE="${NAMESPACE:-sandbox}"
RELEASE_NAME="${RELEASE_NAME:-iot-homeguard}"
CHART_PATH="$(dirname "$0")/helm/iot-homeguard"
VALUES_FILE="${VALUES_FILE:-values-local.yaml}"
TIMEOUT="${TIMEOUT:-300}"  # 5 minutes default timeout

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        exit 1
    fi

    if ! command -v helm &> /dev/null; then
        log_error "helm is not installed"
        exit 1
    fi

    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi

    log_success "Prerequisites check passed"
}

# Wait for a specific pod to be ready
wait_for_pod() {
    local app_label=$1
    local timeout=$2
    local start_time=$(date +%s)

    log_info "Waiting for pod with label app=$app_label..."

    while true; do
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))

        if [ $elapsed -ge $timeout ]; then
            log_error "Timeout waiting for $app_label (${timeout}s)"
            return 1
        fi

        local ready=$(kubectl get pods -n "$NAMESPACE" -l "app=$app_label" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)

        if [ "$ready" == "True" ]; then
            log_success "$app_label is ready"
            return 0
        fi

        sleep 5
    done
}

# Wait for StatefulSet pod to be ready
wait_for_statefulset_pod() {
    local name=$1
    local timeout=$2
    local start_time=$(date +%s)

    log_info "Waiting for StatefulSet pod $name..."

    while true; do
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))

        if [ $elapsed -ge $timeout ]; then
            log_error "Timeout waiting for $name (${timeout}s)"
            return 1
        fi

        local ready=$(kubectl get pod "$name" -n "$NAMESPACE" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)

        if [ "$ready" == "True" ]; then
            log_success "$name is ready"
            return 0
        fi

        sleep 5
    done
}

# Restart a deployment and wait for it to be ready
restart_and_wait() {
    local app_label=$1
    local timeout=$2

    log_info "Restarting $app_label..."
    kubectl rollout restart deployment -n "$NAMESPACE" -l "app=$app_label" 2>/dev/null || true
    sleep 5
    wait_for_pod "$app_label" "$timeout"
}

# Delete pods for an app and wait for new ones
delete_and_wait() {
    local app_label=$1
    local timeout=$2

    log_info "Deleting and recreating pods for $app_label..."
    kubectl delete pods -n "$NAMESPACE" -l "app=$app_label" --force --grace-period=0 2>/dev/null || true
    sleep 10
    wait_for_pod "$app_label" "$timeout"
}

# Clean up old ReplicaSets
cleanup_old_replicasets() {
    log_info "Cleaning up old ReplicaSets..."

    # Get all ReplicaSets with 0 desired replicas and delete them
    kubectl get rs -n "$NAMESPACE" -o json | \
        jq -r '.items[] | select(.spec.replicas == 0) | .metadata.name' | \
        xargs -r kubectl delete rs -n "$NAMESPACE" 2>/dev/null || true

    log_success "Old ReplicaSets cleaned up"
}

# Deploy or upgrade Helm chart
deploy_helm_chart() {
    log_info "Deploying Helm chart..."
    log_info "  Release: $RELEASE_NAME"
    log_info "  Namespace: $NAMESPACE"
    log_info "  Values file: $VALUES_FILE"

    # Check if release exists
    if helm status "$RELEASE_NAME" -n "$NAMESPACE" &> /dev/null; then
        log_info "Upgrading existing release..."
        helm upgrade "$RELEASE_NAME" "$CHART_PATH" \
            -f "$CHART_PATH/$VALUES_FILE" \
            -n "$NAMESPACE" \
            --timeout 10m
    else
        log_info "Installing new release..."
        helm install "$RELEASE_NAME" "$CHART_PATH" \
            -f "$CHART_PATH/$VALUES_FILE" \
            -n "$NAMESPACE" \
            --create-namespace \
            --timeout 10m
    fi

    log_success "Helm chart deployed"
}

# Wait for infrastructure components
wait_for_infrastructure() {
    log_info "Waiting for infrastructure components..."

    # Tier 1: Databases (no dependencies)
    log_info "=== Tier 1: Core Databases ==="
    wait_for_pod "iot-postgresql" 120 &
    wait_for_pod "iot-redis" 60 &
    wait_for_pod "iot-mongodb" 180 &
    wait_for_pod "iot-timescaledb" 120 &
    wait_for_pod "iot-scylladb" 180 &
    wait

    # Tier 2: Messaging
    log_info "=== Tier 2: Messaging ==="
    wait_for_statefulset_pod "iot-kafka-0" 180

    log_success "Infrastructure is ready"
}

# Wait for application components
wait_for_applications() {
    log_info "Waiting for application components..."

    # Tier 3: Core services (depend on databases)
    log_info "=== Tier 3: Core Services ==="
    wait_for_pod "iot-user-service" 120 &
    wait_for_pod "iot-device-service" 120 &
    wait_for_pod "iot-notification-service" 120 &
    wait

    # Tier 4: Event-driven services (depend on Kafka + databases)
    log_info "=== Tier 4: Event-Driven Services ==="
    wait_for_pod "iot-device-ingest" 120 &
    wait_for_pod "iot-event-processor" 120 &
    wait_for_pod "iot-scenario-engine" 120 &
    wait

    # Tier 5: API and Frontend
    log_info "=== Tier 5: API & Frontend ==="
    wait_for_pod "iot-api-gateway" 60 &
    wait_for_pod "iot-frontend" 60 &
    wait_for_pod "iot-agentic-ai" 60 &
    wait

    log_success "Applications are ready"
}

# Wait for monitoring components
wait_for_monitoring() {
    log_info "Waiting for monitoring components..."

    wait_for_pod "iot-prometheus" 120 &
    wait_for_pod "iot-grafana" 120 &
    wait_for_pod "iot-n8n" 180 &
    wait

    log_success "Monitoring is ready"
}

# Restart services that depend on Kafka (in case they started before Kafka was ready)
restart_kafka_dependents() {
    log_info "Restarting Kafka-dependent services..."

    # Check if device-ingest is healthy
    local ingest_ready=$(kubectl get pods -n "$NAMESPACE" -l "app=iot-device-ingest" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)
    if [ "$ingest_ready" != "True" ]; then
        delete_and_wait "iot-device-ingest" 120
    fi

    # Check if event-processor is healthy
    local processor_ready=$(kubectl get pods -n "$NAMESPACE" -l "app=iot-event-processor" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)
    if [ "$processor_ready" != "True" ]; then
        delete_and_wait "iot-event-processor" 120
    fi

    # Check if scenario-engine is healthy
    local engine_ready=$(kubectl get pods -n "$NAMESPACE" -l "app=iot-scenario-engine" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)
    if [ "$engine_ready" != "True" ]; then
        delete_and_wait "iot-scenario-engine" 120
    fi

    log_success "Kafka-dependent services restarted"
}

# Verify all pods are running
verify_deployment() {
    log_info "Verifying deployment..."

    local not_ready=$(kubectl get pods -n "$NAMESPACE" --no-headers | grep -v "Running\|Completed" | wc -l)

    if [ "$not_ready" -gt 0 ]; then
        log_warn "Some pods are not ready:"
        kubectl get pods -n "$NAMESPACE" --no-headers | grep -v "Running\|Completed"
        return 1
    fi

    local not_fully_ready=$(kubectl get pods -n "$NAMESPACE" --no-headers | grep -E "0/[0-9]+" | wc -l)

    if [ "$not_fully_ready" -gt 0 ]; then
        log_warn "Some pods are not fully ready:"
        kubectl get pods -n "$NAMESPACE" --no-headers | grep -E "0/[0-9]+"
        return 1
    fi

    log_success "All pods are running and ready!"
    return 0
}

# Print deployment summary
print_summary() {
    echo ""
    echo "======================================================================="
    echo "  IoT HomeGuard Platform - Deployment Complete"
    echo "======================================================================="
    echo ""
    kubectl get pods -n "$NAMESPACE" -o wide
    echo ""
    echo "-----------------------------------------------------------------------"
    echo "  Access URLs (configure /etc/hosts for local development)"
    echo "-----------------------------------------------------------------------"
    echo "  Frontend:   http://homeguard.localhost"
    echo "  API:        http://homeguard.localhost/api"
    echo "  Grafana:    http://grafana.homeguard.localhost"
    echo "  Prometheus: http://prometheus.homeguard.localhost"
    echo "  n8n:        http://n8n.homeguard.localhost"
    echo ""
    echo "-----------------------------------------------------------------------"
    echo "  Useful Commands"
    echo "-----------------------------------------------------------------------"
    echo "  kubectl get pods -n $NAMESPACE"
    echo "  kubectl logs -n $NAMESPACE -l app=iot-api-gateway"
    echo "  helm status $RELEASE_NAME -n $NAMESPACE"
    echo "======================================================================="
}

# Main function
main() {
    echo ""
    echo "======================================================================="
    echo "  IoT HomeGuard Platform - Deployment Script"
    echo "======================================================================="
    echo ""

    check_prerequisites
    deploy_helm_chart

    echo ""
    log_info "Waiting for pods to start (this may take a few minutes)..."
    sleep 30

    wait_for_infrastructure
    wait_for_applications
    wait_for_monitoring

    # Give everything a moment to stabilize
    sleep 10

    # Restart any services that might have failed due to dependency timing
    restart_kafka_dependents

    # Clean up old resources
    cleanup_old_replicasets

    # Final wait for everything to stabilize
    sleep 15

    # Verify deployment
    local retry=0
    local max_retries=3

    while [ $retry -lt $max_retries ]; do
        if verify_deployment; then
            break
        fi

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            log_warn "Retrying verification in 30 seconds... (attempt $((retry + 1))/$max_retries)"
            sleep 30
        fi
    done

    print_summary

    if ! verify_deployment; then
        log_warn "Deployment completed but some pods may need attention"
        exit 1
    fi

    log_success "Deployment completed successfully!"
}

# Run main function
main "$@"
