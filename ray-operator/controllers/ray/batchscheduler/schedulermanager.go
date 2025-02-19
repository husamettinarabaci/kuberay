package batchscheduler

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/volcano"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
)

var schedulerContainers = map[string]schedulerinterface.BatchSchedulerFactory{
	schedulerinterface.GetDefaultPluginName(): &schedulerinterface.DefaultBatchSchedulerFactory{},
	volcano.GetPluginName():                   &volcano.VolcanoBatchSchedulerFactory{},
}

func GetRegisteredNames() []string {
	var pluginNames []string
	for key := range schedulerContainers {
		pluginNames = append(pluginNames, key)
	}
	return pluginNames
}

func ConfigureReconciler(b *builder.Builder) *builder.Builder {
	for _, factory := range schedulerContainers {
		b = factory.ConfigureReconciler(b)
	}
	return b
}

func AddToScheme(scheme *runtime.Scheme) {
	for _, factory := range schedulerContainers {
		factory.AddToScheme(scheme)
	}
}

type SchedulerManager struct {
	sync.Mutex
	config  *rest.Config
	plugins map[string]schedulerinterface.BatchScheduler
}

func NewSchedulerManager(config *rest.Config) *SchedulerManager {
	manager := SchedulerManager{
		config:  config,
		plugins: make(map[string]schedulerinterface.BatchScheduler),
	}
	return &manager
}

func (batch *SchedulerManager) GetSchedulerForCluster(app *rayv1alpha1.RayCluster) (schedulerinterface.BatchScheduler, error) {
	if schedulerName, ok := app.ObjectMeta.Labels[common.RaySchedulerName]; ok {
		return batch.GetScheduler(schedulerName)
	}

	// no scheduler provided
	return &schedulerinterface.DefaultBatchScheduler{}, nil
}

func (batch *SchedulerManager) GetScheduler(schedulerName string) (schedulerinterface.BatchScheduler, error) {
	factory, registered := schedulerContainers[schedulerName]
	if !registered {
		return nil, fmt.Errorf("unregistered scheduler plugin %s", schedulerName)
	}

	batch.Lock()
	defer batch.Unlock()

	if plugin, existed := batch.plugins[schedulerName]; existed && plugin != nil {
		return plugin, nil
	} else if existed && plugin == nil {
		return nil, fmt.Errorf(
			"failed to get scheduler plugin %s, previous initialization has failed", schedulerName)
	} else {
		if plugin, err := factory.New(batch.config); err != nil {
			batch.plugins[schedulerName] = nil
			return nil, err
		} else {
			batch.plugins[schedulerName] = plugin
			return plugin, nil
		}
	}
}
