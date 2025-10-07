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
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/config"

	. "github.com/alibaba/higress/v2/pkg/ingress/log"
)

type TemplateResourceType string

const (
	TemplateResourceTypeConfigMap = "configmap"
	TemplateResourceTypeSecret    = "secret"
)

var (
	supportedTemplateResourceTypes = map[TemplateResourceType]bool{
		TemplateResourceTypeConfigMap: true,
		TemplateResourceTypeSecret:    true,
	}
	// Find all variables in format:
	// ${type.name.key} or ${type.defaultNamespace/name.key}
	templateVariableRegex = regexp.MustCompile(`\$\{([^.}]+)\.(?:([^/]+)/)?([^.}]+)\.([^}]+)}`)
)

type templateValueGetter func(valueType TemplateResourceType, namespace, name, key string) (string, error)

// TemplateProcessor handles template substitution in configs
type TemplateProcessor struct {
	// getValue is a function that retrieves values by type, namespace, name and key
	getValue          templateValueGetter
	defaultNamespace  string
	templateConfigMgr *TemplateConfigMgr
}

// NewTemplateProcessor creates a new TemplateProcessor with the given value getter function
func NewTemplateProcessor(getValue templateValueGetter, defaultNamespace string, templateConfigMgr *TemplateConfigMgr) *TemplateProcessor {
	return &TemplateProcessor{
		getValue:          getValue,
		defaultNamespace:  defaultNamespace,
		templateConfigMgr: templateConfigMgr,
	}
}

// ProcessConfig processes a config and substitutes any template variables
func (p *TemplateProcessor) ProcessConfig(cfg *config.Config) error {
	// Convert spec to JSON string to process substitutions
	jsonBytes, err := json.Marshal(cfg.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal config spec: %v", err)
	}

	configStr := string(jsonBytes)
	matches := templateVariableRegex.FindAllStringSubmatch(configStr, -1)
	// If there are no value references, return immediately
	if len(matches) == 0 {
		if p.templateConfigMgr != nil {
			if err := p.templateConfigMgr.UpdateTemplateReferences(cfg, nil); err != nil {
				IngressLog.Errorf("failed to delete template resource references for cfg %s/%s: %v", cfg.Namespace, cfg.Name, err)
			}
		}
		return nil
	}

	foundSecretSource := false
	IngressLog.Infof("start to apply config %s/%s with %d variables", cfg.Namespace, cfg.Name, len(matches))
	for _, match := range matches {
		valueType := TemplateResourceType(match[1])
		if !supportedTemplateResourceTypes[valueType] {
			// Unsupported value type. Just keep it as it is.
			continue
		}

		var namespace, name, key string
		if match[2] != "" {
			// Format: ${type.defaultNamespace/name.key}
			namespace = match[2]
		} else {
			// Format: ${type.name.key} - use default namespace
			namespace = p.defaultNamespace
		}
		name = match[3]
		key = match[4]

		// Get value using the provided getter function
		value, err := p.getValue(valueType, namespace, name, key)
		if err != nil {
			return fmt.Errorf("failed to get %s value for %s/%s.%s: %v", valueType, namespace, name, key, err)
		}

		// Add secret dependency if this is a secret reference
		if valueType == "secret" && p.templateConfigMgr != nil {
			foundSecretSource = true
			secretKey := fmt.Sprintf("%s/%s", namespace, name)
			if err := p.templateConfigMgr.AddConfig(secretKey, cfg); err != nil {
				IngressLog.Errorf("failed to add secret dependency: %v", err)
			}
		}
		// Replace placeholder with actual value
		configStr = strings.Replace(configStr, match[0], value, 1)
	}

	// Create a new instance of the same type as cfg.Spec
	newSpec := proto.Clone(cfg.Spec.(proto.Message))
	if err := json.Unmarshal([]byte(configStr), newSpec); err != nil {
		return fmt.Errorf("failed to unmarshal substituted config: %v", err)
	}
	cfg.Spec = newSpec

	// Delete secret dependency if no secret reference is found
	if !foundSecretSource {
		if p.templateConfigMgr != nil {
			if err := p.templateConfigMgr.DeleteConfig(cfg); err != nil {
				IngressLog.Errorf("failed to delete secret dependency: %v", err)
			}
		}
	}

	IngressLog.Infof("end to process config %s/%s", cfg.Namespace, cfg.Name)
	return nil
}
