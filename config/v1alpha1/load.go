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
	"encoding/json"
	"errors"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	jsonSerializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"sigs.k8s.io/yaml"
)

// Sentinel errors returned by LoadFromBytes and LoadFromFile. They are intended
// to be used with errors.Is, so callers can distinguish between an input that
// contains no document at all, an input that contains only comments, and an
// input whose YAML/JSON is syntactically or structurally invalid.
var (
	// ErrEmptyConfiguration is returned when the input does not contain a
	// configuration document. This covers empty/whitespace-only input and
	// documents whose root node is null ("null" or "~" in YAML).
	ErrEmptyConfiguration = errors.New("configuration is empty")

	// ErrConfigurationCommentOnly is returned when the input contains only
	// YAML comments, document markers and blank lines, but no actual
	// configuration document.
	ErrConfigurationCommentOnly = errors.New("configuration contains only comments and no document")

	// ErrInvalidConfiguration is returned when the input cannot be parsed as
	// YAML/JSON, does not have a mapping as its root node, or (in strict mode)
	// contains fields that are not part of KubeProxyConfiguration.
	ErrInvalidConfiguration = errors.New("invalid KubeProxyConfiguration")
)

var (
	// loadScheme is intentionally package-private: registering extra types into
	// a shared scheme to make decoding convenient would break the
	// VerifyExternalTypePackage checks exercised by register_test.go.
	loadScheme = func() *runtime.Scheme {
		scheme := runtime.NewScheme()
		if err := AddToScheme(scheme); err != nil {
			panic(err)
		}
		return scheme
	}()

	// Decoding straight into the concrete *KubeProxyConfiguration avoids the
	// "no kind ... is registered for the internal version" failure that
	// UniversalDecoder().Decode(data, nil, nil) hits: this package has no
	// internal types, so the scheme cannot materialize an object from the
	// wire-format GVK alone.
	lenientDecoder = jsonSerializer.NewSerializerWithOptions(
		jsonSerializer.DefaultMetaFactory, loadScheme, loadScheme,
		jsonSerializer.SerializerOptions{Yaml: true, Strict: false},
	)
	strictDecoder = jsonSerializer.NewSerializerWithOptions(
		jsonSerializer.DefaultMetaFactory, loadScheme, loadScheme,
		jsonSerializer.SerializerOptions{Yaml: true, Strict: true},
	)
)

// LoadOptions controls how LoadFromBytes and LoadFromFile interpret input.
type LoadOptions struct {
	// Strict rejects YAML/JSON documents that contain duplicate keys or fields
	// that are not part of KubeProxyConfiguration (including case mismatches
	// such as "clusterCidr" instead of "clusterCIDR"). Strict errors report the
	// full dotted path of the offending field, e.g. "iptables.bogusField".
	// When false, unknown fields are silently ignored, matching the behavior of
	// the non-strict API machinery decoders.
	Strict bool
}

// APIVersionMismatchError is returned when the input document's apiVersion is
// not the KubeProxyConfiguration v1alpha1 API version.
type APIVersionMismatchError struct {
	// Expected is the apiVersion that was required.
	Expected string
	// Actual is the apiVersion found in the document; it is empty when the
	// document does not set apiVersion at all.
	Actual string
}

// Error implements error.
func (e *APIVersionMismatchError) Error() string {
	if e.Actual == "" {
		return fmt.Sprintf("unexpected apiVersion: expected %q, got \"\" (apiVersion is missing)", e.Expected)
	}
	return fmt.Sprintf("unexpected apiVersion: expected %q, got %q", e.Expected, e.Actual)
}

// KindMismatchError is returned when the input document's kind is not
// KubeProxyConfiguration.
type KindMismatchError struct {
	// Expected is the kind that was required.
	Expected string
	// Actual is the kind found in the document; it is empty when the document
	// does not set kind at all.
	Actual string
}

// Error implements error.
func (e *KindMismatchError) Error() string {
	if e.Actual == "" {
		return fmt.Sprintf("unexpected kind: expected %q, got \"\" (kind is missing)", e.Expected)
	}
	return fmt.Sprintf("unexpected kind: expected %q, got %q", e.Expected, e.Actual)
}

