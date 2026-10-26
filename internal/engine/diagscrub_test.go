package engine

import (
	"strings"
	"testing"
)

func TestEndpointScrubber(t *testing.T) {
	text := strings.Join([]string{
		"Server: https://api.ocp.example.com:6443",
		"refused list pods: GET https://jdoe:pw@api.ocp.example.com:6443/api/v1/namespaces/team-a/pods?limit=500 · pods is forbidden: User \"jdoe\" cannot list resource \"pods\" in API group \"\" in the namespace \"team-a\"",
		"Context: team-a/api-ocp-example-com:6443/jdoe",
		"dial tcp 10.20.30.40:6443: i/o timeout",
		"proxy http://proxy.corp.example:3128 answered 407",
		"lookup api.ocp.example.com on [fd00::53]:53: no such host",
		"kubeconfig /Users/jdoe/.kube/config and ~/work/ocp.yaml and C:\\Users\\jdoe\\kube.yaml",
		"12:01:02  refused  list routes.route.openshift.io in team-a",
		"also /Users/jdoe/Library/Application Support/Cogpit",
		"discovery /apis/metrics.k8s.io/v1beta1 · group api answered",
	}, "\n")
	e := newEndpointScrubber("/Users/jdoe", text, "https://api.ocp.example.com:6443")
	got := e.scrub(text)
	for _, gone := range []string{"ocp.example.com", "api-ocp-example-com", "10.20.30.40", "proxy.corp.example", "fd00::53",
		"/Users/jdoe", "work/ocp.yaml", "C:\\Users", ":6443", ":3128", "pw@"} {
		if strings.Contains(got, gone) {
			t.Errorf("%q kept in:\n%s", gone, got)
		}
	}
	for _, kept := range []string{
		"https://host-1/api/v1/namespaces/team-a/pods?limit=500", // the server replaced, the path kept
		`User "jdoe" cannot list`, "team-a/host-1/jdoe", "http://host-2", "ip-1", "ip-2",
		"12:01:02", "routes.route.openshift.io", "/apis/metrics.k8s.io/v1beta1", "group api answered", "<path>",
	} {
		if !strings.Contains(got, kept) {
			t.Errorf("%q missing in:\n%s", kept, got)
		}
	}
}

func TestIsIPv6(t *testing.T) {
	for s, want := range map[string]bool{"fd00::53": true, "2001:db8:0:0:0:0:0:1": true, "fe80::1%en0": true,
		"14:01:02": false, "1:2": false, "10.0.0.1": false, "abc": false} {
		if got := isIPv6(s); got != want {
			t.Errorf("isIPv6(%q) = %v, want %v", s, got, want)
		}
	}
}
