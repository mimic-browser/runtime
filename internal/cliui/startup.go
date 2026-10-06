package cliui

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

const logoBlue = `  __  __               __  __               ____
U|' \/ '|u   ___     U|' \/ '|u   ___    U /"___|
\| |\/| |/  |_"_|    \| |\/| |/  |_"_|   \| | u`

const logoGreen = ` | |  | |    | |      | |  | |    | |     | |/__
 |_|  |_|  U/| |\u    |_|  |_|  U/| |\u    \____|
<<,-,,-..-,_|___|_,-.<<,-,,-..-,_|___|_,-._// \\
 (./  \.)\_)-' '-(_/  (./  \.)\_)-' '-(_/(__)(__)`

const (
	reset = "\x1b[0m"
	blue  = "\x1b[1;94m"
	dim   = "\x1b[2;37m"
	green = "\x1b[1;32m"
	white = "\x1b[1;37m"
)

type StartupInfo struct {
	Version string
	Chrome  int
	Engine  string
	Address string
}

// WriteOptimizeHeader shares the normal runtime's identity without emitting a
// large decorative banner into redirected output or CI logs.
func WriteOptimizeHeader(w io.Writer, styled bool) error {
	if styled {
		_, err := fmt.Fprintf(w, "\n%s%s\n%s%s\n\n  %sMimic Optimize%s  %s%s%s\n  %sLearn which network traffic your workload needs.%s\n\n", blue, logoBlue, logoGreen, reset, white, reset, dim, BuildVersion(), reset, dim, reset)
		return err
	}
	_, err := fmt.Fprintf(w, "Mimic Optimize · %s\nLearn which network traffic your workload needs.\n\n", BuildVersion())
	return err
}

// BuildVersion returns the module version for releases and a short VCS
// revision for development builds.
func BuildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return buildVersionFromInfo(info)
}

func buildVersionFromInfo(info *debug.BuildInfo) string {
	if info.Main.Version != "" && info.Main.Version != "(devel)" && pseudoRevision(info.Main.Version) == "" {
		return info.Main.Version
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	revision := settings["vcs.revision"]
	if len(revision) > 8 {
		revision = revision[:8]
	}
	if revision == "" {
		revision = pseudoRevision(info.Main.Version)
		if len(revision) > 8 {
			revision = revision[:8]
		}
	}
	if revision == "" {
		return "dev"
	}
	version := "dev+" + revision
	if settings["vcs.modified"] == "true" {
		version += "-dirty"
	}
	return version
}

func pseudoRevision(version string) string {
	version = strings.TrimSuffix(version, "+incompatible")
	parts := strings.Split(version, "-")
	if len(parts) < 3 {
		return ""
	}
	timestamp, revision := parts[len(parts)-2], parts[len(parts)-1]
	if len(timestamp) != 14 || len(revision) < 12 {
		return ""
	}
	for _, char := range timestamp {
		if char < '0' || char > '9' {
			return ""
		}
	}
	for _, char := range revision {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return revision
}

// WriteStartup renders the human-facing startup banner. When styled is false,
// it emits no terminal control sequences.
func WriteStartup(w io.Writer, info StartupInfo, styled bool) error {
	metadata := fmt.Sprintf("Mimic %s  ·  Chrome %d  ·  %s", info.Version, info.Chrome, strings.ToUpper(info.Engine))
	if styled {
		_, err := fmt.Fprintf(w, "\n%s%s\n%s%s\n\n  %s%s%s\n%s  browser runtime · without Chromium%s\n\n  %s◆ READY%s  %sCDP · %s%s\n\n", blue, logoBlue, logoGreen, reset, white, metadata, reset, dim, reset, green, reset, white, info.Address, reset)
		return err
	}
	_, err := fmt.Fprintf(w, "\n%s\n%s\n\n  %s\n  browser runtime · without Chromium\n\n  ◆ READY  CDP · %s\n\n", logoBlue, logoGreen, metadata, info.Address)
	return err
}

// IsInteractive reports whether output is attached directly to a terminal.
func IsInteractive(output *os.File) bool {
	info, err := output.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
