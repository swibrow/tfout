package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TerraformOutputsSpec defines the desired state of TerraformOutputs
type TerraformOutputsSpec struct {
	// Backends defines the list of backend configurations
	// +kubebuilder:validation:MinItems=1
	Backends []BackendSpec `json:"backends"`

	// SyncInterval defines how often to sync outputs (default: 5m)
	// +kubebuilder:default="5m"
	SyncInterval string `json:"syncInterval,omitempty"`

	// Target defines where to store the outputs
	Target TargetSpec `json:"target"`
}

// BackendSpec defines a backend configuration
// Exactly one backend configuration must be specified.
type BackendSpec struct {
	// S3 defines the S3 backend configuration
	// +optional
	S3 *S3Spec `json:"s3,omitempty"`
}

// S3Spec defines S3 backend configuration
type S3Spec struct {
	// Bucket is the S3 bucket name
	Bucket string `json:"bucket"`

	// Key is the path to the terraform state file
	Key string `json:"key"`

	// Region is the AWS region
	Region string `json:"region"`

	// Endpoint is optional S3-compatible endpoint
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Role is the IAM role to assume for accessing the S3 bucket
	// +optional
	Role string `json:"role,omitempty"`
}

// TargetSpec defines where outputs should be stored
type TargetSpec struct {
	// Namespace where ConfigMap/Secret will be created
	// +kubebuilder:default="default"
	Namespace string `json:"namespace,omitempty"`

	// ConfigMapName for non-sensitive outputs
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// SecretName for sensitive outputs (automatically determined from Terraform state)
	// +optional
	SecretName string `json:"secretName,omitempty"`
}

// TerraformOutputsStatus defines the observed state of TerraformOutputs
type TerraformOutputsStatus struct {
	// LastSyncTime is when outputs were last synced
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// SyncStatus represents the current sync status
	// +kubebuilder:validation:Enum=Success;Failed;InProgress
	SyncStatus string `json:"syncStatus,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`

	// OutputCount is the number of outputs found
	// +optional
	OutputCount int `json:"outputCount,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.spec.backends[0].source.bucket`
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.syncStatus`
//+kubebuilder:printcolumn:name="Outputs",type=integer,JSONPath=`.status.outputCount`
//+kubebuilder:printcolumn:name="Last Sync",type=date,JSONPath=`.status.lastSyncTime`

// TerraformOutputs is the Schema for the terraformoutputs API
type TerraformOutputs struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TerraformOutputsSpec   `json:"spec,omitempty"`
	Status TerraformOutputsStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TerraformOutputsList contains a list of TerraformOutputs
type TerraformOutputsList struct {
	metav1.TypeMeta `                   json:",inline"`
	metav1.ListMeta `                   json:"metadata,omitempty"`
	Items           []TerraformOutputs `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TerraformOutputs{}, &TerraformOutputsList{})
}

// ValidateBackend validates that exactly one backend configuration is specified
func (bs *BackendSpec) ValidateBackend() error {
	configCount := 0

	if bs.S3 != nil {
		configCount++
	}

	if configCount == 0 {
		return fmt.Errorf(
			"exactly one backend configuration must be specified (s3)",
		)
	}

	return nil
}

// GetBackendType returns the type of backend configured
func (bs *BackendSpec) GetBackendType() string {
	if bs.S3 != nil {
		return "s3"
	}
	return ""
}
