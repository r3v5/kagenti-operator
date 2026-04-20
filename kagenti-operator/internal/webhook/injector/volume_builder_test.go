/*
Copyright 2025.

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

package injector

import (
	"testing"
)

func TestBuildResolvedVolumes_SpireDisabled(t *testing.T) {
	volumes := BuildResolvedVolumes(false, "", "")

	// Should have: shared-data, envoy-config, authproxy-routes, authbridge-runtime-config
	if len(volumes) != 4 {
		t.Fatalf("expected 4 volumes, got %d", len(volumes))
	}

	names := map[string]bool{}
	for _, v := range volumes {
		names[v.Name] = true
	}

	for _, expected := range []string{"shared-data", "envoy-config", "authproxy-routes", "authbridge-runtime-config"} {
		if !names[expected] {
			t.Errorf("missing volume %q", expected)
		}
	}

	// Should NOT have SPIRE volumes
	for _, absent := range []string{"spire-agent-socket", "spiffe-helper-config", "svid-output"} {
		if names[absent] {
			t.Errorf("unexpected SPIRE volume %q when spireEnabled=false", absent)
		}
	}
}

func TestBuildResolvedVolumes_SpireEnabled(t *testing.T) {
	volumes := BuildResolvedVolumes(true, "", "")

	// Should have: shared-data, spire-agent-socket, spiffe-helper-config, svid-output, envoy-config, authproxy-routes, authbridge-runtime-config
	if len(volumes) != 7 {
		t.Fatalf("expected 7 volumes, got %d", len(volumes))
	}

	names := map[string]bool{}
	for _, v := range volumes {
		names[v.Name] = true
	}

	for _, expected := range []string{"shared-data", "spire-agent-socket", "spiffe-helper-config", "svid-output", "envoy-config", "authproxy-routes", "authbridge-runtime-config"} {
		if !names[expected] {
			t.Errorf("missing volume %q", expected)
		}
	}
}

func TestBuildResolvedVolumes_CustomEnvoyConfigMapName(t *testing.T) {
	volumes := BuildResolvedVolumes(false, "my-custom-envoy", "")

	var envoyVolume *string
	for _, v := range volumes {
		if v.Name == "envoy-config" {
			name := v.VolumeSource.ConfigMap.LocalObjectReference.Name
			envoyVolume = &name
		}
	}

	if envoyVolume == nil {
		t.Fatal("envoy-config volume not found")
	}
	if *envoyVolume != "my-custom-envoy" {
		t.Errorf("envoy-config ConfigMap name = %q, want %q", *envoyVolume, "my-custom-envoy")
	}
}

func TestBuildResolvedVolumes_DefaultEnvoyConfigMapName(t *testing.T) {
	volumes := BuildResolvedVolumes(false, "", "")

	for _, v := range volumes {
		if v.Name == "envoy-config" {
			name := v.VolumeSource.ConfigMap.LocalObjectReference.Name
			if name != EnvoyConfigMapName {
				t.Errorf("envoy-config ConfigMap name = %q, want %q", name, EnvoyConfigMapName)
			}
			return
		}
	}
	t.Fatal("envoy-config volume not found")
}

func TestBuildResolvedVolumes_CustomAuthBridgeConfigMapName(t *testing.T) {
	volumes := BuildResolvedVolumes(false, "", "authbridge-config-weather-service")

	for _, v := range volumes {
		if v.Name == AuthBridgeRuntimeConfigMapName {
			name := v.ConfigMap.Name
			if name != "authbridge-config-weather-service" {
				t.Errorf("authbridge-runtime-config ConfigMap name = %q, want %q", name, "authbridge-config-weather-service")
			}
			return
		}
	}
	t.Fatal("authbridge-runtime-config volume not found")
}

func TestOverrideAuthBridgeConfigMapInVolumes(t *testing.T) {
	original := BuildRequiredVolumes()
	overridden := overrideAuthBridgeConfigMapInVolumes(original, "authbridge-config-my-agent")

	// Original should be unchanged
	for _, v := range original {
		if v.Name == AuthBridgeRuntimeConfigMapName && v.ConfigMap != nil {
			if v.ConfigMap.Name != AuthBridgeRuntimeConfigMapName {
				t.Errorf("original was mutated: got %q", v.ConfigMap.Name)
			}
		}
	}

	// Overridden should have the new name
	for _, v := range overridden {
		if v.Name == AuthBridgeRuntimeConfigMapName && v.ConfigMap != nil {
			if v.ConfigMap.Name != "authbridge-config-my-agent" {
				t.Errorf("override failed: got %q, want %q",
					v.ConfigMap.Name, "authbridge-config-my-agent")
			}
			return
		}
	}
	t.Fatal("authbridge-runtime-config volume not found in overridden volumes")
}