// LoadFromBytes parses a serialized KubeProxyConfiguration. Both YAML and JSON
// inputs are accepted, as the YAML serializer accepts the JSON subset.
//
// Empty input, comment-only input and malformed YAML/JSON are reported as
// distinct errors (see ErrEmptyConfiguration, ErrConfigurationCommentOnly and
// ErrInvalidConfiguration). A non-empty document must set
// kubeproxy.config.k8s.io/v1alpha1 as apiVersion and KubeProxyConfiguration as
// kind; mismatches are reported with the expected and actual values.
func LoadFromBytes(data []byte, opts LoadOptions) (*KubeProxyConfiguration, error) {
	content := classifyContent(data)
	switch content.kind {
	case contentEmpty:
		return nil, fmt.Errorf("%w: input is empty or contains only whitespace", ErrEmptyConfiguration)
	case contentMarkersOnly:
		return nil, fmt.Errorf("%w: input contains only YAML document markers (\"---\"/\"...\")", ErrEmptyConfiguration)
	case contentCommentsOnly:
		return nil, fmt.Errorf("%w: input contains only YAML comments", ErrConfigurationCommentOnly)
	}

	// The serializer does this conversion internally as well; doing it here
	// first lets us classify syntax failures before touching the API machinery
	// decoder, and gives us canonical JSON for the structural checks below.
	jsonData, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("%w: yaml/json parse error: %v", ErrInvalidConfiguration, err)
	}

	if isJSONNull(jsonData) {
		return nil, fmt.Errorf("%w: document root is null (\"null\"/\"~\" is not a KubeProxyConfiguration)", ErrEmptyConfiguration)
	}
	if rootKind := jsonRootKind(jsonData); rootKind != jsonRootMapping {
		return nil, fmt.Errorf("%w: document root must be a YAML/JSON mapping, got %s", ErrInvalidConfiguration, rootKind)
	}

	// Validate apiVersion/kind up front, with explicit expected-vs-actual
	// errors. The API machinery decoder only reports missing kinds/versions and
	// otherwise happily fills whatever object it is handed.
	var typeMeta metav1.TypeMeta
	if err := json.Unmarshal(jsonData, &typeMeta); err != nil {
		return nil, fmt.Errorf("%w: could not read apiVersion/kind: %v", ErrInvalidConfiguration, err)
	}
	expectedAPIVersion := SchemeGroupVersion.Identifier()
	var mismatchErrs []error
	if typeMeta.APIVersion != expectedAPIVersion {
		mismatchErrs = append(mismatchErrs, &APIVersionMismatchError{
			Expected: expectedAPIVersion,
			Actual:   typeMeta.APIVersion,
		})
	}
	if typeMeta.Kind != "KubeProxyConfiguration" {
		mismatchErrs = append(mismatchErrs, &KindMismatchError{
			Expected: "KubeProxyConfiguration",
			Actual:   typeMeta.Kind,
		})
	}
	if err := errors.Join(mismatchErrs...); err != nil {
		return nil, err
	}

	decoder := lenientDecoder
	if opts.Strict {
		decoder = strictDecoder
	}
	config := &KubeProxyConfiguration{}
	if _, _, err := decoder.Decode(data, nil, config); err != nil {
		// Strict errors already carry the dotted field path, e.g.
		// `strict decoding error: unknown field "iptables.bogusField"`.
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfiguration, err)
	}
	return config, nil
}

// LoadFromFile reads the file at path and passes its contents through
// LoadFromBytes. Filesystem errors are returned unwrapped (so errors.Is with
// fs.ErrNotExist and friends keeps working); see LoadFromBytes for the
// semantics of parse errors.
func LoadFromFile(path string, opts LoadOptions) (*KubeProxyConfiguration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadFromBytes(data, opts)
}

type contentKind int

const (
	contentNonEmpty contentKind = iota
	contentEmpty
	contentMarkersOnly
	contentCommentsOnly
)

type contentClassification struct {
	kind contentKind
}

// classifyContent inspects the raw input to distinguish situations that are
// indistinguishable after YAML-to-JSON conversion: empty input, input with
// only document markers, and input with only comments all decode to "null".
func classifyContent(data []byte) contentClassification {
	sawMarker := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		switch {
		case len(trimmed) == 0:
			continue
		case trimmed[0] == '#':
			continue
		case bytes.Equal(trimmed, []byte("---")) || bytes.Equal(trimmed, []byte("...")):
			sawMarker = true
			continue
		default:
			return contentClassification{kind: contentNonEmpty}
		}
	}
	if sawMarker {
		return contentClassification{kind: contentMarkersOnly}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return contentClassification{kind: contentEmpty}
	}
	return contentClassification{kind: contentCommentsOnly}
}

func isJSONNull(data []byte) bool {
	return bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

type jsonRoot string

const (
	jsonRootUnknown jsonRoot = "unknown value"
	jsonRootMapping jsonRoot = "mapping"
	jsonRootArray   jsonRoot = "array"
	jsonRootString  jsonRoot = "string"
	jsonRootNumber  jsonRoot = "number"
	jsonRootBoolean jsonRoot = "boolean"
	jsonRootNull    jsonRoot = "null"
)

// jsonRootKind reports the kind of the root value of canonical JSON produced
// by yaml.YAMLToJSON.
func jsonRootKind(data []byte) jsonRoot {
	switch bytes.TrimSpace(data)[0] {
	case '{':
		return jsonRootMapping
	case '[':
		return jsonRootArray
	case '"':
		return jsonRootString
	case 't', 'f':
		return jsonRootBoolean
	case 'n':
		return jsonRootNull
	default:
		return jsonRootNumber
	}
}
