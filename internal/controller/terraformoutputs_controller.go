package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	outputsv1alpha1 "tfoutputs.io/terraformoutputs/api/v1alpha1"
)

// TerraformOutputsReconciler reconciles a TerraformOutputs object
type TerraformOutputsReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// TerraformState represents the structure of a Terraform state file
type TerraformState struct {
	Outputs map[string]TerraformOutput `json:"outputs"`
}

// TerraformOutput represents a single output in the state
type TerraformOutput struct {
	Value     interface{} `json:"value"`
	Type      interface{} `json:"type"`
	Sensitive bool        `json:"sensitive"`
}

//+kubebuilder:rbac:groups=outputs.tfoutputs.io,resources=terraformoutputs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=outputs.tfoutputs.io,resources=terraformoutputs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=outputs.tfoutputs.io,resources=terraformoutputs/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

const (
	// ETagAnnotation stores the S3 object ETag to detect changes
	ETagAnnotation = "terraform-outputs.tfoutputs.io/s3-etag"
)

// Reconcile handles the reconciliation loop
func (r *TerraformOutputsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the TerraformOutputs instance
	var terraformOutputs outputsv1alpha1.TerraformOutputs
	if err := r.Get(ctx, req.NamespacedName, &terraformOutputs); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TerraformOutputs resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get TerraformOutputs")
		return ctrl.Result{}, err
	}

	// Parse sync interval
	syncInterval, err := time.ParseDuration(terraformOutputs.Spec.SyncInterval)
	if err != nil {
		syncInterval = 5 * time.Minute // default
	}

	// Check if this reconcile was triggered by ConfigMap/Secret deletion
	// If so, we need to recreate them regardless of sync interval or ETag
	shouldForceSync := r.shouldForceSyncDueToMissingResources(ctx, &terraformOutputs)

	if !shouldForceSync {
		// Check if we need to sync based on last sync time
		if terraformOutputs.Status.LastSyncTime != nil {
			timeSinceLastSync := time.Since(terraformOutputs.Status.LastSyncTime.Time)
			if timeSinceLastSync < syncInterval {
				logger.Info("Sync interval not reached, skipping", "timeSinceLastSync", timeSinceLastSync, "syncInterval", syncInterval)
				return ctrl.Result{RequeueAfter: syncInterval - timeSinceLastSync}, nil
			}
		}

		// Check if S3 object has changed by comparing ETags
		currentETag, err := r.getS3ObjectETag(ctx, &terraformOutputs)
		if err != nil {
			logger.Error(err, "Failed to get S3 object ETag")
			// Update status to Failed with retry
			r.updateStatusWithRetry(ctx, req.NamespacedName, func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
				tfOutputs.Status.SyncStatus = "Failed"
				tfOutputs.Status.Message = fmt.Sprintf("Failed to get S3 object ETag: %v", err)
			})
			return ctrl.Result{RequeueAfter: syncInterval}, err
		}

		// Get stored ETag from annotation
		var storedETag string
		if terraformOutputs.Annotations != nil {
			storedETag = terraformOutputs.Annotations[ETagAnnotation]
		}

		// Skip processing if ETag hasn't changed
		if storedETag != "" && storedETag == currentETag {
			logger.Info("S3 object ETag unchanged, skipping sync", "etag", currentETag)
			return ctrl.Result{RequeueAfter: syncInterval}, nil
		}

		logger.Info("S3 object changed, processing updates", "oldETag", storedETag, "newETag", currentETag)
	} else {
		logger.Info("Force sync triggered due to missing ConfigMap/Secret resources")
	}

	// Update status to InProgress with retry
	if err := r.updateStatusWithRetry(ctx, req.NamespacedName, func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
		tfOutputs.Status.SyncStatus = "InProgress"
		if shouldForceSync {
			tfOutputs.Status.Message = "Recreating missing resources"
		} else {
			tfOutputs.Status.Message = "Fetching Terraform outputs"
		}
	}); err != nil {
		logger.Error(err, "Failed to update status to InProgress")
		return ctrl.Result{}, err
	}

	// Fetch outputs from S3
	outputs, sensitiveFlags, err := r.fetchTerraformOutputs(ctx, &terraformOutputs)
	if err != nil {
		logger.Error(err, "Failed to fetch Terraform outputs")
		// Update status to Failed with retry
		r.updateStatusWithRetry(ctx, req.NamespacedName, func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
			tfOutputs.Status.SyncStatus = "Failed"
			tfOutputs.Status.Message = fmt.Sprintf("Failed to fetch outputs: %v", err)
		})
		return ctrl.Result{RequeueAfter: syncInterval}, err
	}

	// Create/Update ConfigMaps and Secrets
	if err := r.syncKubernetesResources(ctx, &terraformOutputs, outputs, sensitiveFlags); err != nil {
		logger.Error(err, "Failed to sync Kubernetes resources")
		// Update status to Failed with retry
		r.updateStatusWithRetry(ctx, req.NamespacedName, func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
			tfOutputs.Status.SyncStatus = "Failed"
			tfOutputs.Status.Message = fmt.Sprintf("Failed to sync resources: %v", err)
		})
		return ctrl.Result{RequeueAfter: syncInterval}, err
	}

	// Update both status and ETag annotation with retry (only update ETag if not force sync)
	if err := r.updateResourceWithRetry(ctx, req.NamespacedName, func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
		// Update status
		now := metav1.Now()
		tfOutputs.Status.LastSyncTime = &now
		tfOutputs.Status.SyncStatus = "Success"
		tfOutputs.Status.OutputCount = len(outputs)
		if shouldForceSync {
			tfOutputs.Status.Message = fmt.Sprintf("Successfully recreated missing resources with %d outputs", len(outputs))
		} else {
			tfOutputs.Status.Message = fmt.Sprintf("Successfully synced %d outputs", len(outputs))
		}

		// Update ETag annotation only if this wasn't a force sync
		if !shouldForceSync {
			// Get current ETag again (might have changed during processing)
			if currentETag, err := r.getS3ObjectETag(ctx, tfOutputs); err == nil {
				if tfOutputs.Annotations == nil {
					tfOutputs.Annotations = make(map[string]string)
				}
				tfOutputs.Annotations[ETagAnnotation] = currentETag
			}
		}
	}); err != nil {
		logger.Error(err, "Failed to update status and annotations")
		return ctrl.Result{}, err
	}

	if shouldForceSync {
		logger.Info("Successfully recreated missing resources", "outputs", len(outputs))
	} else {
		logger.Info("Successfully reconciled TerraformOutputs", "outputs", len(outputs))
	}

	return ctrl.Result{RequeueAfter: syncInterval}, nil
}

