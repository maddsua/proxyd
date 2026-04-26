package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

var Version = "(development)"

func cmd_version() {
	fmt.Fprintln(os.Stderr, "proxyd", Version, fmtBuildInfo())
}

func fmtBuildInfo() string {

	if info, ok := debug.ReadBuildInfo(); ok {
		return fmt.Sprintf("%s %s/%s, compiled with %s",
			info.GoVersion,
			lookupBuildSetting(info.Settings, "GOOS"),
			lookupBuildSetting(info.Settings, "GOARCH"),
			lookupBuildSetting(info.Settings, "-compiler"))
	}

	return "unknown build"
}

func lookupBuildSetting(entries []debug.BuildSetting, key string) string {
	for _, entry := range entries {
		if strings.EqualFold(entry.Key, key) {
			return entry.Value
		}
	}
	return "unknown"
}
