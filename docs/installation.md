# Installation

TFOut can be installed in several ways depending on your preferences and environment.

## Prerequisites

- Kubernetes cluster (1.19+)
- `kubectl` configured to access your cluster
- Appropriate permissions to create CustomResourceDefinitions and RBAC resources

## Method 1: Helm Chart (Recommended)

The easiest way to install TFOut is using the Helm chart:

```bash
# Add the TFOut Helm repository
helm repo add tfout https://swibrow.github.io/tfout

# Update your local Helm chart repository cache
helm repo update

# Install TFOut
helm install tfout tfout/tfout --namespace tfout --create-namespace
```

### Customizing the Installation

Create a `values.yaml` file to customize the installation:

```yaml
# values.yaml
image:
  repository: ghcr.io/swibrow/tfout
  tag: "v0.1.0"

controller:
  replicas: 2
  leaderElection: true

resources:
  limits:
    cpu: 200m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 128Mi

# AWS credentials for S3 access
env:
  - name: AWS_REGION
    value: "us-west-2"

envFrom:
  - secretRef:
      name: aws-credentials
```

Then install with custom values:

```bash
helm install tfout tfout/tfout \
  --namespace tfout \
  --create-namespace \
  --values values.yaml
```

## Method 2: Kubernetes Manifests

You can install TFOut directly using Kubernetes manifests:

```bash
# Install CRDs and operator
kubectl apply -f https://github.com/swibrow/tfout/releases/latest/download/install.yaml
```

## Method 3: Build from Source

If you want to build and install from source:

```bash
# Clone the repository
git clone https://github.com/swibrow/tfout.git
cd tfout

# Build and install
make install
make deploy IMG=controller:latest
```

## Verification

Verify that TFOut is running correctly:

```bash
# Check the operator deployment
kubectl get deployments -n tfout

# Check the operator logs
kubectl logs -n tfout deployment/tfout-controller-manager

# Verify CRDs are installed
kubectl get crd terraformoutputs.tfout.wibrow.net
```

You should see output similar to:

```
NAME                        READY   UP-TO-DATE   AVAILABLE   AGE
tfout-controller-manager    1/1     1            1           2m
```

## Next Steps

- Follow the [Quick Start](quick-start.md) guide to create your first TerraformOutputs resource
- Review [Configuration](configuration/terraformoutputs.md) options

## Uninstallation

### Helm

```bash
helm uninstall tfout --namespace tfout
kubectl delete namespace tfout
```

### Kubernetes Manifests

```bash
kubectl delete -f https://github.com/swibrow/tfout/releases/latest/download/install.yaml
```

### From Source

```bash
make undeploy
make uninstall
```