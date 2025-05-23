# Troubleshooting

This page provides solutions to common issues you might encounter when using TFOut.

## Diagnostic Commands

Before diving into specific issues, these commands help gather diagnostic information:

```bash
# Check TerraformOutputs resources
kubectl get terraformoutputs --all-namespaces

# Get detailed status
kubectl describe terraformoutputs <name> -n <namespace>

# Check operator logs
kubectl logs -n tfout deployment/tfout-controller-manager --tail=100

# Check events
kubectl get events --field-selector involvedObject.kind=TerraformOutputs -n <namespace>

# Verify created resources
kubectl get configmaps,secrets -l managed-by=tfout -n <namespace>
```

## Common Issues

### 1. TerraformOutputs Resource Not Syncing

#### Symptoms
- `syncStatus` shows `Failed` or `InProgress`
- `lastSyncTime` is not updating
- No ConfigMap/Secret created

#### Diagnosis
```bash
kubectl describe terraformoutputs <name> -n <namespace>
kubectl logs -n tfout deployment/tfout-controller-manager --tail=50
```

#### Common Causes & Solutions

**Backend Access Issues**
```bash
# Test S3 access manually
aws s3 ls s3://your-bucket/your-key

# Check IAM permissions
aws sts get-caller-identity
```

**Invalid State File**
```bash
# Verify state file exists and is valid JSON
aws s3 cp s3://your-bucket/your-key - | jq .

# Check for outputs section
aws s3 cp s3://your-bucket/your-key - | jq .outputs
```

**Network Connectivity**
```bash
# Test from within cluster
kubectl run debug-pod --image=amazon/aws-cli --rm -it -- aws s3 ls s3://your-bucket/
```

### 2. Permission Denied Errors

#### Symptoms
- Error messages containing "Access Denied" or "403 Forbidden"
- Logs show authentication/authorization failures

#### Solutions

**For EKS with IRSA**
```bash
# Verify service account annotation
kubectl get serviceaccount tfout-controller-manager -n tfout -o yaml

# Check IAM role trust policy
aws iam get-role --role-name your-tfout-role
```

**For IAM Role Assumption**
```bash
# Test role assumption
aws sts assume-role \
  --role-arn arn:aws:iam::123456789012:role/your-role \
  --role-session-name test-session
```

**Required IAM Policy**
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
      "Resource": "arn:aws:s3:::your-bucket/*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": "arn:aws:s3:::your-bucket"
    }
  ]
}
```

### 3. ConfigMap/Secret Not Created

#### Symptoms
- TerraformOutputs shows `Success` status
- No ConfigMap or Secret appears in target namespace

#### Diagnosis
```bash
# Check if outputs exist in state
kubectl logs -n tfout deployment/tfout-controller-manager | grep "outputs found"

# Verify target namespace exists
kubectl get namespace <target-namespace>

# Check RBAC permissions
kubectl auth can-i create configmaps --as=system:serviceaccount:tfout:tfout-controller-manager -n <target-namespace>
```

#### Solutions

**No Outputs in State**
```bash
# Verify Terraform state has outputs
terraform output -json  # From your Terraform directory
```

**RBAC Issues**
```bash
# Check ClusterRole
kubectl get clusterrole tfout-manager-role -o yaml

# Verify RoleBinding
kubectl get clusterrolebinding tfout-manager-rolebinding -o yaml
```

### 4. Operator Pod Not Starting

#### Symptoms
- TFOut pods in `Pending`, `CrashLoopBackOff`, or `Error` state
- No operator logs available

#### Diagnosis
```bash
kubectl get pods -n tfout
kubectl describe pod <pod-name> -n tfout
kubectl logs <pod-name> -n tfout
```

#### Common Causes & Solutions

**Image Pull Issues**
```bash
# Check image pull secrets
kubectl get pods <pod-name> -n tfout -o yaml | grep -A5 imagePullSecrets

# Verify image exists
docker pull ghcr.io/swibrow/tfout:latest
```

**Resource Constraints**
```yaml
# Increase resource limits
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

**Security Context Issues**
```bash
# Check security policies
kubectl get psp,securitycontextconstraints

# Try relaxed security context for debugging
securityContext: {}
podSecurityContext: {}
```

### 5. Slow Sync Performance

#### Symptoms
- Long delays between Terraform changes and Kubernetes updates
- High CPU/memory usage
- Timeout errors

#### Solutions

**Optimize Sync Interval**
```yaml
spec:
  syncInterval: 5m  # Increase from 1m for production
```

**Reduce State File Size**
```bash
# Check state file size
aws s3api head-object --bucket your-bucket --key your-key

# Split large state files or use remote state data sources
```

**Increase Resources**
```yaml
resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 256Mi
```

### 6. Metrics Not Available

#### Symptoms
- Prometheus metrics endpoint returns 404 or connection refused
- No metrics in monitoring dashboards

#### Diagnosis
```bash
# Check metrics endpoint
kubectl port-forward -n tfout deployment/tfout-controller-manager 8080:8080
curl http://localhost:8080/metrics

# Check service
kubectl get service -n tfout
```

#### Solutions

**Metrics Disabled**
```yaml
# Enable metrics in Helm values
metrics:
  enabled: true
  port: 8080

controller:
  metricsBindAddress: ":8080"  # Not "0"
```

