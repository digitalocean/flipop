package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned/scheme"
	"github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	"github.com/digitalocean/flipop/pkg/metacontext"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
)

type fakeMetricsProvider struct {
	err     error
	metrics *metrics
}

func (f *fakeMetricsProvider) DoSomething(ctx context.Context) (err error) {
	done := f.metrics.startCall(ctx)
	defer done(&err)
	return f.err
}

func TestMetricsStartCall(t *testing.T) {
	ctx := context.Background()
	f := &fakeMetricsProvider{}
	f.metrics = initMetrics().withProvider(Mock)

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

	ctx = metacontext.WithKubeObject(ctx, kubeObj)
	f.DoSomething(ctx)

	f.err = errors.New("err")
	f.DoSomething(ctx)

	ch := make(chan prometheus.Metric, 200)
	f.metrics.callsTotal.Collect(ch)
	close(ch)
	var labelSets []map[string]string
	for m := range ch {
		d := &dto.Metric{}
		m.Write(d)
		v := *d.Counter.Value
		assert.Equal(t, float64(1), v)
		labels := make(map[string]string)
		for _, l := range d.Label {
			labels[l.GetName()] = l.GetValue()
		}
		labelSets = append(labelSets, labels)
	}

	assert.ElementsMatch(t, []map[string]string{
		{
			"provider":  Mock,
			"outcome":   "success",
			"call":      "DoSomething",
			"namespace": "obj-namespace",
			"name":      "obj-name",
			"kind":      "FloatingIPPool",
		},
		{
			"provider":  Mock,
			"outcome":   "error",
			"call":      "DoSomething",
			"namespace": "obj-namespace",
			"name":      "obj-name",
			"kind":      "FloatingIPPool",
		},
	}, labelSets)

}
