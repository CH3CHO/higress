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

package direct

import (
	"testing"

	"istio.io/api/networking/v1alpha3"

	apiv1 "github.com/alibaba/higress/v2/api/networking/v1"
	kubeutil "github.com/alibaba/higress/v2/pkg/ingress/kube/util"
)

func TestGenerateDestinationRule_HTTPS_SkipVerify(t *testing.T) {
	w := &watcher{}
	se := &v1alpha3.ServiceEntry{
		Hosts: []string{"api.example.com"},
		Ports: []*v1alpha3.ServicePort{
			{Number: 8443, Protocol: "HTTPS", Name: "https"},
		},
	}

	dr := w.generateDestinationRule(se)
	if dr == nil {
		t.Fatal("generateDestinationRule returned nil for HTTPS service entry")
	}
	if dr.Host != "api.example.com" {
		t.Errorf("expected Host=api.example.com, got %q", dr.Host)
	}
	if len(dr.TrafficPolicy.PortLevelSettings) != 1 {
		t.Fatalf("expected 1 port level setting, got %d", len(dr.TrafficPolicy.PortLevelSettings))
	}
	if dr.TrafficPolicy.PortLevelSettings[0].Port.Number != 8443 {
		t.Errorf("expected port 8443, got %d", dr.TrafficPolicy.PortLevelSettings[0].Port.Number)
	}

	tls := dr.TrafficPolicy.PortLevelSettings[0].Tls
	if tls == nil {
		t.Fatal("expected TLS settings, got nil")
	}
	if tls.Mode != v1alpha3.ClientTLSSettings_SIMPLE {
		t.Errorf("expected Mode=SIMPLE, got %v", tls.Mode)
	}
	if tls.InsecureSkipVerify == nil || !tls.InsecureSkipVerify.Value {
		t.Errorf("expected InsecureSkipVerify.Value=true, got %v", tls.InsecureSkipVerify)
	}
}

func TestGenerateDestinationRule_HTTP_ReturnsNil(t *testing.T) {
	w := &watcher{}
	se := &v1alpha3.ServiceEntry{
		Hosts: []string{"api.example.com"},
		Ports: []*v1alpha3.ServicePort{
			{Number: 8080, Protocol: "HTTP", Name: "http"},
		},
	}

	dr := w.generateDestinationRule(se)
	if dr != nil {
		t.Errorf("expected nil for HTTP service entry, got %+v", dr)
	}
}

func TestGenerateDestinationRule_MatchesHelper(t *testing.T) {
	w := &watcher{RegistryConfig: apiv1.RegistryConfig{Sni: "explicit-sni.example.com"}}
	se := &v1alpha3.ServiceEntry{
		Hosts: []string{"api.example.com"},
		Ports: []*v1alpha3.ServicePort{
			{Number: 8443, Protocol: "HTTPS", Name: "https"},
		},
	}

	dr := w.generateDestinationRule(se)
	tls := dr.TrafficPolicy.PortLevelSettings[0].Tls
	want := kubeutil.DefaultSkipSimpleTLS("explicit-sni.example.com")

	if tls.Sni != want.Sni {
		t.Errorf("Sni mismatch: want %q, got %q", want.Sni, tls.Sni)
	}
	if tls.Mode != want.Mode {
		t.Errorf("Mode mismatch: want %v, got %v", want.Mode, tls.Mode)
	}
	if (tls.InsecureSkipVerify == nil) != (want.InsecureSkipVerify == nil) {
		t.Errorf("InsecureSkipVerify nilness mismatch: want %v, got %v", want.InsecureSkipVerify, tls.InsecureSkipVerify)
	}
	if tls.InsecureSkipVerify != nil && tls.InsecureSkipVerify.Value != want.InsecureSkipVerify.Value {
		t.Errorf("InsecureSkipVerify.Value mismatch: want %v, got %v", want.InsecureSkipVerify.Value, tls.InsecureSkipVerify.Value)
	}
}
