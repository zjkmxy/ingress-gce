package vm

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	ingctx "k8s.io/ingress-gce/pkg/context"
	migconfigclient "k8s.io/ingress-gce/pkg/experimental/migconfig/client/clientset/versioned"
	informermigconfig "k8s.io/ingress-gce/pkg/experimental/migconfig/client/informers/externalversions/migconfig/v1alpha1"
	"k8s.io/legacy-cloud-providers/gce"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	migconfigv1a1 "k8s.io/ingress-gce/pkg/apis/migconfig/v1alpha1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// ExControllerContext holds the state needed for the execution of the controller.
type ExControllerContext struct {
	MigConfigInformer cache.SharedIndexInformer
}

// NewControllerContext returns a set of informers
func NewControllerContext(
	kubeClient kubernetes.Interface,
	migConfigClient migconfigclient.Interface,
	config ingctx.ControllerContextConfig) *ExControllerContext {
	context := &ExControllerContext{
		MigConfigInformer: informermigconfig.NewMigConfigInformer(migConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
	}
	return context
}

// Start all of the informers.
func (vmctx *ExControllerContext) Start(stopCh chan struct{}) {
	go vmctx.MigConfigInformer.Run(stopCh)
}

// Controller is VM controller
type Controller struct {
	hasSynced func() bool

	// migConfigQueue takes MigConfig key as work item. MigConfig key with format "namespace/name".
	migConfigQueue workqueue.RateLimitingInterface

	migConfigLister cache.Indexer
	serviceLister   cache.Indexer
	kubeClient      kubernetes.Interface
	cloud           *gce.Cloud
}

// NewController returns a VM controller.
func NewController(
	ctx *ingctx.ControllerContext,
	vmctx *ExControllerContext,
) *Controller {
	vmController := &Controller{
		hasSynced: func() bool {
			return ctx.HasSynced() && vmctx.MigConfigInformer.HasSynced()
		},
		migConfigQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		migConfigLister: vmctx.MigConfigInformer.GetIndexer(),
		serviceLister:   ctx.ServiceInformer.GetIndexer(),
		kubeClient:      ctx.KubeClient,
		cloud:           ctx.Cloud,
	}

	vmctx.MigConfigInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: vmController.enqueueMigConfig,
		UpdateFunc: func(old, cur interface{}) {
			vmController.enqueueMigConfig(cur)
		},
	})

	return vmController
}

// Run executes VM controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		klog.V(2).Infof("Waiting for initial sync")
		return c.hasSynced(), nil
	}, stopCh)

	klog.V(2).Infof("Starting VM controller")
	defer func() {
		klog.V(2).Infof("Shutting down network endpoint group controller")
		c.stop()
	}()

	go wait.Until(c.migConfigWorker, time.Second, stopCh)
	<-stopCh
}

func (c *Controller) stop() {
	klog.V(2).Infof("Shutting down network endpoint group controller")
	c.migConfigQueue.ShutDown()
}

func (c *Controller) enqueueMigConfig(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.migConfigQueue.Add(key)
}

func (c *Controller) migConfigWorker() {
	for {
		func() {
			key, quit := c.migConfigQueue.Get()
			if quit {
				return
			}
			defer c.migConfigQueue.Done(key)
			err := c.processMigConfig(key.(string))
			c.handleErr(err, key)
		}()
	}
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.migConfigQueue.Forget(key)
		return
	}

	msg := fmt.Sprintf("error processing MigConfig %q: %v", key, err)
	klog.Errorf(msg)
	if _, exists, err := c.migConfigLister.GetByKey(key.(string)); err != nil {
		klog.Warningf("Failed to retrieve MigConfig %q from store: %v", key.(string), err)
	} else if exists {
		klog.Warningf("Process MigConfig %q failed: %v", key.(string), err)
	}
	c.migConfigQueue.AddRateLimited(key)
}

func (c *Controller) processMigConfig(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	obj, exists, err := c.migConfigLister.GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	migConfig := obj.(*migconfigv1a1.MigConfig)

	migName := migConfig.Spec.MigName
	if migName == "" {
		utilruntime.HandleError(fmt.Errorf("%s: MIG name must be specified", key))
		return nil
	}

	zone := migConfig.Spec.Zone
	if zone == "" {
		utilruntime.HandleError(fmt.Errorf("%s: Zone must be specified", key))
		return nil
	}

	// Create or update Service
	_, exists, err = c.serviceLister.GetByKey(namespace + "/" + name + "-" + migName)
	if !exists {
		_, err = c.kubeClient.CoreV1().Services(namespace).Create(context.TODO(), newService(migConfig), metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}

	// List current VMs in the MIG
	_, err = c.cloud.GetInstanceGroup(migName, zone)
	if err != nil {
		klog.Errorf("Cannot get instance group for %s: %v", migName, err)
		return err
	}

	vms, err := c.cloud.ListInstancesInInstanceGroup(migName, zone, "RUNNING")
	if err != nil {
		klog.Errorf("Cannot get instances in %s: %v", migName, err)
		return err
	}
	for _, vm := range vms {
		klog.V(0).Info("listed instance: ", vm.Instance)
	}
	klog.V(0).Info("Successfully queried mig", migName)
	return nil

	// Create or update Endpoints/EngpointSlices
}

func newService(migConfig *migconfigv1a1.MigConfig) *corev1.Service {
	labels := map[string]string{
		"migName":    migConfig.Spec.MigName,
		"controller": migConfig.Name,
	}
	var port int32 = 80
	if migConfig.Spec.Port != nil {
		port = *migConfig.Spec.Port
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      migConfig.Name + "-" + migConfig.Spec.MigName,
			Namespace: migConfig.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(migConfig, migconfigv1a1.SchemeGroupVersion.WithKind("MigConfig")),
			},
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Protocol:   "TCP",
					Port:       port,
					TargetPort: intstr.IntOrString{IntVal: port},
				},
			},
			Type: "ClusterIP",
		},
	}
}
