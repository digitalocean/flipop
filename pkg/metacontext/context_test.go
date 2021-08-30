package metacontext

import (
	"context"
	"testing"

	"github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned/scheme"
	"github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
)

func TestWithKubeObject(t *testing.T) {
	kubeObj := &v1alpha1.FloatingIPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "obj-name",
			Namespace: "obj-namespace",
			UID:       uuid.NewUUID(),
		},
	}
	// This normally happens on the APIserver, but locally we have to do it ourselves.
	kinds, ok, err := scheme.Scheme.ObjectKinds(kubeObj)
	require.False(t, ok)
	require.NoError(t, err)
	require.Len(t, kinds, 1)
	kubeObj.SetGroupVersionKind(kinds[0])
	tcs := []struct {
		name            string
		ctx             context.Context
		expectOk        bool
		expectName      string
		expectNamespace string
		expectKind      string
	}{
		{
			name:            "success",
			ctx:             WithKubeObject(context.Background(), kubeObj),
			expectOk:        true,
			expectName:      "obj-name",
			expectNamespace: "obj-namespace",
			expectKind:      "FloatingIPPool",
		},
		{
			name:     "no kube metadata on context",
			ctx:      context.Background(),
			expectOk: false,
		},
	}
	for _, tc := range tcs {
		ok, namespace, name, kind := KubeMetadataFromContext(tc.ctx)
		assert.Equal(t, tc.expectOk, ok)
		assert.Equal(t, tc.expectNamespace, namespace)
		assert.Equal(t, tc.expectName, name)
		assert.Equal(t, tc.expectKind, kind)
	}

}
