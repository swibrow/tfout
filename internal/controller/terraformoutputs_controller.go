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
	"github.com/prometheus/client_golang/prometheus"
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
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	outputsv1alpha1 "github.com/swibrow/tfout/api/v1alpha1"
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

//+kubebuilder:rbac:groups=tfout.wibrow.net,resources=terraformoutputs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=tfout.wibrow.net,resources=terraformoutputs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tfout.wibrow.net,resources=terraformoutputs/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

const (
	// ETagAnnotationPrefix stores the S3 object ETag to detect changes for each backend
	ETagAnnotationPrefix = "terraform-tfout.wibrow.net/s3-etag-"
)

var (
	// Metrics for monitoring the operator performance and behavior
	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "terraform_outputs_reconcile_total",
			Help: "Total number of reconciliations",
		},
		[]string{"namespace", "name", "result"},
	)

	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "terraform_outputs_reconcile_duration_seconds",
			Help:    "Duration of reconciliation operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "name", "result"},
	)

	backendFetchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "terraform_outputs_backend_fetch_total",
			Help: "Total number of backend fetches",
		},
		[]string{"namespace", "name", "backend_type", "backend_index", "result"},
	)

	backendFetchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "terraform_outputs_backend_fetch_duration_seconds",
			Help:    "Duration of backend fetch operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "name", "backend_type", "backend_index"},
	)

	outputsFound = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "terraform_outputs_found_total",
			Help: "Total number of outputs found from all backends",
		},
		[]string{"namespace", "name"},
	)

	sensitiveOutputsFound = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "terraform_outputs_sensitive_total",
			Help: "Total number of sensitive outputs found",
		},
		[]string{"namespace", "name"},
	)

	lastSyncTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "terraform_outputs_last_sync_timestamp",
			Help: "Timestamp of the last successful sync",
		},
		[]string{"namespace", "name"},
	)

	s3RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "terraform_outputs_s3_requests_total",
			Help: "Total number of S3 requests",
		},
		[]string{"namespace", "name", "operation", "result"},
	)

	configMapOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "terraform_outputs_configmap_operations_total",
			Help: "Total number of ConfigMap operations",
		},
		[]string{"namespace", "name", "operation", "result"},
	)

	secretOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "terraform_outputs_secret_operations_total",
			Help: "Total number of Secret operations",
		},
		[]string{"namespace", "name", "operation", "result"},
	)
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		reconcileTotal,
		reconcileDuration,
		backendFetchTotal,
		backendFetchDuration,
		outputsFound,
		sensitiveOutputsFound,
		lastSyncTimestamp,
		s3RequestsTotal,
		configMapOperationsTotal,
		secretOperationsTotal,
	)
}