// shouldForceSyncDueToMissingResources checks if ConfigMap or Secret are missing and need recreation
func (r *TerraformOutputsReconciler) shouldForceSyncDueToMissingResources(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs) bool {
	logger := log.FromContext(ctx)

	// Check if ConfigMap should exist but is missing
	if tfOutputs.Spec.Target.ConfigMapName != "" {
		configMap := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      tfOutputs.Spec.Target.ConfigMapName,
			Namespace: tfOutputs.Spec.Target.Namespace,
		}, configMap)

		if errors.IsNotFound(err) {
			logger.Info("ConfigMap missing, triggering force sync", "configmap", tfOutputs.Spec.Target.ConfigMapName)
			return true
		} else if err != nil {
			logger.Error(err, "Failed to check ConfigMap existence")
		} else {
			// Check if ConfigMap has proper owner reference
			if !r.hasOwnerReference(configMap.GetOwnerReferences(), tfOutputs) {
				logger.Info("ConfigMap exists but lacks proper owner reference, triggering force sync", "configmap", tfOutputs.Spec.Target.ConfigMapName)
				return true
			}
		}
	}

	// Check if Secret should exist but is missing
	if tfOutputs.Spec.Target.SecretName != "" {
		secret := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      tfOutputs.Spec.Target.SecretName,
			Namespace: tfOutputs.Spec.Target.Namespace,
		}, secret)

		if errors.IsNotFound(err) {
			logger.Info("Secret missing, triggering force sync", "secret", tfOutputs.Spec.Target.SecretName)
			return true
		} else if err != nil {
			logger.Error(err, "Failed to check Secret existence")
		} else {
			// Check if Secret has proper owner reference
			if !r.hasOwnerReference(secret.GetOwnerReferences(), tfOutputs) {
				logger.Info("Secret exists but lacks proper owner reference, triggering force sync", "secret", tfOutputs.Spec.Target.SecretName)
				return true
			}
		}
	}

	return false
}

// hasOwnerReference checks if the given owner references include our TerraformOutputs resource
func (r *TerraformOutputsReconciler) hasOwnerReference(ownerRefs []metav1.OwnerReference, tfOutputs *outputsv1alpha1.TerraformOutputs) bool {
	for _, ref := range ownerRefs {
		if ref.Kind == "TerraformOutputs" &&
			ref.APIVersion == "outputs.tfoutputs.io/v1alpha1" &&
			ref.Name == tfOutputs.Name &&
			ref.UID == tfOutputs.UID {
			return true
		}
	}
	return false
}

