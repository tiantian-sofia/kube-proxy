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
	"bytes"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fieldTypesGolden is a fingerprint of the Go types of every field of every
// struct in this package. Serialization alone cannot distinguish some type
// changes (e.g. metav1.Duration vs *metav1.Duration both serialize as "0s"),
// so the exact reflect.Type of each field is pinned here.
//
// If a type change is intentional, regenerate with
// UPDATE_COMPONENTCONFIG_FIXTURE_DATA=true and review the diff carefully.
const fieldTypesGolden = "testdata/KubeProxyConfiguration/fieldtypes.txt"

func TestFieldTypes(t *testing.T) {
	thisPackage := reflect.TypeOf(KubeProxyConfiguration{}).PkgPath()

	var lines []string
	seen := map[reflect.Type]bool{}
	var walk func(typ reflect.Type)
	walk = func(typ reflect.Type) {
		if typ.Kind() != reflect.Struct || typ.PkgPath() != thisPackage || seen[typ] {
			return
		}
		seen[typ] = true
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			lines = append(lines, fmt.Sprintf("%s.%s: %s", typ.Name(), field.Name, qualifiedTypeName(field.Type)))
			walk(field.Type)
		}
	}
	walk(reflect.TypeOf(KubeProxyConfiguration{}))
	sort.Strings(lines)
	actual := []byte(strings.Join(lines, "\n") + "\n")

	expected, err := os.ReadFile(fieldTypesGolden)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("couldn't read test data: %v", err)
	}

	const updateEnvVar = "UPDATE_COMPONENTCONFIG_FIXTURE_DATA"
	if err == nil && bytes.Equal(expected, actual) {
		return
	}
	if os.Getenv(updateEnvVar) == "true" {
		if err := os.WriteFile(fieldTypesGolden, actual, 0644); err != nil {
			t.Fatal(err)
		}
		t.Error("wrote expected test data... verify, commit, and rerun tests")
		return
	}
	if os.IsNotExist(err) {
		t.Fatalf("couldn't find test data %s; generate it with %s=true", fieldTypesGolden, updateEnvVar)
	}
	t.Errorf("field types do not match %s; if this change is intentional, re-run with %s=true\ndiff (- want, + got):\n%s",
		fieldTypesGolden, updateEnvVar, diffLines(string(expected), string(actual)))
}

// qualifiedTypeName renders a reflect.Type with full package paths so that
// e.g. metav1.Duration and *metav1.Duration are unambiguous.
func qualifiedTypeName(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Ptr:
		return "*" + qualifiedTypeName(typ.Elem())
	case reflect.Slice:
		return "[]" + qualifiedTypeName(typ.Elem())
	case reflect.Array:
		return fmt.Sprintf("[%d]%s", typ.Len(), qualifiedTypeName(typ.Elem()))
	case reflect.Map:
		return "map[" + qualifiedTypeName(typ.Key()) + "]" + qualifiedTypeName(typ.Elem())
	}
	if typ.Name() == "" {
		return typ.String()
	}
	if typ.PkgPath() == "" {
		return typ.Name()
	}
	return typ.PkgPath() + "." + typ.Name()
}

func diffLines(expected, actual string) string {
	var buf bytes.Buffer
	expectedLines := strings.Split(strings.TrimRight(expected, "\n"), "\n")
	actualLines := strings.Split(strings.TrimRight(actual, "\n"), "\n")
	expectedSet := map[string]bool{}
	for _, l := range expectedLines {
		expectedSet[l] = true
	}
	actualSet := map[string]bool{}
	for _, l := range actualLines {
		actualSet[l] = true
	}
	for _, l := range expectedLines {
		if !actualSet[l] {
			fmt.Fprintf(&buf, "- %s\n", l)
		}
	}
	for _, l := range actualLines {
		if !expectedSet[l] {
			fmt.Fprintf(&buf, "+ %s\n", l)
		}
	}
	return buf.String()
}
