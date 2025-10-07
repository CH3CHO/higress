// Copyright (c) 2022 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"fmt"
	"sync"

	istiomodel "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"

	"github.com/alibaba/higress/v2/pkg/ingress/kube/util"
	. "github.com/alibaba/higress/v2/pkg/ingress/log"
)

// toConfigKey converts config.Config to istiomodel.ConfigKey
func toConfigKey(cfg *config.Config) istiomodel.ConfigKey {
	return istiomodel.ConfigKey{
		Kind:      kind.MustFromGVK(cfg.GroupVersionKind),
		Name:      cfg.Name,
		Namespace: cfg.Namespace,
	}
}

// toResourceKey converts resource meta fields to a string key in format {type}.{namespace}/{name}
func toResourceKey(resourceType TemplateResourceType, namespace, name string) string {
	return fmt.Sprintf("%s.%s/%s", resourceType, namespace, name)
}

// TemplateConfigMgr maintains the mapping between secrets and configs
type TemplateConfigMgr struct {
	mutex sync.RWMutex

	// configSet tracks all configs that have been added
	// key format: defaultNamespace/name
	configSet sets.Set[istiomodel.ConfigKey]

	// resourceToConfigs maps resource key to dependent configs
	// key format: namespace/name
	resourceToConfigs map[string]sets.Set[istiomodel.ConfigKey]

	// watchedResources tracks which resources are being watched
	watchedResources sets.Set[string]

	// xdsUpdater is used to push config updates
	xdsUpdater istiomodel.XDSUpdater
}

// NewTemplateConfigMgr creates a new TemplateConfigMgr
func NewTemplateConfigMgr(xdsUpdater istiomodel.XDSUpdater) *TemplateConfigMgr {
	mgr := &TemplateConfigMgr{
		resourceToConfigs: make(map[string]sets.Set[istiomodel.ConfigKey]),
		watchedResources:  sets.New[string](),
		configSet:         sets.New[istiomodel.ConfigKey](),
		xdsUpdater:        xdsUpdater,
	}
	return mgr
}

// UpdateTemplateReferences adds a config and its resource references
func (m *TemplateConfigMgr) UpdateTemplateReferences(cfg *config.Config, references []*TemplateResourceReference) error {
	configKey := toConfigKey(cfg)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if len(references) != 0 {
		m.configSet.Insert(configKey)

		for _, ref := range references {
			resourceKey := ref.ToResourceKey()
			if configs, exists := m.resourceToConfigs[resourceKey]; exists {
				configs.Insert(configKey)
			} else {
				m.resourceToConfigs[resourceKey] = sets.New(configKey)
			}
			// Add to watched resources
			m.watchedResources.Insert(resourceKey)
		}
	} else if m.configSet.Contains(configKey) {
		resourceKeysToRemove := make([]string, 0)
		// Find and remove the config from all resources
		for resourceKey, configs := range m.resourceToConfigs {
			if configs.Contains(configKey) {
				configs.Delete(configKey)
				// If no more configs depend on this secret, remove it
				if configs.Len() == 0 {
					resourceKeysToRemove = append(resourceKeysToRemove, resourceKey)
				}
			}
		}

		//  Remove resources from the resourceToConfigs map
		for _, key := range resourceKeysToRemove {
			delete(m.resourceToConfigs, key)
			m.watchedResources.Delete(key)
		}

		// Remove the config from the config set
		m.configSet.Delete(configKey)
	}

	return nil
}

// getConfigsForResource returns all configs that depend on the given secret
func (m *TemplateConfigMgr) getConfigsForResource(resourceKey string) []istiomodel.ConfigKey {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if configs, exists := m.resourceToConfigs[resourceKey]; exists {
		return configs.UnsortedList()
	}
	return nil
}

// isResourceWatched checks if a secret is being watched
func (m *TemplateConfigMgr) isResourceWatched(resourceKey string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.watchedResources.Contains(resourceKey)
}

// HandleResourceChange handles resource changes and updates affected configs
func (m *TemplateConfigMgr) HandleResourceChange(resourceType TemplateResourceType, resourceName util.ClusterNamespacedName) {
	resourceKey := toResourceKey(resourceType, resourceName.Namespace, resourceName.Name)
	// Check if this resource is being watched
	if !m.isResourceWatched(resourceKey) {
		return
	}

	// Get affected configs
	configKeys := m.getConfigsForResource(resourceKey)
	if len(configKeys) == 0 {
		return
	}

	IngressLog.Infof("TemplateConfigMgr resource %s changed, updating %d dependent configs and push", resourceKey, len(configKeys))
	m.xdsUpdater.ConfigUpdate(&istiomodel.PushRequest{
		Full:   true,
		Reason: istiomodel.NewReasonStats("template-resource-changed"),
	})
}

type TemplateResourceReference struct {
	Type      TemplateResourceType
	Namespace string
	Name      string
}

func (r *TemplateResourceReference) ToResourceKey() string {
	return toResourceKey(r.Type, r.Namespace, r.Name)
}
