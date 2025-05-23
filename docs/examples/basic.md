# Basic Usage Examples

This page provides practical examples of using TFOut in common scenarios.

## Example 1: Simple Web Application

Let's say you have a Terraform configuration that creates infrastructure for a web application:

### Terraform Configuration

```hcl
# main.tf
resource "aws_db_instance" "webapp" {
  identifier = "webapp-db"
  engine     = "postgres"
  # ... other configuration
}

resource "aws_elasticache_cluster" "webapp" {
  cluster_id = "webapp-cache"
  engine     = "redis"
  # ... other configuration
}

output "database_endpoint" {
  value       = aws_db_instance.webapp.endpoint
  description = "PostgreSQL database endpoint"
}

output "database_port" {
  value       = aws_db_instance.webapp.port
  description = "PostgreSQL database port"
}

output "database_name" {
  value       = aws_db_instance.webapp.db_name
  description = "PostgreSQL database name"
}

output "database_password" {
  value       = aws_db_instance.webapp.password
  description = "PostgreSQL database password"
  sensitive   = true
}

output "cache_endpoint" {
  value       = aws_elasticache_cluster.webapp.cache_nodes[0].address
  description = "Redis cache endpoint"
}

output "cache_port" {
  value       = aws_elasticache_cluster.webapp.port
  description = "Redis cache port"
}
```

### TFOut Configuration

```yaml
# webapp-config.yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: webapp-infrastructure
  namespace: production
  labels:
    app: webapp
    component: infrastructure
spec:
  syncInterval: 5m
  backends:
  - s3:
      bucket: my-terraform-state
      key: webapp/production/terraform.tfstate
      region: us-west-2
  target:
    namespace: production
    configMapName: webapp-config
    secretName: webapp-secrets
```

### Application Deployment

```yaml
# webapp-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
  namespace: production
spec:
  replicas: 3
  selector:
    matchLabels:
      app: webapp
  template:
    metadata:
      labels:
        app: webapp
    spec:
      containers:
      - name: webapp
        image: webapp:latest
        env:
        # Database configuration from ConfigMap
        - name: DB_HOST
          valueFrom:
            configMapKeyRef:
              name: webapp-config
              key: database_endpoint
        - name: DB_PORT
          valueFrom:
            configMapKeyRef:
              name: webapp-config
              key: database_port
        - name: DB_NAME
          valueFrom:
            configMapKeyRef:
              name: webapp-config
              key: database_name
        # Database password from Secret
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: webapp-secrets
              key: database_password
        # Cache configuration from ConfigMap
        - name: CACHE_HOST
          valueFrom:
            configMapKeyRef:
              name: webapp-config
              key: cache_endpoint
        - name: CACHE_PORT
          valueFrom:
            configMapKeyRef:
              name: webapp-config
              key: cache_port
```

## Example 2: Multi-Service Application

For applications with multiple services that need different parts of the infrastructure:

### Terraform Outputs

```hcl
# Infrastructure outputs
output "vpc_id" {
  value = aws_vpc.main.id
}

output "private_subnet_ids" {
  value = aws_subnet.private[*].id
}

output "public_subnet_ids" {
  value = aws_subnet.public[*].id
}

output "database_endpoint" {
  value = aws_rds_instance.main.endpoint
}

output "database_password" {
  value     = aws_rds_instance.main.password
  sensitive = true
}

output "api_gateway_url" {
  value = aws_api_gateway_deployment.main.invoke_url
}

output "s3_bucket_name" {
  value = aws_s3_bucket.uploads.bucket
}

output "cloudfront_domain" {
  value = aws_cloudfront_distribution.main.domain_name
}
```

### Shared Infrastructure Config

```yaml
# shared-infrastructure.yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: shared-infrastructure
  namespace: infrastructure
spec:
  syncInterval: 10m
  backends:
  - s3:
      bucket: terraform-state-prod
      key: infrastructure/shared/terraform.tfstate
      region: us-west-2
  target:
    namespace: infrastructure
    configMapName: shared-config
    secretName: shared-secrets
```

### API Service Config

```yaml
# api-service.yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: api-service-config
  namespace: api
spec:
  syncInterval: 5m
  backends:
  - s3:
      bucket: terraform-state-prod
      key: infrastructure/shared/terraform.tfstate
      region: us-west-2
  target:
    namespace: api
    configMapName: api-config
    secretName: api-secrets
```

### API Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-service
  namespace: api
spec:
  replicas: 2
  selector:
    matchLabels:
      app: api-service
  template:
    spec:
      containers:
      - name: api
        image: api-service:latest
        env:
        - name: DATABASE_URL
          valueFrom:
            configMapKeyRef:
              name: api-config
              key: database_endpoint
        - name: DATABASE_PASSWORD
          valueFrom:
            secretKeyRef:
              name: api-secrets
              key: database_password
        - name: S3_BUCKET
          valueFrom:
            configMapKeyRef:
              name: api-config
              key: s3_bucket_name
