/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	componentconfigtesting "k8s.io/component-base/config/testing"
)

// TestRoundTrip ensures that the serialized form of KubeProxyConfiguration
// stays stable: every testdata/KubeProxyConfiguration/roundtrip/<case>/v1alpha1.yaml
// file must decode and re-encode to byte-identical output. Removing a field,
// renaming a json tag, or changing a field type breaks this test.
//
// If a change to the serialized form is intentional, regenerate the fixtures
// with UPDATE_COMPONENTCONFIG_FIXTURE_DATA=true and review the diff carefully.
func TestRoundTrip(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("unexpected error adding to scheme: %v", err)
	}
	// componentconfigtesting.RoundTripTest decodes into the internal version
	// of each group. This package has no internal types, so register the
	// external types under the internal version as well; with identical Go
	// types on both sides the scheme conversion is a no-op.
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: GroupName, Version: runtime.APIVersionInternal},
		&KubeProxyConfiguration{},
	)
	codecs := serializer.NewCodecFactory(scheme)

	componentconfigtesting.RoundTripTest(t, scheme, codecs)
}
