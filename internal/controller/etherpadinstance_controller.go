package controller

import (
	"context"
	etherpadv1alpha1 "etherpadinstance/api/v1alpha1"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const etherpadinstanceFinalizer = "etherpad.etherpadinstance.io/finalizer"

// Definitions to manage status conditions
const (
	typeAvailableEtherpadInstance = "Available"
	typeDegradedEtherpadInstance  = "Degraded"
)

// EtherpadInstanceReconciler reconciles a EtherpadInstance object
type EtherpadInstanceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=etherpad.etherpadinstance.io,resources=etherpadinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=etherpad.etherpadinstance.io,resources=etherpadinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=etherpad.etherpadinstance.io,resources=etherpadinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *EtherpadInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	etherpadinstance := &etherpadv1alpha1.EtherpadInstance{}
	err := r.Get(ctx, req.NamespacedName, etherpadinstance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("EtherpadInstance resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get EtherpadInstance")
		return ctrl.Result{}, err
	}

	// Set the status as Unknown when no status is available
	if etherpadinstance.Status.Conditions == nil || len(etherpadinstance.Status.Conditions) == 0 {
		meta.SetStatusCondition(&etherpadinstance.Status.Conditions, metav1.Condition{
			Type:    typeAvailableEtherpadInstance,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Starting reconciliation",
		})
		if err = r.Status().Update(ctx, etherpadinstance); err != nil {
			log.Error(err, "Failed to update EtherpadInstance status")
			return ctrl.Result{}, err
		}

		if err := r.Get(ctx, req.NamespacedName, etherpadinstance); err != nil {
			log.Error(err, "Failed to re-fetch EtherpadInstance")
			return ctrl.Result{}, err
		}
	}

	if !controllerutil.ContainsFinalizer(etherpadinstance, etherpadinstanceFinalizer) {
		log.Info("Adding Finalizer for EtherpadInstance")
		if ok := controllerutil.AddFinalizer(etherpadinstance, etherpadinstanceFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, etherpadinstance); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	isEtherpadInstanceMarkedToBeDeleted := etherpadinstance.GetDeletionTimestamp() != nil
	if isEtherpadInstanceMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(etherpadinstance, etherpadinstanceFinalizer) {
			log.Info("Performing Finalizer Operations for EtherpadInstance before delete CR")

			meta.SetStatusCondition(&etherpadinstance.Status.Conditions, metav1.Condition{
				Type:    typeDegradedEtherpadInstance,
				Status:  metav1.ConditionUnknown,
				Reason:  "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s", etherpadinstance.Name),
			})

			if err := r.Status().Update(ctx, etherpadinstance); err != nil {
				log.Error(err, "Failed to update EtherpadInstance status")
				return ctrl.Result{}, err
			}

			r.doFinalizerOperationsForEtherpadInstance(etherpadinstance)

			if err := r.Get(ctx, req.NamespacedName, etherpadinstance); err != nil {
				log.Error(err, "Failed to re-fetch EtherpadInstance")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&etherpadinstance.Status.Conditions, metav1.Condition{
				Type:    typeDegradedEtherpadInstance,
				Status:  metav1.ConditionTrue,
				Reason:  "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s were successfully accomplished", etherpadinstance.Name),
			})

			if err := r.Status().Update(ctx, etherpadinstance); err != nil {
				log.Error(err, "Failed to update EtherpadInstance status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for EtherpadInstance after successfully performing the operations")
			if ok := controllerutil.RemoveFinalizer(etherpadinstance, etherpadinstanceFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for EtherpadInstance")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, etherpadinstance); err != nil {
				log.Error(err, "Failed to remove finalizer for EtherpadInstance")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Create ConfigMap if specified
	if err := r.createConfigMap(ctx, etherpadinstance); err != nil {
		log.Error(err, "Failed to create ConfigMap")
		return ctrl.Result{}, err
	}

	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(etherpadinstance.Namespace),
		client.MatchingLabels(labelsForEtherpadInstance(etherpadinstance.Name)),
	}
	if err = r.List(ctx, podList, listOpts...); err != nil {
		log.Error(err, "Failed to list pods", "EtherpadInstance.Namespace", etherpadinstance.Namespace, "EtherpadInstance.Name", etherpadinstance.Name)
		return ctrl.Result{}, err
	}
	podNames := getPodNames(podList.Items)

	if !equalSlices(podNames, etherpadinstance.Status.Nodes) {
		etherpadinstance.Status.Nodes = podNames
		if err := r.Status().Update(ctx, etherpadinstance); err != nil {
			log.Error(err, "Failed to update EtherpadInstance status")
			return ctrl.Result{}, err
		}
	}

	for _, podSpec := range etherpadinstance.Spec.Pods {
		foundPod := &corev1.Pod{}
		err = r.Get(ctx, types.NamespacedName{Name: podSpec.Name, Namespace: etherpadinstance.Namespace}, foundPod)
		if err != nil && apierrors.IsNotFound(err) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podSpec.Name,
					Namespace: etherpadinstance.Namespace,
					Labels:    labelsForEtherpadInstance(etherpadinstance.Name),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:         podSpec.Name,
						Image:        podSpec.Image,
						Command:      podSpec.Command,
						Args:         podSpec.Args,
						Ports:        podSpec.ContainerPorts,
						Env:          podSpec.Env,
						VolumeMounts: podSpec.VolumeMounts,
					}},
					Volumes: podSpec.Volumes,
				},
			}

			// Add ConfigMap volume if specified
			if etherpadinstance.Spec.ConfigMap != nil {
				volumeName := "config-volume"
				pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
					Name: volumeName,
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: etherpadinstance.Spec.ConfigMap.Name,
							},
							Items: func() []corev1.KeyToPath {
								var items []corev1.KeyToPath
								for _, item := range etherpadinstance.Spec.ConfigMap.Items {
									items = append(items, corev1.KeyToPath{
										Key:  item.Key,
										Path: item.Path,
									})
								}
								return items
							}(),
						},
					},
				})

				pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      volumeName,
					MountPath: "/etc/config",
				})
			}

			if err := ctrl.SetControllerReference(etherpadinstance, pod, r.Scheme); err != nil {
				log.Error(err, "Failed to set controller reference", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
				return ctrl.Result{}, err
			}

			log.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
			if err = r.Create(ctx, pod); err != nil {
				log.Error(err, "Failed to create new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
				return ctrl.Result{}, err
			}

			if len(podSpec.ContainerPorts) > 0 {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podSpec.Name + "-service",
						Namespace: etherpadinstance.Namespace,
						Labels:    labelsForEtherpadInstance(etherpadinstance.Name),
					},
					Spec: corev1.ServiceSpec{
						Selector: labelsForEtherpadInstance(etherpadinstance.Name),
						Ports:    []corev1.ServicePort{},
					},
				}

				for _, port := range podSpec.ContainerPorts {
					service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
						Port:       port.ContainerPort,
						TargetPort: intstr.FromInt(int(port.ContainerPort)),
						Protocol:   port.Protocol,
					})
				}

				if err := ctrl.SetControllerReference(etherpadinstance, service, r.Scheme); err != nil {
					log.Error(err, "Failed to set controller reference", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
					return ctrl.Result{}, err
				}

				log.Info("Creating a new Service for Pod", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
				if err = r.Create(ctx, service); err != nil && !apierrors.IsAlreadyExists(err) {
					log.Error(err, "Failed to create new Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
					return ctrl.Result{}, err
				}
			}

			meta.SetStatusCondition(&etherpadinstance.Status.Conditions, metav1.Condition{
				Type:    typeAvailableEtherpadInstance,
				Status:  metav1.ConditionTrue,
				Reason:  "Reconciling",
				Message: fmt.Sprintf("Pod %s for custom resource %s created successfully", pod.Name, etherpadinstance.Name),
			})
			if err := r.Status().Update(ctx, etherpadinstance); err != nil {
				log.Error(err, "Failed to update EtherpadInstance status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get Pod")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *EtherpadInstanceReconciler) createConfigMap(ctx context.Context, etherpadinstance *etherpadv1alpha1.EtherpadInstance) error {
	log := log.FromContext(ctx)

	if etherpadinstance.Spec.ConfigMap == nil {
		return nil
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      etherpadinstance.Spec.ConfigMap.Name,
			Namespace: etherpadinstance.Namespace,
			Labels:    labelsForEtherpadInstance(etherpadinstance.Name),
		},
		Data: make(map[string]string),
	}

	if err := ctrl.SetControllerReference(etherpadinstance, configMap, r.Scheme); err != nil {
		return err
	}

	err := r.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, &corev1.ConfigMap{})
	if err != nil && apierrors.IsNotFound(err) {
		log.Info("Creating ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
		if err = r.Create(ctx, configMap); err != nil {
			return err
		}
	}
	return nil
}

func labelsForEtherpadInstance(name string) map[string]string {
	return map[string]string{"app": "etherpad", "instance": name}
}

func (r *EtherpadInstanceReconciler) doFinalizerOperationsForEtherpadInstance(cr *etherpadv1alpha1.EtherpadInstance) {
	log := log.Log.WithName("doFinalizerOperationsForEtherpadInstance")

	// Delete ConfigMap if it exists
	if cr.Spec.ConfigMap != nil {
		configMap := &corev1.ConfigMap{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Name:      cr.Spec.ConfigMap.Name,
			Namespace: cr.Namespace,
		}, configMap)
		if err == nil {
			log.Info("Deleting ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
			if err := r.Delete(context.TODO(), configMap); err != nil {
				log.Error(err, "Failed to delete ConfigMap")
			}
		}
	}

	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(cr.Namespace),
		client.MatchingLabels(labelsForEtherpadInstance(cr.Name)),
	}
	if err := r.List(context.TODO(), podList, listOpts...); err != nil {
		log.Error(err, "Failed to list pods for deletion")
		return
	}

	for _, pod := range podList.Items {
		log.Info("Deleting Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		if err := r.Delete(context.TODO(), &pod); err != nil {
			log.Error(err, "Failed to delete Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		}

		serviceName := pod.Name + "-service"
		service := &corev1.Service{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: pod.Namespace}, service)
		if err == nil {
			log.Info("Deleting Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
			if err := r.Delete(context.TODO(), service); err != nil {
				log.Error(err, "Failed to delete Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
			}
		} else if !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get Service for deletion", "Service.Namespace", pod.Namespace, "Service.Name", serviceName)
		}
	}
	fmt.Printf("Finalizer operations for %s\n", cr.Name)
}

func (r *EtherpadInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etherpadv1alpha1.EtherpadInstance{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func getPodNames(pods []corev1.Pod) []string {
	var names []string
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	return names
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