// Reconcile handles the reconciliation loop
func (r *TerraformOutputsReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)

	// Track reconcile metrics
	labels := prometheus.Labels{
		"namespace": req.Namespace,
		"name":      req.Name,
	}

	// Fetch the TerraformOutputs instance
	var terraformOutputs outputsv1alpha1.TerraformOutputs
	if err := r.Get(ctx, req.NamespacedName, &terraformOutputs); err != nil {
		if errors.IsNotFound(err) {
			logger.Info(
				"TerraformOutputs resource not found. Ignoring since object must be deleted",
			)
			// Record successful reconcile for deleted resource
			labels["result"] = "success"
			reconcileTotal.With(labels).Inc()
			reconcileDuration.With(labels).Observe(time.Since(startTime).Seconds())
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get TerraformOutputs")
		labels["result"] = "error"
		reconcileTotal.With(labels).Inc()
		reconcileDuration.With(labels).Observe(time.Since(startTime).Seconds())
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
				logger.Info(
					"Sync interval not reached, skipping",
					"timeSinceLastSync",
					timeSinceLastSync,
					"syncInterval",
					syncInterval,
				)
				return ctrl.Result{RequeueAfter: syncInterval - timeSinceLastSync}, nil
			}
		}

		// Check if any S3 objects have changed by comparing ETags
		hasChanges, _, err := r.checkBackendChanges(ctx, &terraformOutputs)
		if err != nil {
			logger.Error(err, "Failed to check backend changes")
			// Update status to Failed with retry
			r.updateStatusWithRetry(
				ctx,
				req.NamespacedName,
				func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
					tfOutputs.Status.SyncStatus = "Failed"
					tfOutputs.Status.Message = fmt.Sprintf(
						"Failed to check backend changes: %v",
						err,
					)
				},
			)
			return ctrl.Result{RequeueAfter: syncInterval}, err
		}

		// Skip processing if no ETags have changed
		if !hasChanges {
			logger.Info("No backend changes detected, skipping sync")
			return ctrl.Result{RequeueAfter: syncInterval}, nil
		}

		logger.Info("Backend changes detected, processing updates")
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

	// Fetch outputs from all backends
	outputs, sensitiveFlags, err := r.fetchAllTerraformOutputs(ctx, &terraformOutputs)
	if err != nil {
		logger.Error(err, "Failed to fetch Terraform outputs")
		// Update status to Failed with retry
		r.updateStatusWithRetry(
			ctx,
			req.NamespacedName,
			func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
				tfOutputs.Status.SyncStatus = "Failed"
				tfOutputs.Status.Message = fmt.Sprintf("Failed to fetch outputs: %v", err)
			},
		)
		labels["result"] = "error"
		reconcileTotal.With(labels).Inc()
		reconcileDuration.With(labels).Observe(time.Since(startTime).Seconds())
		return ctrl.Result{RequeueAfter: syncInterval}, err
	}

	// Create/Update ConfigMaps and Secrets
	if err := r.syncKubernetesResources(ctx, &terraformOutputs, outputs, sensitiveFlags); err != nil {
		logger.Error(err, "Failed to sync Kubernetes resources")
		// Update status to Failed with retry
		r.updateStatusWithRetry(
			ctx,
			req.NamespacedName,
			func(tfOutputs *outputsv1alpha1.TerraformOutputs) {
				tfOutputs.Status.SyncStatus = "Failed"
				tfOutputs.Status.Message = fmt.Sprintf("Failed to sync resources: %v", err)
			},
		)
		labels["result"] = "error"
		reconcileTotal.With(labels).Inc()
		reconcileDuration.With(labels).Observe(time.Since(startTime).Seconds())
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

		// Update ETag annotations only if this wasn't a force sync
		if !shouldForceSync {
			// Get current ETags again (might have changed during processing)
			if _, currentETags, err := r.checkBackendChanges(ctx, tfOutputs); err == nil {
				if tfOutputs.Annotations == nil {
					tfOutputs.Annotations = make(map[string]string)
				}
				r.updateETagAnnotations(tfOutputs, currentETags)
			}
		}
	}); err != nil {
		logger.Error(err, "Failed to update status and annotations")
		return ctrl.Result{}, err
	}

	// Record success metrics
	labels["result"] = "success"
	reconcileTotal.With(labels).Inc()
	reconcileDuration.With(labels).Observe(time.Since(startTime).Seconds())

	// Update output count metrics
	outputsFound.With(prometheus.Labels{"namespace": req.Namespace, "name": req.Name}).
		Set(float64(len(outputs)))

	sensitiveCount := 0
	for _, isSensitive := range sensitiveFlags {
		if isSensitive {
			sensitiveCount++
		}
	}
	sensitiveOutputsFound.With(prometheus.Labels{"namespace": req.Namespace, "name": req.Name}).
		Set(float64(sensitiveCount))

	// Update last sync timestamp
	lastSyncTimestamp.With(prometheus.Labels{"namespace": req.Namespace, "name": req.Name}).
		SetToCurrentTime()

	if shouldForceSync {
		logger.Info("Successfully recreated missing resources", "outputs", len(outputs))
	} else {
		logger.Info("Successfully reconciled TerraformOutputs", "outputs", len(outputs))
	}

	return ctrl.Result{RequeueAfter: syncInterval}, nil
}

