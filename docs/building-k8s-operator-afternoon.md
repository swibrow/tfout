# Building a Kubernetes Operator for the sake a building a Kubernetes Operator

At my current $job, we recently moved to ArgoCD from Terraform for application deployments :pray:. And with that came the issue of passing terraform outputs into helm inputs. e.g. IAM Role ARN / ECR Image repository. So I spent my Saturday afternoon building a Kubernetes operator to sync Terraform outputs from S3 into Kubernetes ConfigMaps and Secrets.

## What I Built

TFOut - a simple operator that syncs Terraform outputs from S3 into Kubernetes ConfigMaps and Secrets. Nothing groundbreaking, but useful enough for real workflows.

Here's what it looks like in practice:

```yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: my-terraform-outputs
spec:
  backends:
    - s3:
        bucket: my-terraform-state
        key: prod/terraform.tfstate
        region: eu-west-1
        roleArn: arn:aws:iam::123456789012:role/terraform-sync-role
  syncInterval: 5m
  targetNamespace: production
```

And there you go, all your Terraform outputs are now available as ConfigMaps and Secrets in Kubernetes. Sensitive outputs automatically go into Secrets, the rest into ConfigMaps.

## The Complete Package

What you get in an afternoon:
- The operator code (Go + Kubebuilder)
- Helm charts for deployment
- Full documentation with mkdocs
- GitHub Actions for CI/CD
- E2E tests with Kind
- Prometheus metrics + Grafana dashboards
- Proper RBAC setup

## How It Works

The operator watches for `TerraformOutputs` resources and:
1. Fetches the Terraform state from S3
2. Parses all outputs
3. Creates/updates ConfigMaps for non-sensitive values
4. Creates/updates Secrets for sensitive values
5. Handles multiple backends with proper merging

Example output:
```bash
$ kubectl get configmap my-app-outputs -o yaml
data:
  api_endpoint: https://api.example.com
  cdn_domain: cdn.example.com
  database_host: postgres.internal.example.com
```

## The Development Process

Started with Kubebuilder scaffolding, then focused on the actual business logic. The reconciliation loop is straightforward:

```go
func (r *TerraformOutputsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Fetch the TerraformOutputs resource
    // For each backend, download state from S3
    // Parse outputs, separate sensitive/non-sensitive
    // Create or update ConfigMaps and Secrets
    // Update status with sync info
}
```

Used AI assistance throughout to handle the boilerplate - especially useful for helm templates, GitHub Actions workflows, and test setup.

## Production Ready Features

- **Change Detection**: Uses S3 ETags to avoid unnecessary syncs
- **Multi-Backend Support**: Merge outputs from multiple Terraform states
- **Metrics**: Sync duration, error counts, output counts - all exposed for Prometheus
- **Security**: IAM roles for S3 access, proper RBAC, sensitive data handling
- **Testing**: Unit tests + E2E tests that spin up a Kind cluster

## Key Takeaways

1. **Kubernetes operators aren't that scary** when you have the right tools and approach.

2. **Start with the MVP, then iterate**. Basic sync working? Add metrics. Metrics done? Add multi-backend support.

3. **The ecosystem provides so much**. Kubebuilder gives you the structure, controller-runtime handles the heavy lifting.

## Try It Yourself

If you've been putting off building that operator or controller - maybe give it a shot. The Kubernetes ecosystem has matured enough that you can build production-grade tools quickly.

The code is here if you want to check it out: [github.com/swibrow/tfout](https://github.com/swibrow/tfout)

Install with Helm:
```bash
helm install tfout oci://ghcr.io/swibrow/charts/tfout
```