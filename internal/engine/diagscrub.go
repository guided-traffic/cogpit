package engine

import (
	"cmp"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The endpoints of Connection Diagnostics' report (D-133, the owner's choice of 2026-10-26:
// "Nur Endpunkte"): hosts, addresses, ports, URLs' servers and file paths are replaced before a
// report leaves Cogpit; the names of users, groups, namespaces, objects and API groups stay as
// they are. A host becomes host-1, host-2, an address ip-1, ip-2, the same one the same placeholder
// throughout the report, so that its lines can still be read together; a URL keeps its path and
// query (an API path names a resource, not an endpoint) with the placeholder for its server; a
// file path becomes <path>.

var (
	urlPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>()\[\]]+`)
	// ipv4Pattern matches an IPv4 address and its port.
	ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`)
	// ipv6Pattern matches what may be an IPv6 address, in brackets with a port or bare; a time of
	// day ("14:01:02") is told apart by isIPv6.
	ipv6Pattern = regexp.MustCompile(`(?i)\[[0-9a-f:.%]+\](?::\d+)?|\b(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}\b`)
	// pathPattern matches a file path: an absolute one under the usual roots, one in the home folder
	// by ~, and a Windows one.
	pathPattern = regexp.MustCompile(`(?:/(?:Users|home|root|private|var|tmp|etc|opt|Volumes|mnt|usr|Library|Applications)(?:/[^\s"'\])]*)?)|~/[^\s"'\])]*|\b[A-Za-z]:\\[^\s"'\])]*`)
)

// endpointScrubber replaces the endpoints of one report.
type endpointScrubber struct {
	hosts map[string]string // a host name, lower case → host-n
	ips   map[string]string // an address → ip-n
	home  string
}

// newEndpointScrubber learns the hosts of the URLs in text and in known (the server's URL, the
// proxy's), so that a host is replaced wherever it stands, also outside a URL.
func newEndpointScrubber(home, text string, known ...string) *endpointScrubber {
	e := &endpointScrubber{hosts: map[string]string{}, ips: map[string]string{}, home: home}
	for _, raw := range append(slices.Clone(known), urlPattern.FindAllString(text, -1)...) {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			e.placeholder(u.Hostname())
		}
	}
	return e
}

// placeholder is a host's or an address's placeholder, made on first sight.
func (e *endpointScrubber) placeholder(host string) string {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "" {
		return ""
	}
	if net.ParseIP(host) != nil {
		if p := e.ips[host]; p != "" {
			return p
		}
		e.ips[host] = "ip-" + strconv.Itoa(len(e.ips)+1)
		return e.ips[host]
	}
	if p := e.hosts[host]; p != "" {
		return p
	}
	e.hosts[host] = "host-" + strconv.Itoa(len(e.hosts)+1)
	return e.hosts[host]
}

// scrub replaces the endpoints in s.
func (e *endpointScrubber) scrub(s string) string {
	s = urlPattern.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return raw
		}
		u.User = nil
		u.Host = e.placeholder(u.Hostname())
		return u.String()
	})
	if e.home != "" && len(e.home) > 1 {
		s = strings.ReplaceAll(s, e.home, "~")
	}
	s = pathPattern.ReplaceAllString(s, "<path>")
	// Host names outside URLs, the longest first, with a port; also as a kubeconfig context names
	// a server (OpenShift's "project/api-example-com:6443/user"). A name without a dot is replaced
	// only with its port: "api" alone is a word, not a host.
	hosts := make([]string, 0, len(e.hosts))
	for h := range e.hosts {
		hosts = append(hosts, h)
	}
	slices.SortFunc(hosts, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), cmp.Compare(a, b)) })
	for _, h := range hosts {
		forms := []string{h}
		if strings.Contains(h, ".") {
			forms = append(forms, strings.ReplaceAll(h, ".", "-"))
		}
		for _, f := range forms {
			pat := `(?i)(^|[^a-z0-9.-])` + regexp.QuoteMeta(f) + `(:\d+)?([^a-z0-9-]|$)`
			if !strings.Contains(f, ".") && !strings.Contains(f, "-") {
				pat = `(?i)(^|[^a-z0-9.-])` + regexp.QuoteMeta(f) + `(:\d+)([^a-z0-9-]|$)`
			}
			re := regexp.MustCompile(pat)
			// Twice: a match takes the character after it, so neighbours share one.
			for range 2 {
				s = re.ReplaceAllString(s, "${1}"+e.hosts[h]+"${3}")
			}
		}
	}
	s = ipv4Pattern.ReplaceAllStringFunc(s, func(m string) string {
		host, _, err := net.SplitHostPort(m)
		if err != nil {
			host = m
		}
		if net.ParseIP(host) == nil {
			return m
		}
		return e.placeholder(host)
	})
	return ipv6Pattern.ReplaceAllStringFunc(s, func(m string) string {
		host := m
		if h, _, err := net.SplitHostPort(m); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if !isIPv6(host) {
			return m
		}
		return e.placeholder(host)
	})
}

// isIPv6 reports whether s is an IPv6 address and not a time of day: it parses, and it has a "::"
// or a hex letter or more than two colons.
func isIPv6(s string) bool {
	if i := strings.IndexByte(s, '%'); i >= 0 {
		s = s[:i]
	}
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() != nil && !strings.Contains(s, ":") {
		return false
	}
	return strings.Contains(s, "::") || strings.ContainsAny(strings.ToLower(s), "abcdef") || strings.Count(s, ":") > 2
}
