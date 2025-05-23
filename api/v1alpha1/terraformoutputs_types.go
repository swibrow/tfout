package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TerraformOutputsSpec defines the desired state of TerraformOutputs
type TerraformOutputsSpec struct {
	// S3Backend defines the S3 backend configuration
	S3Backend S3BackendSpec `json:"s3Backend"`

	// SyncInterval defines how often to sync outputs (default: 5m)
	// +kubebuilder:default="5m"
	SyncInterval string `json:"syncInterval,omitempty"`

	// Target defines where to store the outputs
	Target TargetSpec `json:"target"`
}

// S3BackendSpec defines S3 backend configuration
type S3BackendSpec struct {
	// Bucket is the S3 bucket name
	Bucket string `json:"bucket"`

	// Key is the path to the terraform state file
	Key string `json:"key"`

	// Region is the AWS region
	Region string `json:"region"`

	// Endpoint is optional S3-compatible endpoint
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
}

// TargetSpec defines where outputs should be stored
type TargetSpec struct {
	// Namespace where ConfigMap/Secret will be created
	// +kubebuilder:default="default"
	Namespace string `json:"namespace,omitempty"`

	// ConfigMapName for non-sensitive outputs
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// SecretName for sensitive outputs
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// SensitiveKeys defines which output keys should go to Secret
	// +optional
	SensitiveKeys []string `json:"sensitiveKeys,omitempty"`
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
//+kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.spec.s3Backend.bucket`
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
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Items             []TerraformOutputs `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TerraformOutputs{}, &TerraformOutputsList{})
}
