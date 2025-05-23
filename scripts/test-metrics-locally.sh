#!/bin/bash

# Script to test metrics locally when running the controller with `make run`

set -e

METRICS_URL="http://localhost:8080/metrics"
TIMEOUT=5

echo "🔍 Testing tfout metrics locally..."
echo "📍 Metrics endpoint: $METRICS_URL"
echo

# Function to check if metrics endpoint is available
check_metrics_endpoint() {
    if curl -s --max-time $TIMEOUT "$METRICS_URL" > /dev/null 2>&1; then
        echo "✅ Metrics endpoint is accessible"
        return 0
    else
        echo "❌ Metrics endpoint is not accessible"
        echo "   Make sure the controller is running with 'make run'"
        return 1
    fi
}

# Function to display specific metrics
show_metric() {
    local metric_name=$1
    local description=$2

    echo "📊 $description"
    echo "   Metric: $metric_name"

    local result=$(curl -s --max-time $TIMEOUT "$METRICS_URL" | grep "^$metric_name" | head -5)
    if [ -n "$result" ]; then
        echo "$result" | sed 's/^/   /'
    else
        echo "   No data found for this metric"
    fi
    echo
}

# Main execution
if check_metrics_endpoint; then
    echo
    echo "🎯 TF Outputs Operator Metrics:"
    echo "================================"

    # Show reconciliation metrics
    show_metric "terraform_outputs_reconcile_total" "Reconciliation Count"
    show_metric "terraform_outputs_reconcile_duration_seconds" "Reconciliation Duration"

    # Show output metrics
    show_metric "terraform_outputs_found_total" "Total Outputs Found"
    show_metric "terraform_outputs_sensitive_total" "Sensitive Outputs"
    show_metric "terraform_outputs_last_sync_timestamp" "Last Sync Timestamp"

    # Show backend metrics
    show_metric "terraform_outputs_backend_fetch_total" "Backend Fetch Count"
    show_metric "terraform_outputs_s3_requests_total" "S3 Requests"

    # Show Kubernetes resource metrics
    show_metric "terraform_outputs_configmap_operations_total" "ConfigMap Operations"
    show_metric "terraform_outputs_secret_operations_total" "Secret Operations"

    echo "💡 Tips:"
    echo "   - Create a TerraformOutputs resource to see metrics populate"
    echo "   - Use 'curl $METRICS_URL | grep terraform_outputs' for all metrics"
    echo "   - Use 'watch curl -s $METRICS_URL | grep terraform_outputs_reconcile_total' to monitor reconciliations"
    echo

    echo "🔄 To monitor metrics in real-time:"
    echo "   watch -n 2 'curl -s $METRICS_URL | grep terraform_outputs_reconcile_total'"

else
    echo
    echo "🚀 To start the controller locally with metrics enabled:"
    echo "   make run"
    echo "   # or with development logging:"
    echo "   make run-dev"
    echo
    echo "   Then run this script again to check metrics."
    echo
    echo "🔍 Alternative manual commands:"
    echo "   go run ./cmd/main.go --metrics-bind-address=:8080 --disable-metrics-auth"
    echo "   go run ./cmd/main.go --metrics-bind-address=:8080 --zap-devel=true --disable-metrics-auth"
fi