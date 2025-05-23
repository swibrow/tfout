# Helm Chart Deployment

The TFOut Helm chart provides a flexible and production-ready way to deploy the operator with customizable configuration options.

## Chart Information

- **Chart Name**: `tfout`
- **Repository**: `https://swibrow.github.io/tfout`
- **Minimum Kubernetes Version**: 1.19+
- **Minimum Helm Version**: 3.0+

## Installation

### Add Repository

```bash
helm repo add tfout https://swibrow.github.io/tfout
helm repo update
```

### Basic Installation

```bash
helm install tfout tfout/tfout --namespace tfout-system --create-namespace
```

### Custom Installation

Create a `values.yaml` file with your configuration:

```yaml
# values.yaml
image:
  repository: ghcr.io/swibrow/tfout
  tag: "v0.1.0"
  pullPolicy: IfNotPresent

controller:
  replicas: 2
  leaderElection: true
  metricsBindAddress: ":8080"
  healthProbeBindAddress: ":8081"

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

# AWS configuration
env:
  - name: AWS_REGION
    value: "us-west-2"

envFrom:
  - secretRef:
      name: aws-credentials

serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/tfout-role

metrics:
  enabled: true
  port: 8080

rbac:
  create: true

crds:
  install: true
```

Install with custom values:

```bash
helm install tfout tfout/tfout \
  --namespace tfout-system \
  --create-namespace \
  --values values.yaml
```

## Configuration Options

### Image Configuration

```yaml
image:
  repository: ghcr.io/swibrow/tfout  # Container image repository
  tag: "v0.1.0"                     # Image tag (defaults to chart appVersion)
  pullPolicy: IfNotPresent          # Image pull policy
```

### Controller Configuration

```yaml
controller:
  replicas: 1                       # Number of controller replicas
  leaderElection: true              # Enable leader election for HA
  metricsBindAddress: ":8080"       # Metrics server bind address
  healthProbeBindAddress: ":8081"   # Health probe bind address
  secureMetrics: false              # Enable secure metrics serving
  enableHTTP2: false                # Enable HTTP/2
  development: false                # Enable development mode logging
  logLevel: "info"                  # Log level (info, debug, error)
```

### Resource Management

```yaml
resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

# Horizontal Pod Autoscaling
autoscaling:
  enabled: false
  minReplicas: 1
  maxReplicas: 10
  targetCPUUtilizationPercentage: 80
```

### Environment Variables

```yaml
# Direct environment variables
env:
  - name: AWS_REGION
    value: "us-west-2"
  - name: LOG_LEVEL
    value: "debug"
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef:
        name: aws-credentials
        key: access-key-id

# Environment from ConfigMaps/Secrets
envFrom:
  - configMapRef:
      name: tfout-config
  - secretRef:
      name: aws-credentials
```

### Service Account and RBAC

```yaml
serviceAccount:
  create: true                      # Create service account
  automount: true                   # Automount service account token
  name: ""                          # Service account name (auto-generated if empty)
  annotations:                      # Service account annotations
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/tfout-role

rbac:
  create: true                      # Create RBAC resources
```

### Security Context

```yaml
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 65532
```

### Monitoring and Metrics

```yaml
metrics:
  enabled: true                     # Enable metrics endpoint
  port: 8080                        # Metrics port
  path: /metrics                    # Metrics path

service:
  type: ClusterIP                   # Service type
  port: 8080                        # Service port
  targetPort: 8080                  # Target port
```

### Custom Resource Definitions

```yaml
crds:
  install: true                     # Install CRDs with chart
  keep: false                       # Keep CRDs on uninstall
```

### Pod Scheduling

```yaml
nodeSelector: {}

tolerations: []

affinity: {}

priorityClassName: ""               # Priority class for pods

terminationGracePeriodSeconds: 10   # Termination grace period
```

### Volumes and Volume Mounts

```yaml
volumes:
  - name: custom-config
    configMap:
      name: tfout-config

volumeMounts:
  - name: custom-config
    mountPath: /etc/config
    readOnly: true
```

## Production Configuration Example

Here's a complete production-ready configuration:

```yaml
# production-values.yaml
image:
  repository: ghcr.io/swibrow/tfout
  tag: "v0.1.0"
  pullPolicy: IfNotPresent

controller:
  replicas: 3
  leaderElection: true
  metricsBindAddress: ":8080"
  healthProbeBindAddress: ":8081"
  logLevel: "info"

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 5
  targetCPUUtilizationPercentage: 70

serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/tfout-production

env:
  - name: AWS_REGION
    value: "us-west-2"

metrics:
  enabled: true
  port: 8080

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - tfout
        topologyKey: kubernetes.io/hostname

tolerations:
  - key: "node-role.kubernetes.io/control-plane"
    operator: "Exists"
    effect: "NoSchedule"

priorityClassName: "system-cluster-critical"

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 65532
```

## Upgrade

### Check for Updates

```bash
helm repo update
helm search repo tfout/tfout --versions
```

### Upgrade

```bash
helm upgrade tfout tfout/tfout \
  --namespace tfout-system \
  --values production-values.yaml
```

### Rollback

```bash
# List releases
helm history tfout --namespace tfout-system

# Rollback to previous version
helm rollback tfout 1 --namespace tfout-system
```

## Uninstallation

```bash
# Uninstall the release
helm uninstall tfout --namespace tfout-system

# Clean up namespace (if desired)
kubectl delete namespace tfout-system

# Remove CRDs (if crds.keep=false)
kubectl delete crd terraformoutputs.tfout.wibrow.net
```

## Helm Chart Development

For chart development and customization:

```bash
# Clone the repository
git clone https://github.com/swibrow/tfout.git
cd tfout

# Render templates locally
helm template tfout ./charts/tfout --values test-values.yaml

# Validate chart
helm lint ./charts/tfout

# Package chart
helm package ./charts/tfout
```

## Troubleshooting

### Common Issues

1. **CRD Installation Failures**
   ```bash
   # Check if CRDs already exist
   kubectl get crd terraformoutputs.tfout.wibrow.net

   # Manual CRD installation
   helm install tfout tfout/tfout --skip-crds
   ```

2. **Permission Issues**
   ```bash
   # Check service account
   kubectl get serviceaccount -n tfout-system

   # Check RBAC
   kubectl auth can-i create configmaps --as=system:serviceaccount:tfout-system:tfout
   ```

3. **Image Pull Issues**
   ```bash
   # Check image pull secrets
   kubectl get pods -n tfout-system
   kubectl describe pod <pod-name> -n tfout-system
   ```

### Debug Configuration

```yaml
# debug-values.yaml
controller:
  development: true
  logLevel: "debug"

env:
  - name: GOMAXPROCS
    value: "1"
  - name: GOMEMLIMIT
    value: "100Mi"
```