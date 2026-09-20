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
	"errors"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sjson "sigs.k8s.io/json"
	"sigs.k8s.io/yaml"
)

// Kind is the expected value of the "kind" field in a kube-proxy
// configuration document.
const Kind = "KubeProxyConfiguration"

// Sentinel errors returned by Load and LoadFile. Use errors.Is to
// distinguish them from wrapped forms.
var (
	// ErrEmptyConfig indicates the input contained no bytes other than
	// whitespace.
	ErrEmptyConfig = errors.New("configuration is empty")
	// ErrNoConfigContent indicates the input contained no YAML document
	// content, e.g. only comments or empty document markers.
	ErrNoConfigContent = errors.New("configuration contains no content (only comments or empty documents)")
	// ErrInvalidSyntax indicates the input is neither valid YAML nor valid
	// JSON.
	ErrInvalidSyntax = errors.New("configuration is not valid YAML or JSON")
)

// KindMismatchError is returned when the apiVersion or kind of the
// configuration document does not match this API group and version.
type KindMismatchError struct {
	ExpectedAPIVersion string
	ExpectedKind       string
	ActualAPIVersion   string
	ActualKind         string
}

// Error implements the error interface.
func (e *KindMismatchError) Error() string {
	return fmt.Sprintf("expected apiVersion %q and kind %q, got apiVersion %q and kind %q",
		e.ExpectedAPIVersion, e.ExpectedKind, e.ActualAPIVersion, e.ActualKind)
}

// StrictDecodingError is returned in strict mode when the configuration
// contains fields that are not part of KubeProxyConfiguration, or duplicate
// fields. Each entry in Problems is a dotted path such as
// "iptables.masqueradeBit" identifying one offending field.
type StrictDecodingError struct {
	UnknownFields   []string
	DuplicateFields []string
}

// Error implements the error interface.
func (e *StrictDecodingError) Error() string {
	var problems []string
	for _, field := range e.UnknownFields {
		problems = append(problems, fmt.Sprintf("unknown field %q", field))
	}
	for _, field := range e.DuplicateFields {
		problems = append(problems, fmt.Sprintf("duplicate field %q", field))
	}
	return "strict decoding failed: " + strings.Join(problems, ", ")
}

// LoadOptions configures the behavior of Load and LoadFile.
type LoadOptions struct {
	// Strict rejects configuration documents containing unknown or
	// duplicate fields. This catches misspelled or misplaced keys that
	// would otherwise be silently dropped, leaving zero values behind.
	Strict bool
}

// Load decodes a kube-proxy configuration document (YAML or JSON) into a
// KubeProxyConfiguration.
//
// Unlike decoding through a runtime.Codec, Load does not require any scheme
// setup and reports problems precisely:
//   - empty input is reported as ErrEmptyConfig;
//   - input with only comments or empty documents is reported as
//     ErrNoConfigContent;
//   - malformed YAML/JSON is reported as ErrInvalidSyntax;
//   - a wrong apiVersion or kind is reported as *KindMismatchError,
//     describing both the expected and the actual values;
//   - with LoadOptions.Strict, unknown or duplicate fields are reported as
//     *StrictDecodingError with dotted field paths.
func Load(data []byte, opts LoadOptions) (*KubeProxyConfiguration, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrEmptyConfig
	}

	jsonData, err := toJSON(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSyntax, err)
	}
	if isNullDocument(jsonData) {
		return nil, ErrNoConfigContent
	}

	var typeMeta metav1.TypeMeta
	if err := k8sjson.UnmarshalCaseSensitivePreserveInts(jsonData, &typeMeta); err != nil {
		return nil, fmt.Errorf("failed to decode apiVersion/kind: %w", err)
	}
	if expected := SchemeGroupVersion.String(); typeMeta.APIVersion != expected || typeMeta.Kind != Kind {
		return nil, &KindMismatchError{
			ExpectedAPIVersion: expected,
			ExpectedKind:       Kind,
			ActualAPIVersion:   typeMeta.APIVersion,
			ActualKind:         typeMeta.Kind,
		}
	}

	config := &KubeProxyConfiguration{}
	if opts.Strict {
		strictErrs, err := k8sjson.UnmarshalStrict(jsonData, config)
		if err != nil {
			return nil, fmt.Errorf("failed to decode configuration: %w", err)
		}
		if len(strictErrs) > 0 {
			return nil, newStrictDecodingError(strictErrs)
		}
		return config, nil
	}

	if err := k8sjson.UnmarshalCaseSensitivePreserveInts(jsonData, config); err != nil {
		return nil, fmt.Errorf("failed to decode configuration: %w", err)
	}
	return config, nil
}

// LoadFile reads the file at path and decodes it like Load. Errors returned
// by Load are wrapped with the file path and remain matchable with
// errors.Is/errors.As.
func LoadFile(path string, opts LoadOptions) (*KubeProxyConfiguration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file %q: %w", path, err)
	}
	config, err := Load(data, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration from %q: %w", path, err)
	}
	return config, nil
}

// toJSON converts a YAML or JSON document to JSON. JSON input is passed
// through unchanged so that strict mode can detect duplicate fields, which
// a YAML round-trip would silently collapse.
func toJSON(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var syntaxCheck interface{}
		if err := k8sjson.UnmarshalCaseSensitivePreserveInts(trimmed, &syntaxCheck); err != nil {
			return nil, err
		}
		return trimmed, nil
	}
	// YAML is a superset of JSON, so this conversion also handles JSON
	// documents that do not start with a '{'.
	return yaml.YAMLToJSON(data)
}

// isNullDocument reports whether a JSON document represents the absence of
// content, e.g. the result of converting a comments-only YAML file.
func isNullDocument(jsonData []byte) bool {
	return bytes.Equal(bytes.TrimSpace(jsonData), []byte("null"))
}

func newStrictDecodingError(strictErrs []error) *StrictDecodingError {
	result := &StrictDecodingError{}
	for _, err := range strictErrs {
		fieldErr, ok := err.(k8sjson.FieldError)
		if !ok {
			continue
		}
		if strings.HasPrefix(err.Error(), "duplicate field") {
			result.DuplicateFields = append(result.DuplicateFields, fieldErr.FieldPath())
		} else {
			result.UnknownFields = append(result.UnknownFields, fieldErr.FieldPath())
		}
	}
	return result
}
