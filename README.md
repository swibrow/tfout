# Terraform Outputs Operator

A Kubernetes operator that automatically syncs Terraform outputs from S3 backends into ConfigMaps and Secrets.

## Description

The Terraform Outputs Operator monitors Terraform state files stored in S3 buckets and automatically creates or updates Kubernetes ConfigMaps and Secrets with the output values. It supports multiple S3 backends, allowing you to aggregate outputs from different Terraform deployments into a single location in your Kubernetes cluster.

### Features

- **Multiple Backend Support**: Configure multiple S3 backends to aggregate outputs from different Terraform states
- **Automatic Detection**: Uses S3 ETags to detect changes and only sync when necessary
- **Sensitive Data Handling**: Automatically separates sensitive and non-sensitive outputs into Secrets and ConfigMaps
- **Conflict Resolution**: Handles output key conflicts when merging from multiple backends
- **Efficient Syncing**: Configurable sync intervals with change detection to minimize API calls

## CI/CD

This project uses GitHub Actions for continuous integration and deployment:

### Workflows

- **CI** (`.github/workflows/ci.yaml`): Runs tests, linting, builds, and pushes container images on every push and PR
- **Release** (`.github/workflows/release.yaml`): Creates releases with artifacts when tags are pushed
- **Security** (`.github/workflows/security.yaml`): Runs security scans including govulncheck, gosec, and CodeQL
- **E2E Tests** (`.github/workflows/e2e.yaml`): Runs end-to-end tests in a Kind cluster
- **Update Installer** (`.github/workflows/update-installer.yaml`): Automatically updates installer manifests on releases

### Container Images

Images are automatically built and pushed to GitHub Container Registry:
- `ghcr.io/swibrow/tf-outputs-operator:latest` - Latest main branch
- `ghcr.io/swibrow/tf-outputs-operator:v*` - Release tags

## Getting Started

### Prerequisites
- kubectl version v1.11.3+
- Access to a Kubernetes v1.11.3+ cluster
- AWS credentials configured for S3 access (see AWS Configuration below)

### Installation

#### Option 1: Install from GitHub Releases (Recommended)

Install the latest release directly from GitHub:

```bash
kubectl apply -f https://github.com/swibrow/tf-outputs-operator/releases/latest/download/install.yaml
```

Or install a specific version:

```bash
kubectl apply -f https://github.com/swibrow/tf-outputs-operator/releases/download/v0.1.0/install.yaml
```

#### Option 2: Build and Deploy from Source

**Prerequisites for building from source:**
- go version v1.21.0+
- docker version 17.03+

**Build and push your image:**

```sh
make docker-build docker-push IMG=swibrow/tf-outputs-operator:tag
```

**Install the CRDs:**

```sh
make install
```

**Deploy the operator:**

```sh
make deploy IMG=swibrow/tf-outputs-operator:tag
```

### AWS Configuration

The operator needs AWS credentials to access S3 buckets. You can configure this in several ways:

1. **IAM Roles for Service Accounts (IRSA)** - Recommended for EKS
2. **Instance profiles** - For EC2-based clusters
3. **AWS credentials file or environment variables**

Example IRSA setup:
```bash
# Create an IAM role with S3 read permissions and associate it with the service account
kubectl annotate serviceaccount tf-outputs-operator-controller-manager \
  -n tf-outputs-operator-system \
  eks.amazonaws.com/role-arn=arn:aws:iam::ACCOUNT:role/tf-outputs-operator-role
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

### Usage

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

**Example TerraformOutputs resource:**

```yaml
apiVersion: outputs.tfoutputs.io/v1alpha1
kind: TerraformOutputs
metadata:
  name: my-terraform-outputs
  namespace: default
spec:
  backends:
    - type: "s3"
      source:
        bucket: "my-terraform-state-bucket"
        key: "prod/terraform.tfstate"
        region: "eu-central-1"
    - type: "s3"
      source:
        bucket: "my-terraform-state-bucket"
        key: "shared/terraform.tfstate"
        region: "eu-central-1"
  syncInterval: "5m"
  target:
    namespace: "default"
    configMapName: "terraform-outputs"
    secretName: "terraform-secrets"
```

This will:
- Monitor both state files for changes every 5 minutes
- Create a ConfigMap named `terraform-outputs` with non-sensitive outputs
- Create a Secret named `terraform-secrets` with sensitive outputs
- Merge outputs from both backends (last backend wins on conflicts)

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/tf-outputs-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/tf-outputs-operator/<tag or branch>/dist/install.yaml
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.