```

### Frontend Service Config

```yaml
# frontend-service.yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: frontend-config
  namespace: frontend
spec:
  syncInterval: 5m
  backends:
  - s3:
      bucket: terraform-state-prod
      key: infrastructure/shared/terraform.tfstate
      region: us-west-2
  target:
    namespace: frontend
    configMapName: frontend-config
    secretName: frontend-secrets
```

### Frontend Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
  namespace: frontend
spec:
  replicas: 3
  selector:
    matchLabels:
      app: frontend
  template:
    spec:
      containers:
      - name: frontend
        image: frontend:latest
        env:
        - name: REACT_APP_API_URL
          valueFrom:
            configMapKeyRef:
              name: frontend-config
              key: api_gateway_url
        - name: REACT_APP_CDN_URL
          valueFrom:
            configMapKeyRef:
              name: frontend-config
              key: cloudfront_domain
```

## Example 3: Environment-Specific Configuration

Managing different environments with environment-specific outputs:

### Development Environment

```yaml
# dev-config.yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: app-config
  namespace: development
  labels:
    environment: development
spec:
  syncInterval: 2m  # Faster sync for development
  backends:
  - s3:
      bucket: terraform-state-dev
      key: app/terraform.tfstate
      region: us-west-2
  target:
    namespace: development
    configMapName: app-config
    secretName: app-secrets
```

### Staging Environment

```yaml
# staging-config.yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: app-config
  namespace: staging
  labels:
    environment: staging
spec:
  syncInterval: 5m
  backends:
  - s3:
      bucket: terraform-state-staging
      key: app/terraform.tfstate
      region: us-west-2
  target:
    namespace: staging
    configMapName: app-config
    secretName: app-secrets
```

### Production Environment

```yaml
# prod-config.yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: app-config
  namespace: production
  labels:
    environment: production
spec:
  syncInterval: 15m  # Slower sync for production stability
  backends:
  - s3:
      bucket: terraform-state-prod
      key: app/terraform.tfstate
      region: us-west-2
      role: arn:aws:iam::123456789012:role/tfout-prod-reader
  target:
    namespace: production
    configMapName: app-config
    secretName: app-secrets
```

## Example 4: ConfigMap and Secret Usage Patterns

### Using envFrom for Bulk Configuration

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  template:
    spec:
      containers:
      - name: app
        image: app:latest
        # Load all non-sensitive config as environment variables
        envFrom:
        - configMapRef:
            name: app-config
        - secretRef:
            name: app-secrets
        # Override specific values if needed
        env:
        - name: LOG_LEVEL
          value: "info"
```

### Mounting as Files

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  template:
    spec:
      containers:
      - name: app
        image: app:latest
        volumeMounts:
        - name: config
          mountPath: /etc/config
          readOnly: true
        - name: secrets
          mountPath: /etc/secrets
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: app-config
      - name: secrets
        secret:
          secretName: app-secrets
```

## Monitoring Your Configurations

### Check TerraformOutputs Status

```bash
# List all TerraformOutputs resources
kubectl get terraformoutputs --all-namespaces

# Get detailed status
kubectl describe terraformoutputs webapp-infrastructure -n production

# Watch for changes
kubectl get terraformoutputs -w
```

### Verify Created Resources

```bash
# Check ConfigMap
kubectl get configmap webapp-config -n production -o yaml

# Check Secret (decode values)
kubectl get secret webapp-secrets -n production -o jsonpath='{.data.database_password}' | base64 -d

# List all keys in ConfigMap
kubectl get configmap webapp-config -n production -o jsonpath='{.data}' | jq 'keys'
```

### Monitor Sync Events

```bash
# View events related to TerraformOutputs
kubectl get events --field-selector involvedObject.kind=TerraformOutputs -n production

# Check operator logs
kubectl logs -n tfout deployment/tfout-controller-manager -f
```

## Best Practices from Examples

1. **Use descriptive names** that indicate the application and environment
2. **Set appropriate sync intervals** based on environment needs
3. **Use labels** for organization and filtering
4. **Separate sensitive and non-sensitive** outputs appropriately
5. **Use namespace isolation** for different environments
6. **Monitor the sync status** regularly
7. **Test configuration changes** in development first

## Troubleshooting Common Issues

### No Outputs Found

```bash
# Check if Terraform state has outputs
aws s3 cp s3://my-terraform-state/app/terraform.tfstate - | jq .outputs

# Verify backend configuration
kubectl describe terraformoutputs webapp-infrastructure -n production
```

### Permission Errors

```bash
# Test S3 access manually
aws s3 ls s3://my-terraform-state/app/terraform.tfstate

# Check operator logs for detailed errors
kubectl logs -n tfout deployment/tfout-controller-manager --tail=50
```

### Sync Not Working

```bash
# Force a sync by updating the resource
kubectl annotate terraformoutputs webapp-infrastructure -n production force-sync="$(date)"

# Check the last sync time
kubectl get terraformoutputs webapp-infrastructure -n production -o jsonpath='{.status.lastSyncTime}'
```