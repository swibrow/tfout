# Backend Configuration

TFOut supports multiple backend types for fetching Terraform state files. Currently, S3 is fully supported, with plans for additional backends.

## S3 Backend

The S3 backend is the primary supported backend for TFOut, compatible with AWS S3 and S3-compatible storage systems.

### Basic Configuration

```yaml
backends:
- s3:
    bucket: my-terraform-state
    key: path/to/terraform.tfstate
    region: us-west-2
```

### Full Configuration Options

```yaml
backends:
- s3:
    bucket: my-terraform-state        # Required: S3 bucket name
    key: path/to/terraform.tfstate    # Required: Object key/path
    region: us-west-2                 # Required: AWS region
    endpoint: https://s3.amazonaws.com # Optional: Custom S3 endpoint
    role: arn:aws:iam::123:role/name  # Optional: IAM role to assume
```

#### Field Descriptions

- **`bucket`** (required): The S3 bucket containing the Terraform state file
- **`key`** (required): The object key (path) to the Terraform state file within the bucket
- **`region`** (required): The AWS region where the bucket is located
- **`endpoint`** (optional): Custom S3 endpoint for S3-compatible storage systems
- **`role`** (optional): IAM role ARN to assume for accessing the bucket

### Authentication

TFOut supports multiple authentication methods for S3:

#### 1. IAM Roles for Service Accounts (IRSA) - Recommended for EKS

```yaml
# Service account with IRSA annotation
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/tfout-s3-reader

# Backend configuration (no role needed)
backends:
- s3:
    bucket: my-terraform-state
    key: terraform.tfstate
    region: us-west-2
```

#### 2. IAM Role Assumption

```yaml
backends:
- s3:
    bucket: my-terraform-state
    key: terraform.tfstate
    region: us-west-2
    role: arn:aws:iam::123456789012:role/terraform-reader
```

#### 3. Environment Variables

```yaml
# Set via Helm values or directly
env:
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef:
        name: aws-credentials
        key: access-key-id
  - name: AWS_SECRET_ACCESS_KEY
    valueFrom:
      secretKeyRef:
        name: aws-credentials
        key: secret-access-key

backends:
- s3:
    bucket: my-terraform-state
    key: terraform.tfstate
    region: us-west-2
```

### S3-Compatible Storage

TFOut works with S3-compatible storage systems like MinIO, DigitalOcean Spaces, etc.:

```yaml
backends:
- s3:
    bucket: my-bucket
    key: terraform.tfstate
    region: us-east-1
    endpoint: https://nyc3.digitaloceanspaces.com
```

### Multiple S3 Backends

You can specify multiple S3 backends to merge outputs from different state files:

```yaml
backends:
- s3:
    bucket: infrastructure-state
    key: vpc/terraform.tfstate
    region: us-west-2
- s3:
    bucket: infrastructure-state
    key: database/terraform.tfstate
    region: us-west-2
- s3:
    bucket: application-state
    key: api/terraform.tfstate
    region: us-west-2
```

Output merging behavior:
- Outputs from later backends override outputs from earlier backends
- Use this for layered configuration where application-specific outputs override infrastructure defaults

## Planned Backends

The following backends are planned for future releases:

### Google Cloud Storage (GCS)

```yaml
# Future support
backends:
- gcs:
    bucket: my-terraform-state
    object: path/to/terraform.tfstate
    project: my-gcp-project
    serviceAccountKey: /path/to/key.json
```

### Azure Blob Storage

```yaml
# Future support  
backends:
- azure:
    storageAccount: mystorageaccount
    container: terraform-state
    key: terraform.tfstate
    resourceGroup: my-resource-group
```

### HTTP Backend

```yaml
# Future support
backends:
- http:
    url: https://terraform-state.example.com/state
    username: user
    password: pass
    headers:
      Authorization: Bearer token
```

## Backend Selection Strategy

When choosing backends, consider:

### Security
- Use IRSA when possible for EKS
- Prefer IAM role assumption over static credentials
- Ensure least-privilege access (read-only)

### Performance
- Choose regions close to your Kubernetes cluster
- Consider S3 Transfer Acceleration for cross-region access
- Use appropriate sync intervals based on backend latency

### Cost
- Minimize cross-region data transfer
- Consider S3 request costs with sync frequency
- Use lifecycle policies for versioned state files

## Best Practices

### 1. State File Organization

Organize your state files logically:

```
bucket/
├── environments/
│   ├── production/
│   │   ├── infrastructure/terraform.tfstate
│   │   └── applications/terraform.tfstate
│   └── staging/
│       ├── infrastructure/terraform.tfstate
│       └── applications/terraform.tfstate
└── shared/
    └── dns/terraform.tfstate
```

### 2. Access Control

Create specific IAM policies for TFOut:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:HeadObject"
      ],
      "Resource": [
        "arn:aws:s3:::my-terraform-state/*"
      ]
    },
    {
      "Effect": "Allow", 
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::my-terraform-state"
      ]
    }
  ]
}
```

### 3. Cross-Account Access

For cross-account state access:

```yaml
backends:
- s3:
    bucket: cross-account-state
    key: shared/terraform.tfstate
    region: us-west-2
    role: arn:aws:iam::OTHER-ACCOUNT:role/tfout-cross-account-reader
```

### 4. Error Handling

Configure appropriate retry and timeout behavior:

```yaml
# Helm values
controller:
  # Faster sync for development
  syncInterval: 1m
  
# For production, use longer intervals
controller:
  syncInterval: 15m
```

## Troubleshooting

### Common Issues

#### 1. Access Denied

```bash
# Check IAM permissions
aws sts get-caller-identity
aws s3 ls s3://my-terraform-state/

# Check TFOut logs
kubectl logs -n tfout-system deployment/tfout-controller-manager
```

#### 2. Invalid State Format

```bash
# Verify state file format
aws s3 cp s3://my-terraform-state/terraform.tfstate - | jq .
```

#### 3. Network Issues

```bash
# Test connectivity from pod
kubectl run test-pod --image=amazon/aws-cli --rm -it -- aws s3 ls s3://my-terraform-state/
```

#### 4. Role Assumption Issues

```bash
# Test role assumption
aws sts assume-role \
  --role-arn arn:aws:iam::123456789012:role/tfout-reader \
  --role-session-name test-session
```

### Debug Configuration

Enable debug logging to troubleshoot backend issues:

```yaml
# Helm values
controller:
  development: true
  logLevel: debug

env:
  - name: AWS_SDK_LOAD_CONFIG
    value: "true"
```

## Performance Considerations

### Sync Frequency

Balance between freshness and cost:

```yaml
# Development: Fast feedback
syncInterval: 1m

# Staging: Moderate
syncInterval: 5m  

# Production: Conservative
syncInterval: 15m
```

### Large State Files

For large state files (>1MB):
- Consider splitting into multiple smaller state files
- Use appropriate resource limits
- Monitor memory usage

### High Availability

For HA deployments:
- Enable leader election
- Use multiple replicas
- Consider regional deployment strategies

```yaml
controller:
  replicas: 3
  leaderElection: true
  
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        topologyKey: kubernetes.io/hostname
```