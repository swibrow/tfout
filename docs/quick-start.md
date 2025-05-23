# Quick Start

This guide will walk you through setting up TFOut and creating your first TerraformOutputs resource.

## Step 1: Prepare Your Terraform State

Ensure you have a Terraform state file with outputs. For example:

```hcl
# main.tf
output "database_endpoint" {
  value = aws_rds_instance.example.endpoint
  description = "The RDS instance endpoint"
}

output "database_password" {
  value = aws_rds_instance.example.password
  description = "The RDS instance password"
  sensitive = true
}

output "api_url" {
  value = "https://api.${var.domain}"
  description = "The API endpoint URL"
}
```

After running `terraform apply`, your state file will contain these outputs.

## Step 2: Configure Backend Access

TFOut needs access to your Terraform state backend. For S3, you can use:

### Option A: IAM Roles for Service Accounts (IRSA) - Recommended for EKS

```yaml
# Create an IAM role with S3 read permissions and associate it with the service account
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tfout-controller-manager
  namespace: tfout
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT-ID:role/tfout-s3-reader
```

### Option B: AWS Credentials Secret

```bash
kubectl create secret generic aws-credentials \
  --namespace tfout \
  --from-literal=AWS_ACCESS_KEY_ID=AKIA... \
  --from-literal=AWS_SECRET_ACCESS_KEY=...
```

Update your Helm values:

```yaml
envFrom:
  - secretRef:
      name: aws-credentials
```

## Step 3: Create a TerraformOutputs Resource

Create a file called `terraform-outputs.yaml`:

```yaml
apiVersion: tfout.wibrow.net/v1alpha1
kind: TerraformOutputs
metadata:
  name: my-app-infrastructure
  namespace: default
spec:
  # Sync every 5 minutes
  syncInterval: 5m

  # Backend configuration
  backends:
  - s3:
      bucket: my-terraform-state-bucket
      key: environments/production/terraform.tfstate
      region: us-west-2
      # Optional: specify IAM role to assume
      # role: arn:aws:iam::123456789012:role/terraform-reader

  # Target resources
  target:
    namespace: default
    configMapName: app-config
    secretName: app-secrets
```

Apply the resource:

```bash
kubectl apply -f terraform-outputs.yaml
```

## Step 4: Verify the Sync

Check the status of your TerraformOutputs resource:

```bash
kubectl get terraformoutputs my-app-infrastructure -o yaml
```

You should see a status section like:

```yaml
status:
  syncStatus: Success
  outputCount: 3
  lastSyncTime: "2024-01-15T10:30:00Z"
  message: "Successfully synced 3 outputs"
```

## Step 5: View the Created Resources

Check the ConfigMap for non-sensitive outputs:

```bash
kubectl get configmap app-config -o yaml
```

Expected output:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  database_endpoint: "mydb.cluster-xyz.us-west-2.rds.amazonaws.com"
  api_url: "https://api.example.com"
```

Check the Secret for sensitive outputs:

```bash
kubectl get secret app-secrets -o yaml
```

Expected output:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
type: Opaque
data:
  database_password: <base64-encoded-password>
```

## Step 6: Use in Your Applications

Now you can reference these resources in your application deployments:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  template:
    spec:
      containers:
      - name: app
        image: my-app:latest
        env:
        # From ConfigMap
        - name: DATABASE_ENDPOINT
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: database_endpoint
        - name: API_URL
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: api_url
        # From Secret
        - name: DATABASE_PASSWORD
          valueFrom:
            secretKeyRef:
              name: app-secrets
              key: database_password
```

## Monitoring the Sync

You can monitor the sync process using:

```bash
# Watch the TerraformOutputs resource
kubectl get terraformoutputs -w

# Check operator logs
kubectl logs -n tfout deployment/tfout-controller-manager -f

# View events
kubectl get events --field-selector involvedObject.name=my-app-infrastructure
```

## Next Steps

- Learn about [advanced configuration options](configuration/terraformoutputs.md)
- Explore [multiple backend scenarios](examples/basic.md)
- Review [troubleshooting tips](reference/troubleshooting.md)

## Common Issues

### Permission Errors

If you see permission errors, ensure your AWS credentials have the necessary S3 permissions:

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
      "Resource": "arn:aws:s3:::my-terraform-state-bucket/*"
    }
  ]
}
```

### Sync Failures

Check the TerraformOutputs status and operator logs for detailed error messages. Common issues include:

- Incorrect bucket/key paths
- Missing credentials
- Network connectivity issues
- Invalid Terraform state format