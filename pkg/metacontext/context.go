package metacontext

import (
	"context"

	"github.com/digitalocean/flipop/pkg/log"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type kubeMetadata struct {
	Kind      string
	Namespace string
	Name      string
}

type kubeMetadataKey struct{}

// WithKubeObject returns adds info about a kubernetes object to the context.
func WithKubeObject(ctx context.Context, obj metav1.Object) context.Context {
	m := &kubeMetadata{}
	m.Namespace = obj.GetNamespace()
	m.Name = obj.GetName()
	if t, err := meta.TypeAccessor(obj); err == nil {
		m.Kind = t.GetKind()
	}
	ctx = context.WithValue(ctx, kubeMetadataKey{}, m)
	return ctx
}

// KubeMetadataFromContext loads metadata from the provided context.
func KubeMetadataFromContext(ctx context.Context) (ok bool, namespace, name, kind string) {
	v := ctx.Value(kubeMetadataKey{})
	if m, ok := v.(*kubeMetadata); ok && m != nil {
		return true, m.Namespace, m.Name, m.Kind
	}
	return false, "", "", ""
}

// AddKubeMetadataToLogger to the provided logger, or create a new one.
func AddKubeMetadataToLogger(ctx context.Context, ll logrus.FieldLogger) logrus.FieldLogger {
	if ll == nil {
		ll = log.FromContext(ctx)
	}
	ok, namespace, name, kind := KubeMetadataFromContext(ctx)
	if !ok {
		return ll
	}
	return ll.WithFields(logrus.Fields{
		"namespace": namespace,
		"name":      name,
		"kind":      kind,
	})
}
