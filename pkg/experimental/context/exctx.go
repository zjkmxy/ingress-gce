package context

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	ingctx "k8s.io/ingress-gce/pkg/context"
	migconfigclient "k8s.io/ingress-gce/pkg/experimental/migconfig/client/clientset/versioned"
	informermigconfig "k8s.io/ingress-gce/pkg/experimental/migconfig/client/informers/externalversions/migconfig/v1alpha1"
	vminstclient "k8s.io/ingress-gce/pkg/experimental/vminstance/client/clientset/versioned"
	informervminst "k8s.io/ingress-gce/pkg/experimental/vminstance/client/informers/externalversions/vminstance/v1alpha1"
	"k8s.io/ingress-gce/pkg/utils"
)

// ExControllerContext holds the state needed for the execution of the controller.
type ExControllerContext struct {
	MigConfigInformer cache.SharedIndexInformer
	VMInstInformer    cache.SharedIndexInformer
}

// NewControllerContext returns a set of informers
func NewControllerContext(
	kubeClient kubernetes.Interface,
	migConfigClient migconfigclient.Interface,
	vmInstClient vminstclient.Interface,
	config ingctx.ControllerContextConfig) *ExControllerContext {
	context := &ExControllerContext{
		MigConfigInformer: informermigconfig.NewMigConfigInformer(migConfigClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
		VMInstInformer:    informervminst.NewVMInstanceInformer(vmInstClient, config.Namespace, config.ResyncPeriod, utils.NewNamespaceIndexer()),
	}
	return context
}

// Start all of the informers.
func (vmctx *ExControllerContext) Start(stopCh chan struct{}) {
	go vmctx.MigConfigInformer.Run(stopCh)
	go vmctx.VMInstInformer.Run(stopCh)
}

func (vmctx *ExControllerContext) HasSynced() bool {
	return vmctx.MigConfigInformer.HasSynced() && vmctx.VMInstInformer.HasSynced()
}
