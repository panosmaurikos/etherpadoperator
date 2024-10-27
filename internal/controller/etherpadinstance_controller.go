package controller

import (
	"context"
	etherpadv1alpha1 "etherpadinstance/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
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

// EtherpadInstanceReconciler reconciles an EtherpadInstance object
type EtherpadInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=etherpad.etherpadinstance.io,resources=etherpadinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=etherpad.etherpadinstance.io,resources=etherpadinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=etherpad.etherpadinstance.io,resources=etherpadinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
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

	// Add Finalizer for this CR
	if !controllerutil.ContainsFinalizer(etherpadinstance, etherpadinstanceFinalizer) {
		log.Info("Adding Finalizer for EtherpadInstance")
		controllerutil.AddFinalizer(etherpadinstance, etherpadinstanceFinalizer)
		if err = r.Update(ctx, etherpadinstance); err != nil {
			log.Error(err, "Failed to update EtherpadInstance with finalizer")
			return ctrl.Result{}, err
		}
	}

	// Handle CR deletion
	isMarkedToBeDeleted := etherpadinstance.GetDeletionTimestamp() != nil
	if isMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(etherpadinstance, etherpadinstanceFinalizer) {
			log.Info("Performing Finalizer Operations for EtherpadInstance")
			controllerutil.RemoveFinalizer(etherpadinstance, etherpadinstanceFinalizer)
			if err := r.Update(ctx, etherpadinstance); err != nil {
				log.Error(err, "Failed to remove finalizer for EtherpadInstance")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Create or update the ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etherpad-config",
			Namespace: etherpadinstance.Namespace,
		},
		Data: etherpadinstance.Spec.ConfigMapData,
	}
	if err := r.createOrUpdateConfigMap(ctx, configMap, etherpadinstance); err != nil {
		log.Error(err, "Failed to create or update ConfigMap")
		return ctrl.Result{}, err
	}

	// Create or update the Deployment
	for _, deploymentSpec := range etherpadinstance.Spec.Deployments {
		if err := r.createOrUpdateDeployment(ctx, deploymentSpec, etherpadinstance); err != nil {
			log.Error(err, "Failed to create or update Deployment", "Deployment.Name", deploymentSpec.Name)
			return ctrl.Result{}, err
		}
	}

	// Create or update the Service
	for _, deploymentSpec := range etherpadinstance.Spec.Deployments {
		if err := r.createOrUpdateService(ctx, deploymentSpec, etherpadinstance); err != nil {
			log.Error(err, "Failed to create or update Service", "Service.Name", deploymentSpec.Name)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *EtherpadInstanceReconciler) createOrUpdateConfigMap(ctx context.Context, configMap *corev1.ConfigMap, etherpadinstance *etherpadv1alpha1.EtherpadInstance) error {
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Data = etherpadinstance.Spec.ConfigMapData
		return controllerutil.SetControllerReference(etherpadinstance, configMap, r.Scheme)
	})
	return err
}

func (r *EtherpadInstanceReconciler) createOrUpdateDeployment(ctx context.Context, deploymentSpec etherpadv1alpha1.DeploymentSpec, etherpadinstance *etherpadv1alpha1.EtherpadInstance) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentSpec.Name,
			Namespace: etherpadinstance.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: deploymentSpec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labelsForEtherpadInstance(etherpadinstance.Name),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelsForEtherpadInstance(etherpadinstance.Name),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:         deploymentSpec.Name,
						Image:        deploymentSpec.Image,
						Command:      deploymentSpec.Command,
						Args:         deploymentSpec.Args,
						Ports:        deploymentSpec.ContainerPorts,
						Env:          deploymentSpec.Env,
						VolumeMounts: deploymentSpec.VolumeMounts,
					}},
					Volumes: deploymentSpec.Volumes,
				},
			},
		}
		return controllerutil.SetControllerReference(etherpadinstance, deployment, r.Scheme)
	})
	return err
}

func (r *EtherpadInstanceReconciler) createOrUpdateService(ctx context.Context, deploymentSpec etherpadv1alpha1.DeploymentSpec, etherpadinstance *etherpadv1alpha1.EtherpadInstance) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentSpec.Name + "-svc",
			Namespace: etherpadinstance.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Spec = corev1.ServiceSpec{
			Selector: labelsForEtherpadInstance(etherpadinstance.Name),
			Ports: []corev1.ServicePort{
				{
					Port:       deploymentSpec.ServicePort,
					TargetPort: intstr.FromInt(int(deploymentSpec.ContainerPorts[0].ContainerPort)),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		}
		return controllerutil.SetControllerReference(etherpadinstance, service, r.Scheme)
	})
	return err
}

// labelsForEtherpadInstance returns the labels for selecting the resources
func labelsForEtherpadInstance(name string) map[string]string {
	return map[string]string{"app": "etherpad", "etherpadinstance": name}
}

func (r *EtherpadInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&etherpadv1alpha1.EtherpadInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
