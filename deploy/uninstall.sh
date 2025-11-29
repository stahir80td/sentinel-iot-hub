#!/bin/bash
#
# IoT HomeGuard Platform - Uninstall Script
# Removes the Helm chart and cleans up all resources
#

set -e

# Configuration
NAMESPACE="${NAMESPACE:-sandbox}"
RELEASE_NAME="${RELEASE_NAME:-iot-homeguard}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Main
echo ""
echo "======================================================================="
echo "  IoT HomeGuard Platform - Uninstall Script"
echo "======================================================================="
echo ""

# Confirm uninstall
read -p "This will remove all IoT HomeGuard resources. Continue? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    log_warn "Uninstall cancelled"
    exit 0
fi

log_info "Uninstalling Helm release..."
helm uninstall "$RELEASE_NAME" -n "$NAMESPACE" 2>/dev/null || log_warn "Release not found or already uninstalled"

log_info "Deleting PVCs..."
kubectl delete pvc --all -n "$NAMESPACE" 2>/dev/null || log_warn "No PVCs to delete"

log_info "Waiting for pods to terminate..."
kubectl wait --for=delete pod --all -n "$NAMESPACE" --timeout=60s 2>/dev/null || true

log_info "Cleaning up any remaining resources..."
kubectl delete all -l app.kubernetes.io/instance="$RELEASE_NAME" -n "$NAMESPACE" 2>/dev/null || true

echo ""
log_success "Uninstall complete!"
echo ""
kubectl get pods -n "$NAMESPACE" 2>/dev/null || echo "Namespace is empty"
