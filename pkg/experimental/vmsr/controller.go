package vmsr

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	vminstv1a1 "k8s.io/ingress-gce/pkg/apis/vminstance/v1alpha1"
	ingctx "k8s.io/ingress-gce/pkg/context"
	exctx "k8s.io/ingress-gce/pkg/experimental/context"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/api/v1/endpoints"
)

type Controller struct {
	hasSynced func() bool

	queue workqueue.RateLimitingInterface

	vmInstLister    cache.Indexer
	serviceLister   cache.Indexer
	endpointsLister cache.Indexer
	kubeClient      kubernetes.Interface
}

// NewController returns a VM controller.
func NewController(
	ctx *ingctx.ControllerContext,
	vmctx *exctx.ExControllerContext,
) *Controller {
	c := &Controller{
		hasSynced: func() bool {
			return ctx.HasSynced() && vmctx.HasSynced()
		},
		queue:           workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		vmInstLister:    vmctx.VMInstInformer.GetIndexer(),
		serviceLister:   ctx.ServiceInformer.GetIndexer(),
		endpointsLister: ctx.EndpointInformer.GetIndexer(),
		kubeClient:      ctx.KubeClient,
	}

	vmctx.VMInstInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addVM,
		UpdateFunc: c.updateVM,
		DeleteFunc: c.deleteVM,
	})

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.onServiceUpdate,
		UpdateFunc: func(old, cur interface{}) {
			c.onServiceUpdate(cur)
		},
		// EndpointsController handles DeleteFunc and deletes corresponding Endpoints object
		// But EndpointSliceController only releases resources used by tracker
		// It seems that the deletion is automatically done
	})

	return c
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		klog.V(2).Infof("Waiting for initial sync")
		return c.hasSynced(), nil
	}, stopCh)

	klog.V(2).Infof("Starting VM (self-registry) controller")
	defer func() {
		klog.V(2).Infof("Shutting down VM (self-registry) controller")
		c.stop()
	}()

	go wait.Until(c.serviceWorker, time.Second, stopCh)
	<-stopCh
}

func (c *Controller) stop() {
	klog.V(2).Infof("Shutting down VM (self-registry) controller")
	c.queue.ShutDown()
}

func (c *Controller) serviceWorker() {
	for {
		func() {
			key, quit := c.queue.Get()
			if quit {
				return
			}
			defer c.queue.Done(key)
			err := c.processService(key.(string))
			c.handleErr(err, key)
		}()
	}
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	msg := fmt.Sprintf("error processing Service %q: %v", key, err)
	klog.Errorf(msg)
	if _, exists, err := c.vmInstLister.GetByKey(key.(string)); err != nil {
		klog.Warningf("Failed to retrieve Service %q from store: %v", key.(string), err)
	} else if exists {
		klog.Warningf("Process Service %q failed: %v", key.(string), err)
	}
	c.queue.AddRateLimited(key)
}

func (c *Controller) onServiceUpdate(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %+v: %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func getVMFromDeleteAction(obj interface{}) *vminstv1a1.VMInstance {
	if vminst, ok := obj.(*vminstv1a1.VMInstance); ok {
		// Enqueue all the services that the VM used to be a member of.
		// This is the same thing we do when we add a VM.
		return vminst
	}
	// If we reached here it means the VM was deleted but its final state is unrecorded.
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
		return nil
	}
	vminst, ok := tombstone.Obj.(*vminstv1a1.VMInstance)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a Pod: %#v", obj))
		return nil
	}
	return vminst
}

