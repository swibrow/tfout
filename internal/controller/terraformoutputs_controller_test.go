/*
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
*/

package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	outputsv1alpha1 "github.com/swibrow/tfout/api/v1alpha1"
)

var _ = Describe("TerraformOutputs Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()
		var mockS3Server *httptest.Server

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create a mock S3 server that returns a sample Terraform state
			sampleTerraformState := map[string]interface{}{
				"outputs": map[string]interface{}{
					"vpc_id": map[string]interface{}{
						"value":     "vpc-12345",
						"sensitive": false,
					},
					"database_password": map[string]interface{}{
						"value":     "super-secret-password",
						"sensitive": true,
					},
					"region": map[string]interface{}{
						"value":     "us-east-1",
						"sensitive": false,
					},
				},
			}

			stateBytes, _ := json.Marshal(sampleTerraformState)

			mockS3Server = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case "HEAD":
						// Mock HeadObject response for ETag checking
						w.Header().Set("ETag", "\"sample-etag-123\"")
						w.WriteHeader(http.StatusOK)
					case "GET":
						// Mock GetObject response with Terraform state
						w.Header().Set("Content-Type", "application/json")
						w.Header().Set("ETag", "\"sample-etag-123\"")
						w.WriteHeader(http.StatusOK)
						w.Write(stateBytes)
					default:
						w.WriteHeader(http.StatusMethodNotAllowed)
					}
				}),
			)

			// Set environment variables to use mock server
			os.Setenv("AWS_ACCESS_KEY_ID", "test")
			os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			os.Setenv("AWS_SESSION_TOKEN", "test")

			By("creating the custom resource for the Kind TerraformOutputs")
			resource := &outputsv1alpha1.TerraformOutputs{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: outputsv1alpha1.TerraformOutputsSpec{
					SyncInterval: "5m",
					Backends: []outputsv1alpha1.BackendSpec{
						{
							S3: &outputsv1alpha1.S3Spec{
								Bucket:   "test-bucket",
								Key:      "test.tfstate",
								Region:   "us-east-1",
								Endpoint: mockS3Server.URL,
							},
						},
					},
					Target: outputsv1alpha1.TargetSpec{
						Namespace:     "default",
						ConfigMapName: "test-configmap",
						SecretName:    "test-secret",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			// Clean up the mock server
			if mockS3Server != nil {
				mockS3Server.Close()
			}

			// Clean up environment variables
			os.Unsetenv("AWS_ACCESS_KEY_ID")
			os.Unsetenv("AWS_SECRET_ACCESS_KEY")
			os.Unsetenv("AWS_SESSION_TOKEN")

			// Clean up the resource
			resource := &outputsv1alpha1.TerraformOutputs{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance TerraformOutputs")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Clean up created ConfigMap and Secret
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(
				ctx,
				types.NamespacedName{Name: "test-configmap", Namespace: "default"},
				configMap,
			)
			if err == nil {
				k8sClient.Delete(ctx, configMap)
			}

			secret := &corev1.Secret{}
			err = k8sClient.Get(
				ctx,
				types.NamespacedName{Name: "test-secret", Namespace: "default"},
				secret,
			)
			if err == nil {
				k8sClient.Delete(ctx, secret)
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TerraformOutputsReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Verify ConfigMap was created with non-sensitive outputs
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-configmap",
					Namespace: "default",
				}, configMap)
			}, "10s", "500ms").Should(Succeed())

			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-configmap",
				Namespace: "default",
			}, configMap)).To(Succeed())
			Expect(configMap.Data).To(HaveKey("vpc_id"))
			Expect(configMap.Data["vpc_id"]).To(Equal("vpc-12345"))
			Expect(configMap.Data).To(HaveKey("region"))
			Expect(configMap.Data["region"]).To(Equal("us-east-1"))
			Expect(
				configMap.Data,
			).NotTo(HaveKey("database_password"))
			// Should be in Secret, not ConfigMap

			// Verify Secret was created with sensitive outputs
			Eventually(func() error {
				secret := &corev1.Secret{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-secret",
					Namespace: "default",
				}, secret)
			}, "10s", "500ms").Should(Succeed())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-secret",
				Namespace: "default",
			}, secret)).To(Succeed())
			Expect(secret.Data).To(HaveKey("database_password"))
			Expect(string(secret.Data["database_password"])).To(Equal("super-secret-password"))
			Expect(secret.Data).NotTo(HaveKey("vpc_id")) // Should be in ConfigMap, not Secret
			Expect(secret.Data).NotTo(HaveKey("region")) // Should be in ConfigMap, not Secret
		})

		It("should handle missing ConfigMap by triggering force sync", func() {
			By("First reconciling to create resources")
			controllerReconciler := &TerraformOutputsReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Deleting the ConfigMap to simulate missing resource")
			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-configmap",
				Namespace: "default",
			}, configMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())

			By("Reconciling again should recreate the ConfigMap")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap was recreated
			newConfigMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-configmap",
				Namespace: "default",
			}, newConfigMap)).To(Succeed())
			Expect(newConfigMap.Data).To(HaveKey("vpc_id"))
		})
	})
})
