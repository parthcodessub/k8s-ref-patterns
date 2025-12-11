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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	webappv1 "mydomain.com/appservice/api/v1"
)

// AppServiceReconciler reconciles a AppService object
type AppServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=webapp.mydomain.com,resources=appservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=webapp.mydomain.com,resources=appservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=webapp.mydomain.com,resources=appservices/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *AppServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// 1. Fetch the AppService instance (The "Instruction")
	var appService webappv1.AppService
	if err := r.Get(ctx, req.NamespacedName, &appService); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Define the Desired Deployment (The "Goal")
	// We want a Deployment with the same name as the AppService
	desiredDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appService.Name,
			Namespace: appService.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &appService.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": appService.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appService.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "main",
						Image: appService.Spec.Image,
					}},
				},
			},
		},
	}
	// Set OwnerReference (Garbage Collection glue)
	if err := ctrl.SetControllerReference(&appService, desiredDep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// 3. Check if Deployment exists
	foundDep := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: appService.Name, Namespace: appService.Namespace}, foundDep)

	if err != nil && errors.IsNotFound(err) {
		// CASE A: Deployment does not exist -> CREATE IT
		l.Info("Creating a new Deployment", "Replicas", appService.Spec.Replicas)
		err = r.Create(ctx, desiredDep)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err == nil {
		// CASE B: Deployment exists -> CHECK FOR DRIFT (Update)

		shouldUpdate := false

		// Check 1: Are replicas correct?
		if *foundDep.Spec.Replicas != *desiredDep.Spec.Replicas {
			foundDep.Spec.Replicas = desiredDep.Spec.Replicas
			shouldUpdate = true
		}

		// Check 2: Is image correct?
		currentImage := foundDep.Spec.Template.Spec.Containers[0].Image
		desiredImage := desiredDep.Spec.Template.Spec.Containers[0].Image
		if currentImage != desiredImage {
			foundDep.Spec.Template.Spec.Containers[0].Image = desiredImage
			shouldUpdate = true
		}

		if shouldUpdate {
			l.Info("Drift detected. Updating Deployment.")
			err = r.Update(ctx, foundDep)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.AppService{}).
		Named("appservice").
		Complete(r)
}