// shouldForceSyncDueToMissingResources checks if ConfigMap or Secret are missing and need recreation
func (r *TerraformOutputsReconciler) shouldForceSyncDueToMissingResources(
	ctx context.Context,
	tfOutputs *outputsv1alpha1.TerraformOutputs,
) bool {
	logger := log.FromContext(ctx)

	// Check if ConfigMap should exist but is missing
	if tfOutputs.Spec.Target.ConfigMapName != "" {
		configMap := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      tfOutputs.Spec.Target.ConfigMapName,
			Namespace: tfOutputs.Spec.Target.Namespace,
		}, configMap)

		if errors.IsNotFound(err) {
			logger.Info(
				"ConfigMap missing, triggering force sync",
				"configmap",
				tfOutputs.Spec.Target.ConfigMapName,
			)
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
			logger.Info(
				"Secret missing, triggering force sync",
				"secret",
				tfOutputs.Spec.Target.SecretName,
			)
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
func (r *TerraformOutputsReconciler) hasOwnerReference(
	ownerRefs []metav1.OwnerReference,
	tfOutputs *outputsv1alpha1.TerraformOutputs,
) bool {
	for _, ref := range ownerRefs {
		if ref.Kind == "TerraformOutputs" &&
			ref.APIVersion == "tfout.wibrow.net/v1alpha1" &&
			ref.Name == tfOutputs.Name &&
			ref.UID == tfOutputs.UID {
			return true
		}
	}
	return false
}

// checkBackendChanges checks if any backend has changed by comparing ETags
func (r *TerraformOutputsReconciler) checkBackendChanges(
	ctx context.Context,
	tfOutputs *outputsv1alpha1.TerraformOutputs,
) (bool, map[int]string, error) {
	if len(tfOutputs.Spec.Backends) == 0 {
		return false, nil, fmt.Errorf("no backends configured")
	}

	currentETags := make(map[int]string)
	hasChanges := false

	for i, backend := range tfOutputs.Spec.Backends {
		backendType := backend.GetBackendType()
		if backendType != "s3" {
			return false, nil, fmt.Errorf("unsupported backend type: %s", backendType)
		}

		etag, err := r.getS3ObjectETag(ctx, *backend.S3, tfOutputs.Namespace, tfOutputs.Name)
		if err != nil {
			return false, nil, fmt.Errorf("failed to get ETag for backend %d: %w", i, err)
		}

		currentETags[i] = etag

		// Compare with stored ETag
		storedETag := ""
		if tfOutputs.Annotations != nil {
			storedETag = tfOutputs.Annotations[fmt.Sprintf("%s%d", ETagAnnotationPrefix, i)]
		}

		if storedETag == "" || storedETag != etag {
			hasChanges = true
		}
	}

	return hasChanges, currentETags, nil
}

// getS3ObjectETag gets the ETag of an S3 object without downloading it
func (r *TerraformOutputsReconciler) getS3ObjectETag(
	ctx context.Context,
	s3Spec outputsv1alpha1.S3Spec,
	namespace, name string,
) (string, error) {
	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3Spec.Region))
	if err != nil {
		return "", fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	var s3Client *s3.Client
	if s3Spec.Endpoint != "" {
		s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s3Spec.Endpoint)
			o.UsePathStyle = true // Often needed for S3-compatible services
		})
	} else {
		s3Client = s3.NewFromConfig(cfg)
	}

	// Use HeadObject to get metadata without downloading the file
	s3Labels := prometheus.Labels{
		"namespace": namespace,
		"name":      name,
		"operation": "HeadObject",
	}

	result, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s3Spec.Bucket),
		Key:    aws.String(s3Spec.Key),
	})
	if err != nil {
		s3Labels["result"] = "error"
		s3RequestsTotal.With(s3Labels).Inc()
		return "", fmt.Errorf("failed to get S3 object metadata: %w", err)
	}

	s3Labels["result"] = "success"
	s3RequestsTotal.With(s3Labels).Inc()

	// Extract ETag and remove quotes if present
	etag := aws.ToString(result.ETag)
	etag = strings.Trim(etag, "\"")

	return etag, nil
}

// updateETagAnnotations updates the ETag annotations for all backends
func (r *TerraformOutputsReconciler) updateETagAnnotations(
	tfOutputs *outputsv1alpha1.TerraformOutputs,
	etags map[int]string,
) {
	for i, etag := range etags {
		tfOutputs.Annotations[fmt.Sprintf("%s%d", ETagAnnotationPrefix, i)] = etag
	}
}