func getVMServiceMemberships(serviceLister cache.Indexer, vm *vminstv1a1.VMInstance) []string {
	// Add a cache if necessary?
	ret := make([]string, 0)
	for _, obj := range serviceLister.List() {
		service := obj.(*corev1.Service)
		if service.Namespace != vm.Namespace {
			continue
		}

		selectorStr, exists := service.Annotations["vm-selector"]
		if !exists {
			continue
		}
		selector, err := labels.Parse(selectorStr)
		if err != nil {
			klog.Warningf("Parse vm-selector of Service %q failed: %v", service.GetName(), err)
			continue
		}
		if !selector.Matches(labels.Set(vm.Labels)) {
			continue
		}

		key, err := cache.MetaNamespaceKeyFunc(service)
		if err != nil {
			klog.Errorf("Get the key of Service %q failed: %v", service.GetName(), err)
			continue
		}

		ret = append(ret, key)
	}
	return ret
}

func (c *Controller) addVM(obj interface{}) {
	vm := obj.(*vminstv1a1.VMInstance)
	services := getVMServiceMemberships(c.serviceLister, vm)
	for _, key := range services {
		c.queue.Add(key)
	}
}

func (c *Controller) updateVM(old, cur interface{}) {
	oldVM := old.(*vminstv1a1.VMInstance)
	newVM := cur.(*vminstv1a1.VMInstance)
	if newVM.ResourceVersion == oldVM.ResourceVersion {
		return
	}

	// To provide a quick and dirty solution, here I redo all services.
	// A better way: github.com/kubernetes/kubernetes@master:/pkg/controller/util/endpoint/controller_utils.go:
	//               func GetServicesToUpdateOnPodChange
	c.addVM(cur)
}

func (c *Controller) deleteVM(obj interface{}) {
	vminst := getVMFromDeleteAction(obj)
	if vminst != nil {
		c.addVM(vminst)
	}
}

func getEndpointPortsFromServicePorts(svcPorts []corev1.ServicePort) []corev1.EndpointPort {
	ret := []corev1.EndpointPort{}
	for _, port := range svcPorts {
		ret = append(ret, corev1.EndpointPort{
			Name:        port.Name,
			Port:        port.Port,
			Protocol:    port.Protocol,
			AppProtocol: port.AppProtocol,
		})
	}
	return ret
}

func (c *Controller) processService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	obj, exists, err := c.serviceLister.GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	service := obj.(*corev1.Service)
	selectorStr, exists := service.Annotations["vm-selector"]
	if !exists {
		return nil
	}
	selector, err := labels.Parse(selectorStr)
	if err != nil {
		return nil
	}

	vms := c.vmInstLister.List()
	subsets := []corev1.EndpointSubset{}
	for _, obj := range vms {
		vm := obj.(*vminstv1a1.VMInstance)
		if vm.Namespace != namespace {
			continue
		}
		if !selector.Matches(labels.Set(vm.Labels)) {
			continue
		}

		subsets = append(subsets, corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{
				{
					Hostname: vm.Spec.HostName,
					IP:       vm.Spec.IP,
				},
			},
			Ports: getEndpointPortsFromServicePorts(service.Spec.Ports),
		})
	}
	subsets = endpoints.RepackSubsets(subsets)

	// Create or update Endpoints
	obj, exists, err = c.endpointsLister.GetByKey(namespace + "/" + name)
	var curEps *corev1.Endpoints
	if exists {
		curEps = obj.(*corev1.Endpoints)
		if apiequality.Semantic.DeepEqual(curEps.Subsets, subsets) {
			klog.V(0).Infof("No need to update for %s", name)
			return nil
		}
	} else {
		curEps = &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: service.Labels,
			},
		}
	}

	newEps := curEps.DeepCopy()
	newEps.Subsets = subsets
	newEps.Labels = service.Labels

	if !exists {
		_, err = c.kubeClient.CoreV1().Endpoints(namespace).Create(context.TODO(), newEps, metav1.CreateOptions{})
	} else {
		_, err = c.kubeClient.CoreV1().Endpoints(namespace).Update(context.TODO(), newEps, metav1.UpdateOptions{})
	}
	klog.V(0).Infof("Updated Endpoints for %s", name)

	return err
}
