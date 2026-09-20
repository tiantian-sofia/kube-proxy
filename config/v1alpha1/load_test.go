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
	"reflect"
	"strings"
	"testing"
	"time"
)

const validYAML = `
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
mode: iptables
clusterCIDR: 10.244.0.0/16
hostnameOverride: node-1
iptables:
  masqueradeAll: true
  syncPeriod: 5s
conntrack:
  tcpBeLiberal: true
`

const validJSON = `{
  "apiVersion": "kubeproxy.config.k8s.io/v1alpha1",
  "kind": "KubeProxyConfiguration",
  "mode": "ipvs",
  "clusterCIDR": "10.244.0.0/16",
  "ipvs": {
    "scheduler": "rr",
    "syncPeriod": "30s"
  }
}`

func TestLoadYAML(t *testing.T) {
	config, err := Load([]byte(validYAML), LoadOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config.APIVersion != "kubeproxy.config.k8s.io/v1alpha1" {
		t.Errorf("expected apiVersion to be preserved, got %q", config.APIVersion)
	}
	if config.Kind != "KubeProxyConfiguration" {
		t.Errorf("expected kind to be preserved, got %q", config.Kind)
	}
	if config.Mode != "iptables" {
		t.Errorf("expected mode iptables, got %q", config.Mode)
	}
	if config.ClusterCIDR != "10.244.0.0/16" {
		t.Errorf("expected clusterCIDR 10.244.0.0/16, got %q", config.ClusterCIDR)
	}
	if !config.IPTables.MasqueradeAll {
		t.Errorf("expected iptables.masqueradeAll to be true")
	}
	if config.IPTables.SyncPeriod.Duration != 5*time.Second {
		t.Errorf("expected iptables.syncPeriod 5s, got %v", config.IPTables.SyncPeriod.Duration)
	}
	if !config.Conntrack.TCPBeLiberal {
		t.Errorf("expected conntrack.tcpBeLiberal to be true")
	}
}

func TestLoadJSON(t *testing.T) {
	config, err := Load([]byte(validJSON), LoadOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config.Mode != "ipvs" {
		t.Errorf("expected mode ipvs, got %q", config.Mode)
	}
	if config.IPVS.Scheduler != "rr" {
		t.Errorf("expected ipvs.scheduler rr, got %q", config.IPVS.Scheduler)
	}
	if config.IPVS.SyncPeriod.Duration != 30*time.Second {
		t.Errorf("expected ipvs.syncPeriod 30s, got %v", config.IPVS.SyncPeriod.Duration)
	}
}

func TestLoadEmpty(t *testing.T) {
	for name, data := range map[string]string{
		"zero bytes":      "",
		"whitespace only": "  \n\t\n  ",
	} {
		t.Run(name, func(t *testing.T) {
			config, err := Load([]byte(data), LoadOptions{})
			if !errors.Is(err, ErrEmptyConfig) {
				t.Fatalf("expected ErrEmptyConfig, got %v", err)
			}
			if config != nil {
				t.Errorf("expected nil config, got %+v", config)
			}
		})
	}
}

func TestLoadNoContent(t *testing.T) {
	for name, data := range map[string]string{
		"comments only":          "# just a comment\n# another one\n",
		"empty document marker":  "---\n",
		"comments and separator": "# header\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			config, err := Load([]byte(data), LoadOptions{})
			if !errors.Is(err, ErrNoConfigContent) {
				t.Fatalf("expected ErrNoConfigContent, got %v", err)
			}
			if config != nil {
				t.Errorf("expected nil config, got %+v", config)
			}
		})
	}
}

func TestLoadInvalidSyntax(t *testing.T) {
	for name, data := range map[string]string{
		"bad yaml indentation": "apiVersion: kubeproxy.config.k8s.io/v1alpha1\nkind: KubeProxyConfiguration\niptables:\n masqueradeAll: true\n  syncPeriod: 5s\n",
		"unbalanced json":      `{"apiVersion": "kubeproxy.config.k8s.io/v1alpha1", "kind": `,
		"scalar document":      "just a string\n",
	} {
		t.Run(name, func(t *testing.T) {
			config, err := Load([]byte(data), LoadOptions{})
			if err == nil {
				t.Fatalf("expected error, got config %+v", config)
			}
			if !errors.Is(err, ErrInvalidSyntax) && !strings.Contains(err.Error(), "failed to decode") {
				t.Fatalf("expected ErrInvalidSyntax or decode error, got %v", err)
			}
			if config != nil {
				t.Errorf("expected nil config, got %+v", config)
			}
		})
	}
}

