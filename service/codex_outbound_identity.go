package service

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	CodexDefaultOriginator   = "codex-tui"
	CodexFallbackVersion     = "0.146.0"
	codexMinimumVersion      = "0.144.0"
	codexVersionMaximumBytes = 64
)

var codexVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}(-[0-9A-Za-z.]+)?$`)

type CodexOutboundIdentity struct {
	UserAgent  string
	Originator string
	Version    string
}

func NormalizeCodexClientVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > codexVersionMaximumBytes || !codexVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

func ResolveCodexClientVersion() string {
	settings := system_setting.GetCodexSetting()
	for _, candidate := range []string{settings.ClientVersion, settings.SyncedClientVersion, CodexFallbackVersion} {
		version := NormalizeCodexClientVersion(candidate)
		if version != "" && compareCodexVersions(version, codexMinimumVersion) >= 0 {
			return version
		}
	}
	return CodexFallbackVersion
}

func ResolveCodexOutboundIdentity() CodexOutboundIdentity {
	version := ResolveCodexClientVersion()
	return CodexOutboundIdentity{
		UserAgent:  CodexDefaultOriginator + "/" + version + " (Ubuntu 22.4.0; x86_64) xterm-256color",
		Originator: CodexDefaultOriginator,
		Version:    version,
	}
}

// ApplyCodexOutboundIdentity is the final OAuth identity convergence point.
// It must run after client passthrough and header overrides.
func ApplyCodexOutboundIdentity(header http.Header) {
	if header == nil {
		return
	}
	identity := ResolveCodexOutboundIdentity()
	settings := system_setting.GetCodexSetting()
	if settings.DisableIdentityEnforcement {
		if strings.TrimSpace(header.Get("User-Agent")) == "" {
			header.Set("User-Agent", identity.UserAgent)
		}
		if strings.TrimSpace(header.Get("originator")) == "" {
			header.Set("originator", identity.Originator)
		}
		version := NormalizeCodexClientVersion(header.Get("version"))
		if version == "" || compareCodexVersions(version, codexMinimumVersion) < 0 {
			header.Set("version", identity.Version)
		}
		return
	}
	header.Set("User-Agent", identity.UserAgent)
	header.Set("originator", identity.Originator)
	header.Set("version", identity.Version)
}

func compareCodexVersions(left string, right string) int {
	parse := func(value string) []int {
		value = strings.SplitN(strings.TrimSpace(value), "-", 2)[0]
		parts := strings.Split(value, ".")
		result := make([]int, len(parts))
		for index, part := range parts {
			result[index], _ = strconv.Atoi(part)
		}
		return result
	}
	lhs, rhs := parse(left), parse(right)
	length := len(lhs)
	if len(rhs) > length {
		length = len(rhs)
	}
	for index := 0; index < length; index++ {
		var l, r int
		if index < len(lhs) {
			l = lhs[index]
		}
		if index < len(rhs) {
			r = rhs[index]
		}
		if l < r {
			return -1
		}
		if l > r {
			return 1
		}
	}
	return 0
}
