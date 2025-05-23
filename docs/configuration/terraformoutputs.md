# TerraformOutputs CRD

The `TerraformOutputs` Custom Resource Definition (CRD) is the primary way to configure TFOut. This page provides a comprehensive reference for all available configuration options.

## Basic Structure

```yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: <resource-name>
  namespace: <namespace>
spec:
  syncInterval: <duration>
  backends: []
  target: {}
status:
  # Populated by the operator
```

## Spec Fields

### `syncInterval`

**Type**: `string` (duration)
**Default**: `5m`
**Required**: No

Controls how often TFOut checks for changes in the Terraform state.

```yaml
spec:
  syncInterval: 10m  # Check every 10 minutes
```

Valid duration formats:
- `30s` - 30 seconds
- `5m` - 5 minutes
- `1h` - 1 hour
- `24h` - 24 hours

### `backends`

**Type**: `[]BackendSpec`
**Required**: Yes
**Minimum**: 1 backend

List of backend configurations to fetch Terraform state from. Multiple backends can be specified, and their outputs will be merged (with later backends taking precedence for conflicting keys).

```yaml
spec:
  backends:
  - s3:
      bucket: primary-state-bucket
      key: app/terraform.tfstate
      region: us-west-2
  - s3:
      bucket: secondary-state-bucket
      key: database/terraform.tfstate
      region: us-east-1
```

See [Backends](backends.md) for detailed backend configuration options.

### `target`

**Type**: `TargetSpec`
**Required**: Yes

Defines where the extracted outputs should be stored in Kubernetes.

```yaml
spec:
  target:
    namespace: production
    configMapName: app-config
    secretName: app-secrets
```

#### Target Fields

- **`namespace`** (string, default: `default`): Target namespace for ConfigMap/Secret
- **`configMapName`** (string, required): Name for the ConfigMap containing non-sensitive outputs
- **`secretName`** (string, required): Name for the Secret containing sensitive outputs

## Status Fields

The status section is managed by TFOut and provides information about the sync process:

### `syncStatus`

**Type**: `enum`
**Values**: `Success`, `Failed`, `InProgress`

Current sync status.

### `outputCount`

**Type**: `integer`

Total number of outputs found across all backends.

### `lastSyncTime`

**Type**: `timestamp`

When the last successful sync occurred.

### `message`

**Type**: `string`

Human-readable status message with additional details.

### `conditions`

**Type**: `[]Condition`

Detailed condition information following Kubernetes conventions.

## Complete Example

```yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: multi-environment-config
  namespace: production
  labels:
    app: myapp
    environment: production
spec:
  # Check for changes every 2 minutes
  syncInterval: 2m

  # Multiple backends for different components
  backends:
  - s3:
      bucket: terraform-state-prod
      key: infrastructure/vpc/terraform.tfstate
      region: us-west-2
      role: arn:aws:iam::123456789012:role/terraform-reader
  - s3:
      bucket: terraform-state-prod
      key: infrastructure/database/terraform.tfstate
      region: us-west-2
      role: arn:aws:iam::123456789012:role/terraform-reader
  - s3:
      bucket: terraform-state-prod
      key: applications/api/terraform.tfstate
      region: us-west-2
      role: arn:aws:iam::123456789012:role/terraform-reader

  # Target resources
  target:
    namespace: production
    configMapName: infrastructure-config
    secretName: infrastructure-secrets

# Status is managed by the operator
status:
  syncStatus: Success
  outputCount: 15
  lastSyncTime: "2024-01-15T10:30:00Z"
  message: "Successfully synced 15 outputs from 3 backends"
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2024-01-15T10:30:00Z"
    reason: SyncSuccessful
    message: "All backends synced successfully"
```

## Output Processing

TFOut processes Terraform outputs as follows:

1. **Extraction**: Reads the `outputs` section from each Terraform state file
2. **Merging**: Combines outputs from multiple backends (later backends override earlier ones)
3. **Sensitivity Detection**: Checks the `sensitive` flag in the Terraform output definition
4. **Resource Creation**:
   - Non-sensitive outputs → ConfigMap
   - Sensitive outputs → Secret (base64 encoded)

## Validation

The CRD includes validation rules:

- At least one backend must be specified
- Target namespace, configMapName, and secretName are required
- Backend configurations must be valid for their type
- Sync interval must be a valid duration

## Best Practices

### Naming

Use descriptive names that indicate the purpose and environment:

```yaml
metadata:
  name: production-database-config
  # Better than: db-config
```

### Labels

Add labels for better organization:

```yaml
metadata:
  labels:
    app: myapp
    component: database
    environment: production
    managed-by: tfout
```

### Sync Intervals

Choose appropriate sync intervals based on your needs:

- **Development**: `1m` - Fast feedback
- **Staging**: `5m` - Balance of freshness and load
- **Production**: `10m` or `15m` - Reduce API calls, stable configs

### Multiple Backends

When using multiple backends, order them by precedence (most important last):

```yaml
backends:
- s3: # Base infrastructure (lowest precedence)
    bucket: infra-state
    key: vpc/terraform.tfstate
- s3: # Application-specific overrides (highest precedence)
    bucket: app-state
    key: myapp/terraform.tfstate
```

## Troubleshooting

Common configuration issues:

1. **Permission errors**: Ensure the operator has access to the specified backends
2. **Sync failures**: Check that bucket/key paths are correct
3. **Missing outputs**: Verify that your Terraform configuration includes outputs
4. **Conflicts**: When using multiple backends, later ones override earlier ones

See [Troubleshooting](../reference/troubleshooting.md) for more detailed guidance.