**Network Policy Issues**
```bash
# Check network policies
kubectl get networkpolicy -n tfout

# Test connectivity
kubectl run test-pod --image=curlimages/curl --rm -it -- \
  curl http://tfout-metrics.tfout.svc.cluster.local:8080/metrics
```

## Advanced Debugging

### Enable Debug Logging

```yaml
# Helm values for detailed logging
controller:
  development: true
  logLevel: debug

env:
  - name: GOMAXPROCS
    value: "1"
  - name: GOMEMLIMIT
    value: "100Mi"
```

### Debug Pod Access

Create a debug pod to test connectivity:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: debug-pod
  namespace: tfout
spec:
  serviceAccountName: tfout-controller-manager
  containers:
  - name: debug
    image: amazon/aws-cli
    command: ["/bin/sh"]
    args: ["-c", "sleep 3600"]
    env:
    - name: AWS_REGION
      value: us-west-2
```

Then exec into it:
```bash
kubectl exec -it debug-pod -n tfout -- /bin/sh

# Test AWS access
aws sts get-caller-identity
aws s3 ls s3://your-bucket/

# Test network connectivity
curl -v https://s3.amazonaws.com
```

### Operator Profiling

Enable profiling for performance issues:

```yaml
# Add pprof endpoint
controller:
  development: true

env:
  - name: ENABLE_PPROF
    value: "true"
```

Access profiling data:
```bash
kubectl port-forward -n tfout deployment/tfout-controller-manager 6060:6060
go tool pprof http://localhost:6060/debug/pprof/profile
```

## State File Validation

### Verify State Format

Valid Terraform state should have this structure:

```json
{
  "version": 4,
  "terraform_version": "1.0.0",
  "outputs": {
    "example_output": {
      "value": "example_value",
      "type": "string",
      "sensitive": false
    }
  }
}
```

### Common State Issues

**Missing Outputs Section**
```bash
# Check if outputs exist
jq '.outputs // "No outputs found"' terraform.tfstate
```

**Corrupted State**
```bash
# Validate JSON format
jq . terraform.tfstate > /dev/null && echo "Valid JSON" || echo "Invalid JSON"
```

**State Lock Issues**
```bash
# Check for state locks (if using DynamoDB)
aws dynamodb get-item --table-name terraform-locks --key '{"LockID":{"S":"bucket/key"}}'
```

## Resource Management

### Clean Up Orphaned Resources

```bash
# Find ConfigMaps/Secrets managed by TFOut
kubectl get configmaps,secrets -l managed-by=tfout --all-namespaces

# Clean up specific resources
kubectl delete configmap <name> -n <namespace>
kubectl delete secret <name> -n <namespace>
```

### Force Resource Recreation

```bash
# Delete existing resources to force recreation
kubectl delete configmap <configmap-name> -n <namespace>
kubectl delete secret <secret-name> -n <namespace>

# Trigger resync
kubectl annotate terraformoutputs <name> -n <namespace> force-sync="$(date)"
```

## Performance Tuning

### Memory Optimization

```yaml
resources:
  limits:
    memory: 256Mi  # Adjust based on state file size
  requests:
    memory: 128Mi

env:
  - name: GOMEMLIMIT
    value: "200Mi"  # Set Go memory limit
```

### CPU Optimization

```yaml
resources:
  limits:
    cpu: 500m
  requests:
    cpu: 100m

env:
  - name: GOMAXPROCS
    value: "2"  # Limit Go runtime threads
```

### Sync Optimization

```yaml
spec:
  # Adjust based on requirements
  syncInterval: 15m  # Longer intervals for stable environments

# Use multiple smaller TerraformOutputs instead of one large one
```

## Getting Help

### Collect Debug Information

When reporting issues, include:

```bash
# Cluster information
kubectl version
kubectl get nodes

# TFOut deployment info
helm list -n tfout
kubectl get pods -n tfout -o wide

# Resource status
kubectl get terraformoutputs --all-namespaces -o yaml > terraformoutputs.yaml

# Operator logs
kubectl logs -n tfout deployment/tfout-controller-manager --tail=200 > operator.log

# Events
kubectl get events --all-namespaces --sort-by='.lastTimestamp' > events.yaml
```

### Community Resources

- [GitHub Issues](https://github.com/swibrow/tfout/issues)
- [GitHub Discussions](https://github.com/swibrow/tfout/discussions)
- [Documentation](https://swibrow.github.io/tfout/)

### Support Levels

- **Community Support**: GitHub issues and discussions
- **Bug Reports**: Include debug information and reproduction steps
- **Feature Requests**: Describe use case and expected behavior

## Prevention

### Best Practices

1. **Test in Development First**: Always test configuration changes in non-production
2. **Monitor Sync Status**: Set up alerts for failed syncs
3. **Use Appropriate Intervals**: Don't sync too frequently in production
4. **Implement RBAC**: Use least-privilege access
5. **Version Control**: Keep TerraformOutputs resources in Git
6. **Document Dependencies**: Clearly document which applications depend on which outputs

### Health Checks

```yaml
# Add health monitoring
livenessProbe:
  httpGet:
    path: /healthz
    port: 8081
  initialDelaySeconds: 15
  periodSeconds: 20

readinessProbe:
  httpGet:
    path: /readyz
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 10
```