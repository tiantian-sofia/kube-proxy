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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const header = "apiVersion: kubeproxy.config.k8s.io/v1alpha1\nkind: KubeProxyConfiguration\n"

func TestLoadFromBytes_YAML(t *testing.T) {
	data := []byte(header + `
clusterCIDR: 10.0.0.0/24
mode: iptables
iptables:
  syncPeriod: 5s
  masqueradeBit: 14
`)
	cfg, err := LoadFromBytes(data, LoadOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClusterCIDR != "10.0.0.0/24" {
		t.Errorf("clusterCIDR = %q, want 10.0.0.0/24", cfg.ClusterCIDR)
	}
	if cfg.Mode != "iptables" {
		t.Errorf("mode = %q, want iptables", cfg.Mode)
	}
	if cfg.IPTables.SyncPeriod.Duration != 5*time.Second {
		t.Errorf("iptables.syncPeriod = %v, want 5s", cfg.IPTables.SyncPeriod.Duration)
	}
	if cfg.IPTables.MasqueradeBit == nil || *cfg.IPTables.MasqueradeBit != 14 {
		t.Errorf("iptables.masqueradeBit = %v, want 14", cfg.IPTables.MasqueradeBit)
	}
	// Decoding must populate TypeMeta on the returned object.
	if cfg.APIVersion != "kubeproxy.config.k8s.io/v1alpha1" || cfg.Kind != "KubeProxyConfiguration" {
		t.Errorf("TypeMeta = %+v, want kubeproxy.config.k8s.io/v1alpha1/KubeProxyConfiguration", cfg.TypeMeta)
	}
}

func TestLoadFromBytes_JSON(t *testing.T) {
	data := []byte(`{
  "apiVersion": "kubeproxy.config.k8s.io/v1alpha1",
  "kind": "KubeProxyConfiguration",
  "clusterCIDR": "fd00::/64",
  "bindAddress": "::"
}`)
	cfg, err := LoadFromBytes(data, LoadOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClusterCIDR != "fd00::/64" || cfg.BindAddress != "::" {
		t.Errorf("got clusterCIDR=%q bindAddress=%q, want fd00::/64 and ::", cfg.ClusterCIDR, cfg.BindAddress)
	}
}

func TestLoadFromBytes_EmptyAndCommentOnly(t *testing.T) {
	cases := []struct {
		name     string
		data     string
		wantErr  error
		wantText string
	}{
		{"empty", "", ErrEmptyConfiguration, "empty"},
		{"whitespace only", "\n\t  \n   \n", ErrEmptyConfiguration, "empty"},
		{"comments only", "# just a comment\n# another\n", ErrConfigurationCommentOnly, "only YAML comments"},
		{"comment with trailing newlines", "# header\n\n   \n", ErrConfigurationCommentOnly, "only YAML comments"},
		{"markers only", "---\n...\n", ErrEmptyConfiguration, "document markers"},
		{"explicit null", "null\n", ErrEmptyConfiguration, "null"},
		{"explicit tilde", "~\n", ErrEmptyConfiguration, "null"},
		{"null in YAML mapping position", header + "iptables: null\n", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromBytes([]byte(tc.data), LoadOptions{})
			if tc.wantErr == nil {
				// "iptables: null" is valid; it decodes to the zero-value nested struct.
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want errors.Is %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantText)
			}
		})
	}
}

func TestLoadFromBytes_SyntaxErrors(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"broken yaml", "apiVersion: [unclosed\n"},
		{"broken json", `{"apiVersion": "kubeproxy.config.k8s.io/v1alpha1", "kind":`},
		{"tab indentation", "apiVersion: x\n\tkind: y\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromBytes([]byte(tc.data), LoadOptions{})
			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("error = %v, want errors.Is ErrInvalidConfiguration", err)
			}
			if err == nil || !strings.Contains(err.Error(), "parse error") {
				t.Fatalf("error %q should mention a parse error", err)
			}
		})
	}
}

func TestLoadFromBytes_NonMappingRoot(t *testing.T) {
	cases := []struct {
		name     string
		data     string
		wantText string
	}{
		{"scalar root", "just-a-string\n", "mapping"},
		{"array root", "- a\n- b\n", "mapping"},
		{"number root", "42\n", "mapping"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromBytes([]byte(tc.data), LoadOptions{})
			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("error = %v, want errors.Is ErrInvalidConfiguration", err)
			}
			if !strings.Contains(err.Error(), "root must be a YAML/JSON mapping") {
				t.Fatalf("error %q should mention non-mapping root", err.Error())
			}
		})
	}
}

