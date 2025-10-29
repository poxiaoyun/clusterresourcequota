package install

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	quotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
)

func Install(scheme *runtime.Scheme) {
	utilruntime.Must(quotav1.AddToScheme(scheme))
	utilruntime.Must(scheme.SetVersionPriority(quotav1.SchemeGroupVersion))
}
