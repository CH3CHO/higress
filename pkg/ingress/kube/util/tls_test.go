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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	networking "istio.io/api/networking/v1alpha3"
)

func TestDefaultSkipSimpleTLS(t *testing.T) {
	testCases := []struct {
		name string
		sni  string
		want *networking.ClientTLSSettings
	}{
		{
			name: "with sni",
			sni:  "api.example.com",
			want: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_SIMPLE,
				Sni:  "api.example.com",
				InsecureSkipVerify: &wrappers.BoolValue{
					Value: true,
				},
			},
		},
		{
			name: "empty sni",
			sni:  "",
			want: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_SIMPLE,
				Sni:  "",
				InsecureSkipVerify: &wrappers.BoolValue{
					Value: true,
				},
			},
		},
	}

	unexportedIgnoredTypes := []interface{}{
		networking.ClientTLSSettings{},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultSkipSimpleTLS(tc.sni)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform(),
				cmpopts.IgnoreUnexported(unexportedIgnoredTypes...),
			); diff != "" {
				t.Errorf("DefaultSkipSimpleTLS() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
