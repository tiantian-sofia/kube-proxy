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
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-openapi/pkg/util"
)

// TestOpenAPIModelNames verifies that zz_generated.model_name.go is in sync
// with the types in this package: every struct type defined here must
// implement util.OpenAPIModelNamer, and the returned name must match the
// canonical name derived from the package path and type name. Hand-editing
// the generated file, or adding a type without re-running openapi-gen, breaks
// this test.
func TestOpenAPIModelNames(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("unexpected error adding to scheme: %v", err)
	}

	checked := map[reflect.Type]bool{}
	check := func(rtype reflect.Type) {
		t.Helper()
		if checked[rtype] {
			return
		}
		checked[rtype] = true

		namer, ok := reflect.New(rtype).Interface().(util.OpenAPIModelNamer)
		if !ok {
			t.Errorf("type %v does not implement util.OpenAPIModelNamer; regenerate zz_generated.model_name.go", rtype)
			return
		}
		expected := util.ToRESTFriendlyName(rtype.PkgPath() + "." + rtype.Name())
		if got := namer.OpenAPIModelName(); got != expected {
			t.Errorf("type %v: expected OpenAPI model name %v, got %v", rtype, expected, got)
		}
	}

	// Types registered in the scheme.
	for gvk := range scheme.AllKnownTypes() {
		if gvk.Version == runtime.APIVersionInternal {
			continue
		}
		example, err := scheme.New(gvk)
		if err != nil {
			t.Fatalf("unexpected error creating example for %v: %v", gvk, err)
		}
		check(reflect.TypeOf(example).Elem())
	}

	// All struct types defined in this package that are reachable from the
	// root configuration type, including nested ones not in the scheme.
	walkPackageTypes(reflect.TypeOf(KubeProxyConfiguration{}), check)
}

// walkPackageTypes recursively walks the fields of rtype and calls check for
// every struct type declared in this package.
func walkPackageTypes(rtype reflect.Type, check func(reflect.Type)) {
	for rtype.Kind() == reflect.Ptr || rtype.Kind() == reflect.Slice || rtype.Kind() == reflect.Array || rtype.Kind() == reflect.Map {
		if rtype.Kind() == reflect.Map {
			walkPackageTypes(rtype.Elem(), check)
			rtype = rtype.Key()
			continue
		}
		rtype = rtype.Elem()
	}
	if rtype.Kind() != reflect.Struct || rtype.PkgPath() != reflect.TypeOf(KubeProxyConfiguration{}).PkgPath() {
		return
	}
	check(rtype)
	for i := 0; i < rtype.NumField(); i++ {
		walkPackageTypes(rtype.Field(i).Type, check)
	}
}
