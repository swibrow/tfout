package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// Check if we need to sync based on last sync time
	if terraformOutputs.Status.LastSyncTime != nil {
		timeSinceLastSync := time.Since(terraformOutputs.Status.LastSyncTime.Time)
		if timeSinceLastSync < syncInterval {
			logger.Info("Sync interval not reached, skipping", "timeSinceLastSync", timeSinceLastSync, "syncInterval", syncInterval)
			return ctrl.Result{RequeueAfter: syncInterval - timeSinceLastSync}, nil
		}
	}

	// Update status to InProgress
	terraformOutputs.Status.SyncStatus = "InProgress"
	terraformOutputs.Status.Message = "Fetching Terraform outputs"
	if err := r.Status().Update(ctx, &terraformOutputs); err != nil {
		logger.Error(err, "Failed to update status to InProgress")
		return ctrl.Result{}, err
	}

	// Fetch outputs from S3
	outputs, err := r.fetchTerraformOutputs(ctx, &terraformOutputs)
	if err != nil {
		logger.Error(err, "Failed to fetch Terraform outputs")
		terraformOutputs.Status.SyncStatus = "Failed"
		terraformOutputs.Status.Message = fmt.Sprintf("Failed to fetch outputs: %v", err)
		r.Status().Update(ctx, &terraformOutputs)
		return ctrl.Result{RequeueAfter: syncInterval}, err
	}

	// Create/Update ConfigMaps and Secrets
	if err := r.syncKubernetesResources(ctx, &terraformOutputs, outputs); err != nil {
		logger.Error(err, "Failed to sync Kubernetes resources")
		terraformOutputs.Status.SyncStatus = "Failed"
		terraformOutputs.Status.Message = fmt.Sprintf("Failed to sync resources: %v", err)
		r.Status().Update(ctx, &terraformOutputs)
		return ctrl.Result{RequeueAfter: syncInterval}, err
	}

	// Update status to Success
	now := metav1.Now()
	terraformOutputs.Status.LastSyncTime = &now
	terraformOutputs.Status.SyncStatus = "Success"
	terraformOutputs.Status.OutputCount = len(outputs)
	terraformOutputs.Status.Message = fmt.Sprintf("Successfully synced %d outputs", len(outputs))

	if err := r.Status().Update(ctx, &terraformOutputs); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled TerraformOutputs", "outputs", len(outputs))
	return ctrl.Result{RequeueAfter: syncInterval}, nil
}

// fetchTerraformOutputs fetches outputs from S3 Terraform state using AWS SDK v2
func (r *TerraformOutputsReconciler) fetchTerraformOutputs(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs) (map[string]interface{}, error) {
	logger := log.FromContext(ctx)

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(tfOutputs.Spec.S3Backend.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
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
		return nil, fmt.Errorf("failed to download state file: %w", err)
	}
	defer result.Body.Close()

	// Read the entire body
	body, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file body: %w", err)
	}

	// Parse Terraform state
	var tfState TerraformState
	if err := json.Unmarshal(body, &tfState); err != nil {
		return nil, fmt.Errorf("failed to parse Terraform state: %w", err)
	}

	// Extract output values
	outputs := make(map[string]interface{})
	for key, output := range tfState.Outputs {
		outputs[key] = output.Value
	}

	logger.Info("Successfully fetched Terraform outputs", "count", len(outputs))
	return outputs, nil
}

// syncKubernetesResources creates/updates ConfigMaps and Secrets
func (r *TerraformOutputsReconciler) syncKubernetesResources(ctx context.Context, tfOutputs *outputsv1alpha1.TerraformOutputs, outputs map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Separate sensitive and non-sensitive outputs
	sensitiveKeys := make(map[string]bool)
	for _, key := range tfOutputs.Spec.Target.SensitiveKeys {
		sensitiveKeys[key] = true
	}

	configData := make(map[string]string)
	secretData := make(map[string][]byte)

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

		if sensitiveKeys[key] {
			secretData[key] = []byte(valueStr)
		} else {
			configData[key] = valueStr
		}
	}

	// Create/Update ConfigMap if needed
	if tfOutputs.Spec.Target.ConfigMapName != "" && len(configData) > 0 {
		if err := r.syncConfigMap(ctx, tfOutputs, configData); err != nil {
			return fmt.Errorf("failed to sync ConfigMap: %w", err)
		}
		logger.Info("ConfigMap synced", "name", tfOutputs.Spec.Target.ConfigMapName, "keys", len(configData))
	}

	// Create/Update Secret if needed
	if tfOutputs.Spec.Target.SecretName != "" && len(secretData) > 0 {
		if err := r.syncSecret(ctx, tfOutputs, secretData); err != nil {
			return fmt.Errorf("failed to sync Secret: %w", err)
		}
		logger.Info("Secret synced", "name", tfOutputs.Spec.Target.SecretName, "keys", len(secretData))
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
		Complete(r)
}