func TestLoadKindMismatch(t *testing.T) {
	for name, tc := range map[string]struct {
		data             string
		actualAPIVersion string
		actualKind       string
	}{
		"wrong apiVersion": {
			data:             "apiVersion: kubeproxy.config.k8s.io/v1beta1\nkind: KubeProxyConfiguration\n",
			actualAPIVersion: "kubeproxy.config.k8s.io/v1beta1",
			actualKind:       "KubeProxyConfiguration",
		},
		"wrong kind": {
			data:             "apiVersion: kubeproxy.config.k8s.io/v1alpha1\nkind: KubeletConfiguration\n",
			actualAPIVersion: "kubeproxy.config.k8s.io/v1alpha1",
			actualKind:       "KubeletConfiguration",
		},
		"missing both": {
			data:             "mode: iptables\n",
			actualAPIVersion: "",
			actualKind:       "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			config, err := Load([]byte(tc.data), LoadOptions{})
			if config != nil {
				t.Errorf("expected nil config, got %+v", config)
			}
			var mismatch *KindMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("expected KindMismatchError, got %v", err)
			}
			if mismatch.ExpectedAPIVersion != "kubeproxy.config.k8s.io/v1alpha1" {
				t.Errorf("expected expected apiVersion kubeproxy.config.k8s.io/v1alpha1, got %q", mismatch.ExpectedAPIVersion)
			}
			if mismatch.ExpectedKind != "KubeProxyConfiguration" {
				t.Errorf("expected expected kind KubeProxyConfiguration, got %q", mismatch.ExpectedKind)
			}
			if mismatch.ActualAPIVersion != tc.actualAPIVersion {
				t.Errorf("expected actual apiVersion %q, got %q", tc.actualAPIVersion, mismatch.ActualAPIVersion)
			}
			if mismatch.ActualKind != tc.actualKind {
				t.Errorf("expected actual kind %q, got %q", tc.actualKind, mismatch.ActualKind)
			}
			if !strings.Contains(err.Error(), "expected apiVersion") {
				t.Errorf("expected error message to describe expectation, got %q", err.Error())
			}
		})
	}
}

func TestLoadUnknownFieldLenient(t *testing.T) {
	data := `
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
bogusField: hello
clusterCidr: 10.244.0.0/16
`
	config, err := Load([]byte(data), LoadOptions{})
	if err != nil {
		t.Fatalf("expected unknown fields to be tolerated without strict mode, got %v", err)
	}
	if config.ClusterCIDR != "" {
		t.Errorf("expected misspelled clusterCidr to be dropped, got %q", config.ClusterCIDR)
	}
}

func TestLoadUnknownFieldStrict(t *testing.T) {
	data := `
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
clusterCidr: 10.244.0.0/16
iptables:
  masqueradeAll: true
  bogusField: oops
`
	config, err := Load([]byte(data), LoadOptions{Strict: true})
	if config != nil {
		t.Errorf("expected nil config, got %+v", config)
	}
	var strictErr *StrictDecodingError
	if !errors.As(err, &strictErr) {
		t.Fatalf("expected StrictDecodingError, got %v", err)
	}
	expected := []string{"clusterCidr", "iptables.bogusField"}
	if !reflect.DeepEqual(strictErr.UnknownFields, expected) {
		t.Errorf("expected unknown fields %v, got %v", expected, strictErr.UnknownFields)
	}
	if !strings.Contains(err.Error(), `"iptables.bogusField"`) {
		t.Errorf("expected error message to contain field path, got %q", err.Error())
	}
}

func TestLoadDuplicateFieldStrict(t *testing.T) {
	data := `{
  "apiVersion": "kubeproxy.config.k8s.io/v1alpha1",
  "kind": "KubeProxyConfiguration",
  "mode": "iptables",
  "mode": "ipvs"
}`
	_, err := Load([]byte(data), LoadOptions{Strict: true})
	var strictErr *StrictDecodingError
	if !errors.As(err, &strictErr) {
		t.Fatalf("expected StrictDecodingError, got %v", err)
	}
	expected := []string{"mode"}
	if !reflect.DeepEqual(strictErr.DuplicateFields, expected) {
		t.Errorf("expected duplicate fields %v, got %v", expected, strictErr.DuplicateFields)
	}
}

func TestLoadStrictValidConfig(t *testing.T) {
	config, err := Load([]byte(validYAML), LoadOptions{Strict: true})
	if err != nil {
		t.Fatalf("expected valid config to pass strict decoding, got %v", err)
	}
	if config.Mode != "iptables" {
		t.Errorf("expected mode iptables, got %q", config.Mode)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("valid file", func(t *testing.T) {
		path := filepath.Join(dir, "config.conf")
		if err := os.WriteFile(path, []byte(validYAML), 0644); err != nil {
			t.Fatal(err)
		}
		config, err := LoadFile(path, LoadOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if config.Mode != "iptables" {
			t.Errorf("expected mode iptables, got %q", config.Mode)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadFile(filepath.Join(dir, "does-not-exist.conf"), LoadOptions{})
		if err == nil {
			t.Fatal("expected error for missing file")
		}
		if !strings.Contains(err.Error(), "does-not-exist.conf") {
			t.Errorf("expected error to mention file path, got %q", err.Error())
		}
	})

	t.Run("load errors stay matchable", func(t *testing.T) {
		path := filepath.Join(dir, "empty.conf")
		if err := os.WriteFile(path, []byte("# nothing here\n"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFile(path, LoadOptions{})
		if !errors.Is(err, ErrNoConfigContent) {
			t.Fatalf("expected ErrNoConfigContent through wrapping, got %v", err)
		}
	})
}
