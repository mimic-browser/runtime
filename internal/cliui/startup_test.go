package cliui

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestOptimizeHeaderHasPlainAndStyledPresentations(t *testing.T) {
	for _, styled := range []bool{false, true} {
		var output bytes.Buffer
		if err := WriteOptimizeHeader(&output, styled); err != nil {
			t.Fatal(err)
		}
		text := output.String()
		if !strings.Contains(text, "Mimic Optimize") || !strings.Contains(text, "network traffic") || strings.Contains(text, "\x1b[") != styled {
			t.Fatalf("invalid header presentation: %q", text)
		}
	}
}

func TestWriteStartupPlain(t *testing.T) {
	var output bytes.Buffer
	info := StartupInfo{Version: "v0.1.4", Chrome: 152, Engine: "v8", Address: "127.0.0.1:9222"}
	if err := WriteStartup(&output, info, false); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{"U|' \\/ '|u", "<<,-,,-..-,_|___|_,-.", "Mimic v0.1.4  ·  Chrome 152  ·  V8", "browser runtime · without Chromium", "◆ READY  CDP · 127.0.0.1:9222"} {
		if !strings.Contains(text, want) {
			t.Fatalf("startup banner is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("plain startup banner contains terminal styling: %q", text)
	}
}

func TestWriteStartupStyled(t *testing.T) {
	var output bytes.Buffer
	info := StartupInfo{Version: "dev", Chrome: 152, Engine: "goja", Address: "127.0.0.1:9222"}
	if err := WriteStartup(&output, info, true); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "\x1b[1;94m") || !strings.Contains(text, "\x1b[1;32m") || !strings.Contains(text, "\x1b[1;32m◆ READY") {
		t.Fatalf("styled startup banner is missing expected colors: %q", text)
	}
	if strings.Contains(text, "\x1b[1;32m"+logoGreen) {
		t.Fatalf("logo must use one blue color: %q", text)
	}
}

func TestBuildVersion(t *testing.T) {
	if version := BuildVersion(); version == "" {
		t.Fatal("empty build version")
	}
}

func TestBuildVersionFromGoBuildInfo(t *testing.T) {
	tests := []struct {
		name string
		info debug.BuildInfo
		want string
	}{
		{name: "release", info: debug.BuildInfo{Main: debug.Module{Version: "v0.1.4"}}, want: "v0.1.4"},
		{name: "pseudo", info: debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260920165425-17f508b820af"}}, want: "dev+17f508b8"},
		{name: "local", info: debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "17f508b820af1234"}}}, want: "dev+17f508b8"},
		{name: "dirty", info: debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "17f508b820af1234"}, {Key: "vcs.modified", Value: "true"}}}, want: "dev+17f508b8-dirty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVersionFromInfo(&test.info); got != test.want {
				t.Fatalf("buildVersionFromInfo() = %q, want %q", got, test.want)
			}
		})
	}
}
