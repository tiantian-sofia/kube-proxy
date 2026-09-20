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
	"strings"
	"testing"
)

// openAPIModelNamer mirrors util.OpenAPIModelNamer from k8s.io/kube-openapi,
// which every type in zz_generated.model_name.go implements.
type openAPIModelNamer interface {
	OpenAPIModelName() string
}

// TestOpenAPIModelNames verifies that every struct type in this package
// reachable from KubeProxyConfiguration implements OpenAPIModelName (i.e.
// zz_generated.model_name.go is complete and unmodified) and that each
// returned name matches the canonical name openapi-gen derives from the
// package path and type name.
func TestOpenAPIModelNames(t *testing.T) {
	thisPackage := reflect.TypeOf(KubeProxyConfiguration{}).PkgPath()

	var types []reflect.Type
	seen := map[reflect.Type]bool{}
	var walk func(typ reflect.Type)
	walk = func(typ reflect.Type) {
		for typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		switch typ.Kind() {
		case reflect.Slice, reflect.Array:
			walk(typ.Elem())
			return
		case reflect.Map:
			walk(typ.Key())
			walk(typ.Elem())
			return
		}
		if typ.Kind() != reflect.Struct || typ.PkgPath() != thisPackage || seen[typ] {
			return
		}
		seen[typ] = true
		types = append(types, typ)
		for i := 0; i < typ.NumField(); i++ {
			walk(typ.Field(i).Type)
		}
	}
	walk(reflect.TypeOf(KubeProxyConfiguration{}))

	if len(types) == 0 {
		t.Fatal("found no types in this package reachable from KubeProxyConfiguration")
	}

	for _, typ := range types {
		t.Run(typ.Name(), func(t *testing.T) {
			namer, ok := reflect.New(typ).Elem().Interface().(openAPIModelNamer)
			if !ok {
				t.Fatalf("type %s does not implement OpenAPIModelName(); regenerate zz_generated.model_name.go", typ.Name())
			}
			expected := toRESTFriendlyName(typ.PkgPath() + "." + typ.Name())
			if got := namer.OpenAPIModelName(); got != expected {
				t.Errorf("OpenAPIModelName() = %q, want %q; regenerate zz_generated.model_name.go", got, expected)
			}
		})
	}
}

// toRESTFriendlyName converts a fully qualified Go type name to its OpenAPI
// model name, mirroring util.ToRESTFriendlyName from k8s.io/kube-openapi:
// the domain part of the package path is reversed and slashes become dots,
// e.g. k8s.io/kube-proxy/config/v1alpha1.KubeProxyConfiguration becomes
// io.k8s.kube-proxy.config.v1alpha1.KubeProxyConfiguration.
func toRESTFriendlyName(name string) string {
	nameParts := strings.Split(name, "/")
	if len(nameParts) > 0 && strings.Contains(nameParts[0], ".") {
		parts := strings.Split(nameParts[0], ".")
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		nameParts[0] = strings.Join(parts, ".")
	}
	return strings.Join(nameParts, ".")
}
