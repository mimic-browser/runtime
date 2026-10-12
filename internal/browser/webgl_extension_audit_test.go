//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestWebGLExtensionOperationsFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/webgl_extension_audit_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/webgl_extension_audit_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Result struct {
			Result struct {
				Value map[string]any `json:"value"`
			} `json:"result"`
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	// This exact implemented inventory is intentionally smaller than native.
	// Every advertised extension must pass its native operation; removing an
	// advertised extension cannot silently evade this regression.
	supported := map[string][]string{
		"webgl":  {"ANGLE_instanced_arrays", "EXT_clip_control", "EXT_color_buffer_half_float", "EXT_depth_clamp", "EXT_disjoint_timer_query", "EXT_polygon_offset_clamp", "EXT_texture_compression_bptc", "EXT_texture_compression_rgtc", "EXT_texture_filter_anisotropic", "KHR_parallel_shader_compile", "OES_element_index_uint", "OES_standard_derivatives", "OES_texture_float", "OES_texture_float_linear", "OES_texture_half_float", "OES_texture_half_float_linear", "OES_vertex_array_object", "WEBGL_color_buffer_float", "WEBGL_compressed_texture_s3tc", "WEBGL_compressed_texture_s3tc_srgb", "WEBGL_debug_renderer_info", "WEBGL_debug_shaders", "WEBGL_lose_context"},
		"webgl2": {"EXT_clip_control", "EXT_color_buffer_float", "EXT_color_buffer_half_float", "EXT_depth_clamp", "EXT_disjoint_timer_query_webgl2", "EXT_polygon_offset_clamp", "EXT_texture_compression_bptc", "EXT_texture_compression_rgtc", "EXT_texture_filter_anisotropic", "KHR_parallel_shader_compile", "OES_texture_float_linear", "WEBGL_compressed_texture_s3tc", "WEBGL_compressed_texture_s3tc_srgb", "WEBGL_debug_renderer_info", "WEBGL_debug_shaders", "WEBGL_lose_context"},
	}
	want := map[string]any{}
	for kind, names := range supported {
		native := capture.Result.Result.Value[kind].(map[string]any)
		operations := native["extensions"].(map[string]any)
		selected := map[string]any{}
		for _, name := range names {
			if _, ok := operations[name]; !ok {
				t.Fatalf("%s missing native oracle", name)
			}
			selected[name] = operations[name]
		}
		inventory := make([]any, len(names))
		for i, name := range names {
			inventory[i] = name
		}
		want[kind] = map[string]any{"names": inventory, "extensions": selected}
	}
	for _, restored := range []bool{false, true} {
		t.Run(strconv.FormatBool(restored), func(t *testing.T) {
			t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", map[bool]string{false: "1", true: "0"}[restored])
			seed := bootstrapSnapshotPage(t)
			navigateCapabilityFixture(t, seed)
			if restored {
				bootstrapSnapshotWarm(t, seed)
			}
			p, err := seed.ctx.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			navigateCapabilityFixture(t, p)
			value, err := p.Evaluate(context.Background(), string(source))
			if err != nil {
				t.Fatal(err)
			}
			if d := bootstrapSnapshotDifference("WebGL extension operation", want, value); d != "" {
				t.Fatal(d)
			}
		})
	}
}