// updateResourceWithRetry updates both spec/status and annotations with retry logic
func (r *TerraformOutputsReconciler) updateResourceWithRetry(
	ctx context.Context,
	namespacedName types.NamespacedName,
	updateFunc func(*outputsv1alpha1.TerraformOutputs),
) error {
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

func (r *TerraformOutputsReconciler) updateStatusWithRetry(
	ctx context.Context,
	namespacedName types.NamespacedName,
	updateFunc func(*outputsv1alpha1.TerraformOutputs),
) error {
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

// fetchAllTerraformOutputs fetches outputs from all backends and merges them
func (r *TerraformOutputsReconciler) fetchAllTerraformOutputs(
	ctx context.Context,
	tfOutputs *outputsv1alpha1.TerraformOutputs,
) (map[string]interface{}, map[string]bool, error) {
	logger := log.FromContext(ctx)

	if len(tfOutputs.Spec.Backends) == 0 {
		return nil, nil, fmt.Errorf("no backends configured")
	}

	// Merged outputs from all backends
	mergedOutputs := make(map[string]interface{})
	mergedSensitiveFlags := make(map[string]bool)

	for i, backend := range tfOutputs.Spec.Backends {
		backendType := backend.GetBackendType()
		if backendType != "s3" {
			return nil, nil, fmt.Errorf(
				"unsupported backend type: %s for backend %d",
				backendType,
				i,
			)
		}

		logger.Info(
			"Processing backend",
			"index",
			i,
			"bucket",
			backend.S3.Bucket,
			"key",
			backend.S3.Key,
		)

		// Track backend fetch metrics
		backendStartTime := time.Now()
		backendLabels := prometheus.Labels{
			"namespace":     tfOutputs.Namespace,
			"name":          tfOutputs.Name,
			"backend_type":  backendType,
			"backend_index": fmt.Sprintf("%d", i),
		}

		outputs, sensitiveFlags, err := r.fetchTerraformOutputsFromS3(
			ctx,
			*backend.S3,
			i,
			tfOutputs.Namespace,
			tfOutputs.Name,
		)

		if err != nil {
			backendLabels["result"] = "error"
			backendFetchTotal.With(backendLabels).Inc()
			return nil, nil, fmt.Errorf("failed to fetch outputs from backend %d: %w", i, err)
		}

		backendLabels["result"] = "success"
		backendFetchTotal.With(backendLabels).Inc()

		// Remove result label for duration metric
		delete(backendLabels, "result")
		backendFetchDuration.With(backendLabels).Observe(time.Since(backendStartTime).Seconds())

		// Merge outputs, checking for conflicts
		for key, value := range outputs {
			if existingValue, exists := mergedOutputs[key]; exists {
				logger.Info(
					"Output key conflict detected, using latest value",
					"key",
					key,
					"backend",
					i,
				)
				// Log the conflict but use the latest value (last backend wins)
				_ = existingValue
			}
			mergedOutputs[key] = value
			mergedSensitiveFlags[key] = sensitiveFlags[key]
		}

		logger.Info("Successfully processed backend", "index", i, "outputs", len(outputs))
	}

	logger.Info(
		"Successfully fetched and merged Terraform outputs from all backends",
		"totalOutputs",
		len(mergedOutputs),
		"backends",
		len(tfOutputs.Spec.Backends),
	)
	return mergedOutputs, mergedSensitiveFlags, nil
}

// fetchTerraformOutputsFromS3 fetches outputs from a single S3 backend
func (r *TerraformOutputsReconciler) fetchTerraformOutputsFromS3(
	ctx context.Context,
	s3Spec outputsv1alpha1.S3Spec,
	backendIndex int,
	namespace, name string,
) (map[string]interface{}, map[string]bool, error) {
	logger := log.FromContext(ctx)

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3Spec.Region))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	var s3Client *s3.Client
	if s3Spec.Endpoint != "" {
		s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s3Spec.Endpoint)
			o.UsePathStyle = true // Often needed for S3-compatible services
		})
	} else {
		s3Client = s3.NewFromConfig(cfg)
	}

	// Download state file
	logger.Info(
		"Downloading Terraform state",
		"backend",
		backendIndex,
		"bucket",
		s3Spec.Bucket,
		"key",
		s3Spec.Key,
	)

	s3Labels := prometheus.Labels{
		"namespace": namespace,
		"name":      name,
		"operation": "GetObject",
	}

	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s3Spec.Bucket),
		Key:    aws.String(s3Spec.Key),
	})
	if err != nil {
		s3Labels["result"] = "error"
		s3RequestsTotal.With(s3Labels).Inc()
		return nil, nil, fmt.Errorf("failed to download state file: %w", err)
	}
	defer result.Body.Close()

	s3Labels["result"] = "success"
	s3RequestsTotal.With(s3Labels).Inc()

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

	return outputs, sensitiveFlags, nil
}

