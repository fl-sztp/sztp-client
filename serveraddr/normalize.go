// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package serveraddr

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

var hostnameIDNA = idna.New(
	idna.MapForLookup(),
	idna.StrictDomainName(false), // Preserve underscore names used by local services.
	idna.CheckHyphens(false),     // Internal consecutive hyphens are valid hostnames.
	idna.BidiRule(),
)

// Normalize converts a bootstrap server address to an HTTPS origin, preserving
// an explicit port and removing userinfo, path, query, and fragment.
// Unicode hostnames are returned in their validated ASCII IDNA form.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("bootstrap server address is empty")
	}

	// A scheme delimiter belongs to the leading address only when it precedes
	// the path, query, and fragment; URLs inside those components are discarded.
	schemeEnd := strings.Index(raw, "://")
	componentStart := strings.IndexAny(raw, "/?#")
	explicitURL := strings.HasPrefix(raw, "//") || (schemeEnd >= 0 && schemeEnd < componentStart)
	input := raw
	if !explicitURL {
		// Bare IPv6 authorities use literal zone delimiters; URL authorities
		// require those delimiters to be escaped.
		if strings.HasPrefix(input, "[") {
			if end := strings.IndexByte(input, ']'); end >= 0 {
				input = strings.ReplaceAll(input[:end], "%", "%25") + input[end:]
			}
		}
		input = "//" + input
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("invalid bootstrap server address: %w", err)
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported bootstrap server scheme %q", u.Scheme)
	}
	if !explicitURL && u.User != nil {
		return "", fmt.Errorf("bare bootstrap server address must not contain userinfo")
	}
	if u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("bootstrap server host is empty")
	}

	host := u.Host
	port := ""
	hasPort := false
	if strings.HasPrefix(host, "[") {
		end := strings.IndexByte(host, ']')
		if end < 0 {
			return "", fmt.Errorf("bootstrap server IPv6 host is missing its closing bracket")
		}
		literal := host[1:end]
		addr, err := netip.ParseAddr(literal)
		if err != nil || !addr.Is6() || strings.HasSuffix(literal, "%") {
			return "", fmt.Errorf("invalid bootstrap server IPv6 host %q", literal)
		}
		if suffix := host[end+1:]; suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return "", fmt.Errorf("invalid bootstrap server authority %q", host)
			}
			port, hasPort = suffix[1:], true
		}
	} else {
		if strings.ContainsAny(host, "[]") || strings.Count(host, ":") > 1 {
			return "", fmt.Errorf("bootstrap server IPv6 host must be enclosed in brackets")
		}
		if colon := strings.IndexByte(host, ':'); colon >= 0 {
			port, hasPort = host[colon+1:], true
		}
		hostname, err := normalizeHostname(u.Hostname())
		if err != nil {
			return "", fmt.Errorf("invalid bootstrap server hostname %q: %w", u.Hostname(), err)
		}
		host = hostname
		if hasPort {
			host += ":" + port
		}
	}
	if hasPort {
		if port == "" {
			return "", fmt.Errorf("bootstrap server port is empty")
		}
		for _, digit := range port {
			if digit < '0' || digit > '9' {
				return "", fmt.Errorf("bootstrap server port %q is not numeric", port)
			}
		}
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return "", fmt.Errorf("bootstrap server port %q is outside 1..65535", port)
		}
	}

	return (&url.URL{Scheme: "https", Host: host}).String(), nil
}

// normalizeHostname validates lookup syntax without resolving DNS and returns
// an ASCII hostname. Existing ASCII spelling and the root dot are preserved.
func normalizeHostname(hostname string) (string, error) {
	unicodeHostname := strings.ContainsFunc(hostname, func(r rune) bool { return r > 127 })
	if unicodeHostname {
		var err error
		// Validate and map Unicode before checking label edges: punycode encoding
		// would hide a leading or trailing hyphen in the original Unicode label.
		hostname, err = hostnameIDNA.ToUnicode(hostname)
		if err != nil {
			return "", err
		}
	}
	rootDot := strings.HasSuffix(hostname, ".")
	labels := strings.Split(strings.TrimSuffix(hostname, "."), ".")
	length := len(labels) - 1 // Separating dots, excluding the optional root dot.
	for i, label := range labels {
		if label == "" {
			return "", fmt.Errorf("hostname contains an empty label")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("hostname label must not begin or end with a hyphen")
		}
		if strings.ContainsFunc(label, func(r rune) bool { return r > 127 }) {
			var err error
			label, err = idna.Punycode.ToASCII(label)
			if err != nil {
				return "", err
			}
		}
		if len(label) > 63 {
			return "", fmt.Errorf("hostname label exceeds 63 bytes")
		}
		for _, char := range label {
			if !('a' <= char && char <= 'z' || 'A' <= char && char <= 'Z' || '0' <= char && char <= '9' || char == '-' || char == '_') {
				return "", fmt.Errorf("hostname label contains invalid character %q", char)
			}
		}
		length += len(label)
		labels[i] = label
	}
	if length > 253 {
		return "", fmt.Errorf("hostname exceeds 253 bytes")
	}
	if unicodeHostname {
		hostname = strings.Join(labels, ".")
		if rootDot {
			hostname += "."
		}
	}
	return hostname, nil
}
