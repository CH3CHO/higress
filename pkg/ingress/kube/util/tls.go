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

package util

import (
	"google.golang.org/protobuf/types/known/wrapperspb"
	"istio.io/api/networking/v1alpha3"
)

// DefaultSkipSimpleTLS constructs a ClientTLSSettings that defaults to skipping
// certificate verification on a SIMPLE TLS connection. This makes self-signed
// or internal-CA backends work out-of-the-box when Higress is the client.
//
// Users who want strict verification should not use this helper: provide a
// BackendTLSPolicy with explicit SubjectAltNames / CredentialName, or set
// `proxy-ssl-verify: on` for Ingress annotation paths.
func DefaultSkipSimpleTLS(sni string) *v1alpha3.ClientTLSSettings {
	return &v1alpha3.ClientTLSSettings{
		Mode:               v1alpha3.ClientTLSSettings_SIMPLE,
		Sni:                sni,
		InsecureSkipVerify: &wrapperspb.BoolValue{Value: true},
	}
}