// getS3ObjectETag gets the ETag of the S3 object without downloading it
func (r *TerraformOutputsReconciler) getS3ObjectETag(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs) (string, error) {
	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(tfOutputs.Spec.S3Backend.Region))
	if err != nil {
		return "", fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	var s3Client *s3.Client
	if tfOutputs.Spec.S3Backend.Endpoint != "" {
		s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(tfOutputs.Spec.S3Backend.Endpoint)
			o.UsePathStyle = true // Often needed for S3-compatible services
		})
	} else {
		s3Client = s3.NewFromConfig(cfg)
	}

	// Use HeadObject to get metadata without downloading the file
	result, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(tfOutputs.Spec.S3Backend.Bucket),
		Key:    aws.String(tfOutputs.Spec.S3Backend.Key),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get S3 object metadata: %w", err)
	}

	// Extract ETag and remove quotes if present
	etag := aws.ToString(result.ETag)
	etag = strings.Trim(etag, "\"")

	return etag, nil
}

// updateResourceWithRetry updates both spec/status and annotations with retry logic
func (r *TerraformOutputsReconciler) updateResourceWithRetry(ctx context.Context, namespacedName types.NamespacedName, updateFunc func(*outputsv1alpha1.TerraformOutputs)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch the latest version of the resource
		var terraformOutputs outputsv1alpha1.TerraformOutputs
		if err := r.Get(ctx, namespacedName, &terraformOutputs); err != nil {
			return err
		}

		// Apply the update function
		updateFunc(&terraformOutputs)

		// Update the resource (this will update both annotations and status)
		if err := r.Update(ctx, &terraformOutputs); err != nil {
			return err
		}

		// Also update the status subresource
		return r.Status().Update(ctx, &terraformOutputs)
	})
}
func (r *TerraformOutputsReconciler) updateStatusWithRetry(ctx context.Context, namespacedName types.NamespacedName, updateFunc func(*outputsv1alpha1.TerraformOutputs)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch the latest version of the resource
		var terraformOutputs outputsv1alpha1.TerraformOutputs
		if err := r.Get(ctx, namespacedName, &terraformOutputs); err != nil {
			return err
		}

		// Apply the update function
		updateFunc(&terraformOutputs)

		// Try to update the status
		return r.Status().Update(ctx, &terraformOutputs)
	})
}

// fetchTerraformOutputs fetches outputs from S3 Terraform state using AWS SDK v2
func (r *TerraformOutputsReconciler) fetchTerraformOutputs(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs) (map[string]interface{}, map[string]bool, error) {
	logger := log.FromContext(ctx)

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(tfOutputs.Spec.S3Backend.Region))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	var s3Client *s3.Client
	if tfOutputs.Spec.S3Backend.Endpoint != "" {
		s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(tfOutputs.Spec.S3Backend.Endpoint)
			o.UsePathStyle = true // Often needed for S3-compatible services
		})
	} else {
		s3Client = s3.NewFromConfig(cfg)
	}

	// Download state file
	logger.Info("Downloading Terraform state", "bucket", tfOutputs.Spec.S3Backend.Bucket, "key", tfOutputs.Spec.S3Backend.Key)

	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(tfOutputs.Spec.S3Backend.Bucket),
		Key:    aws.String(tfOutputs.Spec.S3Backend.Key),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download state file: %w", err)
	}
	defer result.Body.Close()

	// Read the entire body
	body, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read state file body: %w", err)
	}

	// Parse Terraform state
	var tfState TerraformState
	if err := json.Unmarshal(body, &tfState); err != nil {
		return nil, nil, fmt.Errorf("failed to parse Terraform state: %w", err)
	}

	// Extract output values and sensitivity flags
	outputs := make(map[string]interface{})
	sensitiveFlags := make(map[string]bool)

	for key, output := range tfState.Outputs {
		outputs[key] = output.Value
		sensitiveFlags[key] = output.Sensitive
	}

	logger.Info("Successfully fetched Terraform outputs", "count", len(outputs))
	return outputs, sensitiveFlags, nil
}