func TestLoadFromBytes_TypeMetaMismatch(t *testing.T) {
	cases := []struct {
		name        string
		data        string
		wantAPIVers string
		wantKind    string
	}{
		{
			name:        "missing both",
			data:        "clusterCIDR: 10.0.0.0/24\n",
			wantAPIVers: "",
			wantKind:    "",
		},
		{
			name:        "wrong group",
			data:        "apiVersion: other.config.k8s.io/v1alpha1\nkind: KubeProxyConfiguration\nclusterCIDR: x\n",
			wantAPIVers: "other.config.k8s.io/v1alpha1",
			wantKind:    "",
		},
		{
			name:        "wrong version",
			data:        "apiVersion: kubeproxy.config.k8s.io/v1beta1\nkind: KubeProxyConfiguration\nclusterCIDR: x\n",
			wantAPIVers: "kubeproxy.config.k8s.io/v1beta1",
			wantKind:    "",
		},
		{
			name:        "wrong kind",
			data:        "apiVersion: kubeproxy.config.k8s.io/v1alpha1\nkind: KubeletConfiguration\nclusterCIDR: x\n",
			wantAPIVers: "",
			wantKind:    "KubeletConfiguration",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromBytes([]byte(tc.data), LoadOptions{})
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if tc.wantAPIVers != "" || strings.Contains(tc.name, "missing") {
				var mismatch *APIVersionMismatchError
				if !errors.As(err, &mismatch) {
					t.Fatalf("error %v is not an APIVersionMismatchError", err)
				}
				if mismatch.Expected != "kubeproxy.config.k8s.io/v1alpha1" || mismatch.Actual != tc.wantAPIVers {
					t.Fatalf("apiVersion mismatch = expected %q actual %q, want expected %q actual %q",
						mismatch.Expected, mismatch.Actual, "kubeproxy.config.k8s.io/v1alpha1", tc.wantAPIVers)
				}
				if !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "got") {
					t.Fatalf("error %q should state expected and actual apiVersion", err.Error())
				}
			}
			if tc.wantKind != "" || strings.Contains(tc.name, "missing") {
				var mismatch *KindMismatchError
				if !errors.As(err, &mismatch) {
					t.Fatalf("error %v is not a KindMismatchError", err)
				}
				if mismatch.Expected != "KubeProxyConfiguration" || mismatch.Actual != tc.wantKind {
					t.Fatalf("kind mismatch = expected %q actual %q, want expected %q actual %q",
						mismatch.Expected, mismatch.Actual, "KubeProxyConfiguration", tc.wantKind)
				}
			}
		})
	}
}

func TestLoadFromBytes_StrictUnknownFields(t *testing.T) {
	t.Run("nested unknown field reports dotted path", func(t *testing.T) {
		data := []byte(header + `
iptables:
  syncPeriod: 5s
  bogusField: 42
`)
		_, err := LoadFromBytes(data, LoadOptions{Strict: true})
		if !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("error = %v, want errors.Is ErrInvalidConfiguration", err)
		}
		if !strings.Contains(err.Error(), `unknown field "iptables.bogusField"`) {
			t.Fatalf("error %q should contain unknown field path iptables.bogusField", err.Error())
		}
	})

	t.Run("top level unknown field", func(t *testing.T) {
		_, err := LoadFromBytes([]byte(header+"bogusField: true\n"), LoadOptions{Strict: true})
		if !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("error = %v, want errors.Is ErrInvalidConfiguration", err)
		}
		if !strings.Contains(err.Error(), `unknown field "bogusField"`) {
			t.Fatalf("error %q should contain unknown field path bogusField", err.Error())
		}
	})

	t.Run("case mismatched field is unknown in strict mode", func(t *testing.T) {
		_, err := LoadFromBytes([]byte(header+"clusterCidr: 10.0.0.0/24\n"), LoadOptions{Strict: true})
		if !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("error = %v, want errors.Is ErrInvalidConfiguration", err)
		}
		if !strings.Contains(err.Error(), `unknown field "clusterCidr"`) {
			t.Fatalf("error %q should contain unknown field path clusterCidr", err.Error())
		}
	})

	t.Run("duplicate fields are rejected", func(t *testing.T) {
		data := []byte(header + "clusterCIDR: 10.0.0.0/24\nclusterCIDR: 10.1.0.0/24\n")
		_, err := LoadFromBytes(data, LoadOptions{Strict: true})
		if !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("error = %v, want errors.Is ErrInvalidConfiguration", err)
		}
		if !strings.Contains(err.Error(), "already set in map") || !strings.Contains(err.Error(), "clusterCIDR") {
			t.Fatalf("error %q should mention duplicate key clusterCIDR", err.Error())
		}
	})
}

func TestLoadFromBytes_LenientIgnoresUnknownFields(t *testing.T) {
	for _, input := range []string{
		header + "bogusField: 1\nclusterCIDR: 10.0.0.0/24\n",
		header + "clusterCidr: 10.0.0.0/24\n",
	} {
		cfg, err := LoadFromBytes([]byte(input), LoadOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(input, "bogusField") && cfg.ClusterCIDR != "10.0.0.0/24" {
			t.Fatalf("clusterCIDR = %q, want 10.0.0.0/24", cfg.ClusterCIDR)
		}
	}
}

func TestLoadFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.conf")
	data := []byte(header + "clusterCIDR: 10.0.0.0/24\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := LoadFromFile(path, LoadOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClusterCIDR != "10.0.0.0/24" {
		t.Errorf("clusterCIDR = %q, want 10.0.0.0/24", cfg.ClusterCIDR)
	}

	if _, err := LoadFromFile(filepath.Join(t.TempDir(), "missing.conf"), LoadOptions{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v, want os.ErrNotExist", err)
	}

	if err := os.WriteFile(path, []byte("# nothing here\n"), 0o600); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}
	if _, err := LoadFromFile(path, LoadOptions{}); !errors.Is(err, ErrConfigurationCommentOnly) {
		t.Fatalf("comment-only file error = %v, want ErrConfigurationCommentOnly", err)
	}
}
