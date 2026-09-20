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
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

// updateFixtureDataEnvVar mirrors component-base's componentconfigtesting:
// when set to "true", mismatched or missing golden files are regenerated.
const updateFixtureDataEnvVar = "UPDATE_COMPONENTCONFIG_FIXTURE_DATA"

// TestRoundTripTypes verifies that the serialized form of the types in this
// package is stable: decoding the golden YAML files under
// testdata/<Kind>/roundtrip/<case>/<version>.yaml and encoding the resulting
// object again must reproduce the golden file byte-for-byte. Removing or
// renaming a field, changing a json tag, or changing a field's type breaks
// this test.
//
// This intentionally does not use componentconfigtesting.RoundTripTest: that
// helper assumes an internal ("__internal") API version exists and decodes
// through it, while this package only has the external v1alpha1 types. The
// testdata layout convention is kept so the fixtures remain familiar.
func TestRoundTripTypes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("unexpected error adding to scheme: %v", err)
	}
	codecs := serializer.NewCodecFactory(scheme)

	serializerInfo, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), runtime.ContentTypeYAML)
	if !ok {
		t.Fatalf("unable to locate encoder -- %q is not a supported media type", runtime.ContentTypeYAML)
	}

	for gvk := range scheme.AllKnownTypes() {
		if gvk.Version == runtime.APIVersionInternal {
			continue
		}
		// Decode into and encode from the external version directly; there is
		// no internal version to convert through.
		codec := codecs.CodecForVersions(serializerInfo.Serializer, codecs.UniversalDeserializer(), gvk.GroupVersion(), gvk.GroupVersion())

		testdir := filepath.Join("testdata", gvk.Kind, "roundtrip")
		dirs, err := os.ReadDir(testdir)
		if err != nil {
			t.Fatalf("failed to read testdir %s: %v", testdir, err)
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			golden := filepath.Join(testdir, dir.Name(), gvk.Version+".yaml")
			t.Run(gvk.Kind+"_"+dir.Name(), func(t *testing.T) {
				roundTripToGolden(t, codec, golden)
			})
		}
	}
}

func roundTripToGolden(t *testing.T, codec runtime.Codec, golden string) {
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("failed to read golden file %s: %v", golden, err)
	}

	original, err := runtime.Decode(codec, data)
	if err != nil {
		t.Fatalf("failed to decode %s: %v", golden, err)
	}

	encoded, err := runtime.Encode(codec, original)
	if err != nil {
		t.Fatalf("failed to encode object: %v", err)
	}

	// Decoding the re-encoded object must yield the original object.
	roundTripped, err := runtime.Decode(codec, encoded)
	if err != nil {
		t.Fatalf("failed to decode re-encoded object: %v", err)
	}
	if !apiequality.Semantic.DeepEqual(original, roundTripped) {
		t.Fatalf("object was not the same after roundtrip, diff (- want, + got):\n%v", cmp.Diff(original, roundTripped))
	}

	// The re-encoded output must match the golden file byte-for-byte.
	if bytes.Equal(data, encoded) {
		return
	}
	if os.Getenv(updateFixtureDataEnvVar) == "true" {
		if err := os.WriteFile(golden, encoded, 0644); err != nil {
			t.Fatal(err)
		}
		t.Errorf("wrote updated golden file %s... verify, commit, and rerun tests", golden)
		return
	}
	t.Errorf("output does not match golden file %s, diff (- want, + got):\n%s\n"+
		"if the diff is expected because of a new type or a new field, re-run with %s=true to update the fixture data",
		golden, cmp.Diff(string(data), string(encoded)), updateFixtureDataEnvVar)
}

// TestRoundTripTestdataStrictlyDecodable guards the golden files themselves:
// every file must decode in strict mode, so a typo'd or stale field name in
// testdata cannot be silently dropped and weaken TestRoundTripTypes.
func TestRoundTripTestdataStrictlyDecodable(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("unexpected error adding to scheme: %v", err)
	}
	strictDecoder := serializer.NewCodecFactory(scheme, serializer.EnableStrict).UniversalDeserializer()

	err := filepath.WalkDir("testdata", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, _, err := strictDecoder.Decode(data, nil, nil); err != nil {
			t.Errorf("strict decode of %s failed: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk testdata: %v", err)
	}
}