// syncKubernetesResources creates/updates ConfigMaps and Secrets based on sensitivity flags
func (r *TerraformOutputsReconciler) syncKubernetesResources(
	ctx context.Context,
	tfOutputs *outputsv1alpha1.TerraformOutputs,
	outputs map[string]interface{},
	sensitiveFlags map[string]bool,
) error {
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

	logger.Info(
		"Categorized outputs",
		"sensitive",
		sensitiveCount,
		"non-sensitive",
		len(configData),
	)

	// Create/Update ConfigMap if needed and has non-sensitive data
	if tfOutputs.Spec.Target.ConfigMapName != "" && len(configData) > 0 {
		if err := r.syncConfigMap(ctx, tfOutputs, configData); err != nil {
			return fmt.Errorf("failed to sync ConfigMap: %w", err)
		}
		logger.Info(
			"ConfigMap synced",
			"name",
			tfOutputs.Spec.Target.ConfigMapName,
			"keys",
			len(configData),
		)
	}

	// Create/Update Secret if needed and has sensitive data
	if tfOutputs.Spec.Target.SecretName != "" && len(secretData) > 0 {
		if err := r.syncSecret(ctx, tfOutputs, secretData); err != nil {
			return fmt.Errorf("failed to sync Secret: %w", err)
		}
		logger.Info(
			"Secret synced",
			"name",
			tfOutputs.Spec.Target.SecretName,
			"keys",
			len(secretData),
		)
	}

	// If ConfigMap is specified but no non-sensitive data exists, create empty ConfigMap
	if tfOutputs.Spec.Target.ConfigMapName != "" && len(configData) == 0 {
		if err := r.syncConfigMap(ctx, tfOutputs, configData); err != nil {
			return fmt.Errorf("failed to sync empty ConfigMap: %w", err)
		}
		logger.Info(
			"Empty ConfigMap synced (no non-sensitive outputs)",
			"name",
			tfOutputs.Spec.Target.ConfigMapName,
		)
	}

	// If Secret is specified but no sensitive data exists, create empty Secret
	if tfOutputs.Spec.Target.SecretName != "" && len(secretData) == 0 {
		if err := r.syncSecret(ctx, tfOutputs, secretData); err != nil {
			return fmt.Errorf("failed to sync empty Secret: %w", err)
		}
		logger.Info(
			"Empty Secret synced (no sensitive outputs)",
			"name",
			tfOutputs.Spec.Target.SecretName,
		)
	}

	return nil
}

// syncConfigMap creates or updates a ConfigMap
func (r *TerraformOutputsReconciler) syncConfigMap(
	ctx context.Context,
	tfOutputs *outputsv1alpha1.TerraformOutputs,
	data map[string]string,
) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tfOutputs.Spec.Target.ConfigMapName,
			Namespace: tfOutputs.Spec.Target.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tfout",
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

	configMapLabels := prometheus.Labels{
		"namespace": tfOutputs.Namespace,
		"name":      tfOutputs.Name,
	}

	if errors.IsNotFound(err) {
		configMapLabels["operation"] = "create"
		err := r.Create(ctx, configMap)
		if err != nil {
			configMapLabels["result"] = "error"
		} else {
			configMapLabels["result"] = "success"
		}
		configMapOperationsTotal.With(configMapLabels).Inc()
		return err
	} else if err != nil {
		return err
	}

	// Update existing ConfigMap
	existingConfigMap.Data = data
	existingConfigMap.Labels = configMap.Labels
	configMapLabels["operation"] = "update"
	err = r.Update(ctx, existingConfigMap)
	if err != nil {
		configMapLabels["result"] = "error"
	} else {
		configMapLabels["result"] = "success"
	}
	configMapOperationsTotal.With(configMapLabels).Inc()
	return err
}

// syncSecret creates or updates a Secret
func (r *TerraformOutputsReconciler) syncSecret(
	ctx context.Context,
	tfOutputs *outputsv1alpha1.TerraformOutputs,
	data map[string][]byte,
) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tfOutputs.Spec.Target.SecretName,
			Namespace: tfOutputs.Spec.Target.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tfout",
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

	secretLabels := prometheus.Labels{
		"namespace": tfOutputs.Namespace,
		"name":      tfOutputs.Name,
	}

	if errors.IsNotFound(err) {
		secretLabels["operation"] = "create"
		err := r.Create(ctx, secret)
		if err != nil {
			secretLabels["result"] = "error"
		} else {
			secretLabels["result"] = "success"
		}
		secretOperationsTotal.With(secretLabels).Inc()
		return err
	} else if err != nil {
		return err
	}

	// Update existing Secret
	existingSecret.Data = data
	existingSecret.Labels = secret.Labels
	secretLabels["operation"] = "update"
	err = r.Update(ctx, existingSecret)
	if err != nil {
		secretLabels["result"] = "error"
	} else {
		secretLabels["result"] = "success"
	}
	secretOperationsTotal.With(secretLabels).Inc()
	return err
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