// syncKubernetesResources creates/updates ConfigMaps and Secrets based on sensitivity flags
func (r *TerraformOutputsReconciler) syncKubernetesResources(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs, outputs map[string]interface{}, sensitiveFlags map[string]bool) error {
	logger := log.FromContext(ctx)

	configData := make(map[string]string)
	secretData := make(map[string][]byte)
	sensitiveCount := 0

	for key, value := range outputs {
		// Convert value to string - handle complex types
		var valueStr string
		switch v := value.(type) {
		case string:
			valueStr = v
		case []interface{}, map[string]interface{}:
			// For complex types, marshal to JSON
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to marshal complex output %s: %w", key, err)
			}
			valueStr = string(jsonBytes)
		default:
			valueStr = fmt.Sprintf("%v", v)
		}

		// Check if this output is marked as sensitive in Terraform state
		if sensitiveFlags[key] {
			secretData[key] = []byte(valueStr)
			sensitiveCount++
			logger.V(1).Info("Output marked as sensitive", "key", key)
		} else {
			configData[key] = valueStr
			logger.V(1).Info("Output marked as non-sensitive", "key", key)
		}
	}

	logger.Info("Categorized outputs", "sensitive", sensitiveCount, "non-sensitive", len(configData))

	// Create/Update ConfigMap if needed and has non-sensitive data
	if tfOutputs.Spec.Target.ConfigMapName != "" && len(configData) > 0 {
		if err := r.syncConfigMap(ctx, tfOutputs, configData); err != nil {
			return fmt.Errorf("failed to sync ConfigMap: %w", err)
		}
		logger.Info("ConfigMap synced", "name", tfOutputs.Spec.Target.ConfigMapName, "keys", len(configData))
	}

	// Create/Update Secret if needed and has sensitive data
	if tfOutputs.Spec.Target.SecretName != "" && len(secretData) > 0 {
		if err := r.syncSecret(ctx, tfOutputs, secretData); err != nil {
			return fmt.Errorf("failed to sync Secret: %w", err)
		}
		logger.Info("Secret synced", "name", tfOutputs.Spec.Target.SecretName, "keys", len(secretData))
	}

	// If ConfigMap is specified but no non-sensitive data exists, create empty ConfigMap
	if tfOutputs.Spec.Target.ConfigMapName != "" && len(configData) == 0 {
		if err := r.syncConfigMap(ctx, tfOutputs, configData); err != nil {
			return fmt.Errorf("failed to sync empty ConfigMap: %w", err)
		}
		logger.Info("Empty ConfigMap synced (no non-sensitive outputs)", "name", tfOutputs.Spec.Target.ConfigMapName)
	}

	// If Secret is specified but no sensitive data exists, create empty Secret
	if tfOutputs.Spec.Target.SecretName != "" && len(secretData) == 0 {
		if err := r.syncSecret(ctx, tfOutputs, secretData); err != nil {
			return fmt.Errorf("failed to sync empty Secret: %w", err)
		}
		logger.Info("Empty Secret synced (no sensitive outputs)", "name", tfOutputs.Spec.Target.SecretName)
	}

	return nil
}

// syncConfigMap creates or updates a ConfigMap
func (r *TerraformOutputsReconciler) syncConfigMap(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs, data map[string]string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tfOutputs.Spec.Target.ConfigMapName,
			Namespace: tfOutputs.Spec.Target.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tf-outputs-operator",
				"terraform-outputs/source":     tfOutputs.Name,
			},
		},
		Data: data,
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(tfOutputs, configMap, r.Scheme); err != nil {
		return err
	}

	// Create or update
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      configMap.Name,
		Namespace: configMap.Namespace,
	}, existingConfigMap)

	if errors.IsNotFound(err) {
		return r.Create(ctx, configMap)
	} else if err != nil {
		return err
	}

	// Update existing ConfigMap
	existingConfigMap.Data = data
	existingConfigMap.Labels = configMap.Labels
	return r.Update(ctx, existingConfigMap)
}

// syncSecret creates or updates a Secret
func (r *TerraformOutputsReconciler) syncSecret(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs, data map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tfOutputs.Spec.Target.SecretName,
			Namespace: tfOutputs.Spec.Target.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tf-outputs-operator",
				"terraform-outputs/source":     tfOutputs.Name,
			},
		},
		Data: data,
		Type: corev1.SecretTypeOpaque,
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(tfOutputs, secret, r.Scheme); err != nil {
		return err
	}

	// Create or update
	existingSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}, existingSecret)

	if errors.IsNotFound(err) {
		return r.Create(ctx, secret)
	} else if err != nil {
		return err
	}

	// Update existing Secret
	existingSecret.Data = data
	existingSecret.Labels = secret.Labels
	return r.Update(ctx, existingSecret)
}

// SetupWithManager sets up the controller with the Manager
func (r *TerraformOutputsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&outputsv1alpha1.TerraformOutputs{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1, // Ensure serial processing to avoid conflicts
		}).
		Complete(r)
}
