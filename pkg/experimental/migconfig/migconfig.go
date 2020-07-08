package migconfig

import (
	apismigconfig "k8s.io/ingress-gce/pkg/apis/migconfig"
	migconfigv1a1 "k8s.io/ingress-gce/pkg/apis/migconfig/v1alpha1"
	"k8s.io/ingress-gce/pkg/crd"
)

func CRDMeta() *crd.CRDMeta {
	meta := crd.NewCRDMeta(
		apismigconfig.GroupName,
		"v1alpha1",
		"MigConfig",
		"MigConfigList",
		"migconfig",
		"migconfigs",
		"migc",
	)
	meta.AddValidationInfo("k8s.io/ingress-gce/pkg/apis/migconfig/v1alpha1.MigConfig", migconfigv1a1.GetOpenAPIDefinitions)
	return meta
}
