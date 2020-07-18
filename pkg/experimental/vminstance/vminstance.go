package vminstance

import (
	apisvminstance "k8s.io/ingress-gce/pkg/apis/vminstance"
	vminstancev1a1 "k8s.io/ingress-gce/pkg/apis/vminstance/v1alpha1"
	"k8s.io/ingress-gce/pkg/crd"
)

func CRDMeta() *crd.CRDMeta {
	meta := crd.NewCRDMeta(
		apisvminstance.GroupName,
		"v1alpha1",
		"VMInstance",
		"VMInstanceList",
		"vminstance",
		"vminstances",
		"vm",
	)
	meta.AddValidationInfo("k8s.io/ingress-gce/pkg/apis/vminstance/v1alpha1.VMInstance", vminstancev1a1.GetOpenAPIDefinitions)
	return meta
}
