# Metrics

The tfout exposes comprehensive Prometheus metrics to monitor its performance and behavior. These metrics help you track the health, performance, and usage patterns of your Terraform outputs synchronization.

## Metrics Endpoint

Metrics are exposed on the controller-runtime metrics endpoint:
- **Path**: `/metrics`
- **Port**: `8080` (production) / `8080` (local development)
- **Format**: Prometheus format
- **Authentication**: Required in production, disabled for local development with `--disable-metrics-auth`

## Available Metrics

### Reconciliation Metrics

#### `terraform_outputs_reconcile_total`
**Type**: Counter
**Description**: Total number of reconciliations performed
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource
- `result`: Result of reconciliation (`success`, `error`)

#### `terraform_outputs_reconcile_duration_seconds`
**Type**: Histogram
**Description**: Duration of reconciliation operations in seconds
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource
- `result`: Result of reconciliation (`success`, `error`)

### Backend Fetch Metrics

#### `terraform_outputs_backend_fetch_total`
**Type**: Counter
**Description**: Total number of backend fetch operations
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource
- `backend_type`: Type of backend (`s3`)
- `backend_index`: Index of the backend in the backends array (0-based)
- `result`: Result of the fetch operation (`success`, `error`)

#### `terraform_outputs_backend_fetch_duration_seconds`
**Type**: Histogram
**Description**: Duration of backend fetch operations in seconds
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource
- `backend_type`: Type of backend (`s3`)
- `backend_index`: Index of the backend in the backends array (0-based)

### Output Metrics

#### `terraform_outputs_found_total`
**Type**: Gauge
**Description**: Total number of outputs found from all backends
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource

#### `terraform_outputs_sensitive_total`
**Type**: Gauge
**Description**: Total number of sensitive outputs found
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource

#### `terraform_outputs_last_sync_timestamp`
**Type**: Gauge
**Description**: Unix timestamp of the last successful sync
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource

### S3 Backend Metrics

#### `terraform_outputs_s3_requests_total`
**Type**: Counter
**Description**: Total number of S3 API requests made
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource
- `operation`: S3 operation type (`GetObject`, `HeadObject`)
- `result`: Result of the S3 request (`success`, `error`)

### Kubernetes Resource Metrics

#### `terraform_outputs_configmap_operations_total`
**Type**: Counter
**Description**: Total number of ConfigMap operations performed
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource
- `operation`: Kubernetes operation (`create`, `update`)
- `result`: Result of the operation (`success`, `error`)

#### `terraform_outputs_secret_operations_total`
**Type**: Counter
**Description**: Total number of Secret operations performed
**Labels**:
- `namespace`: Namespace of the TerraformOutputs resource
- `name`: Name of the TerraformOutputs resource
- `operation`: Kubernetes operation (`create`, `update`)
- `result`: Result of the operation (`success`, `error`)

## Example Queries

### Basic Health Monitoring

```promql
# Reconciliation success rate
rate(terraform_outputs_reconcile_total{result="success"}[5m]) / rate(terraform_outputs_reconcile_total[5m])

# Average reconciliation duration
rate(terraform_outputs_reconcile_duration_seconds_sum[5m]) / rate(terraform_outputs_reconcile_duration_seconds_count[5m])

# Error rate
rate(terraform_outputs_reconcile_total{result="error"}[5m])
```

### Backend Performance

```promql
# S3 request success rate
rate(terraform_outputs_s3_requests_total{result="success"}[5m]) / rate(terraform_outputs_s3_requests_total[5m])

# Backend fetch duration 95th percentile
histogram_quantile(0.95, rate(terraform_outputs_backend_fetch_duration_seconds_bucket[5m]))

# Failed backend fetches
rate(terraform_outputs_backend_fetch_total{result="error"}[5m])
```

### Resource Operations

```promql
# ConfigMap create/update operations
rate(terraform_outputs_configmap_operations_total[5m])

# Secret operation errors
rate(terraform_outputs_secret_operations_total{result="error"}[5m])
```

### Output Tracking

```promql
# Total outputs managed per resource
terraform_outputs_found_total

# Percentage of sensitive outputs
terraform_outputs_sensitive_total / terraform_outputs_found_total * 100

# Time since last successful sync
time() - terraform_outputs_last_sync_timestamp
```

## Alerting Rules

Here are some example alerting rules for monitoring the operator:

```yaml
groups:
- name: tfout
  rules:
  - alert: TerraformOutputsReconcileFailure
    expr: rate(terraform_outputs_reconcile_total{result="error"}[5m]) > 0
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "TerraformOutputs reconciliation failures detected"
      description: "{{ $labels.namespace }}/{{ $labels.name }} has failed reconciliations"

  - alert: TerraformOutputsS3Errors
    expr: rate(terraform_outputs_s3_requests_total{result="error"}[5m]) > 0
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "S3 request failures for TerraformOutputs"
      description: "S3 requests failing for {{ $labels.namespace }}/{{ $labels.name }}"

  - alert: TerraformOutputsStaleSync
    expr: time() - terraform_outputs_last_sync_timestamp > 3600
    for: 0m
    labels:
      severity: warning
    annotations:
      summary: "TerraformOutputs sync is stale"
      description: "{{ $labels.namespace }}/{{ $labels.name }} hasn't synced for over 1 hour"

  - alert: TerraformOutputsSlowReconcile
    expr: histogram_quantile(0.95, rate(terraform_outputs_reconcile_duration_seconds_bucket[5m])) > 30
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "TerraformOutputs reconciliation is slow"
      description: "95th percentile reconciliation time is over 30 seconds"
```

## Grafana Dashboard

You can use these metrics to create comprehensive Grafana dashboards. Key panels to include:

1. **Overview**: Success rate, error rate, reconciliation count
2. **Performance**: Reconciliation duration, backend fetch duration
3. **Outputs**: Total outputs, sensitive outputs ratio
4. **Backend Health**: S3 request success rate, error breakdown
5. **Resource Operations**: ConfigMap/Secret create/update rates

## Monitoring Best Practices

1. **Set up alerts** for reconciliation failures and S3 errors
2. **Monitor sync freshness** using `terraform_outputs_last_sync_timestamp`
3. **Track performance trends** with duration histograms
4. **Monitor resource creation** to ensure ConfigMaps and Secrets are being created
5. **Watch for backend-specific issues** using backend_type labels