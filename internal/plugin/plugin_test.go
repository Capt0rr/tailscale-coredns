package plugin

import (
	"net/netip"
	"testing"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/views"
)

// peerWithTags builds a minimal PeerStatus for table tests.
func peerWithTags(host string, tags ...string) *ipnstate.PeerStatus {
	var tagSlice views.Slice[string]
	if len(tags) > 0 {
		tagSlice = views.SliceOf(tags)
	}
	return &ipnstate.PeerStatus{
		HostName:     host,
		TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.0.0.1")},
		Tags:         &tagSlice,
	}
}

func TestProcessNodeForDomain_SubdomainOrdering(t *testing.T) {
	cases := []struct {
		name     string
		subFirst bool
		host     string
		tag      string // full tag value e.g. "tag:subdomain-web"
		domain   string
		wantFQDN string
	}{
		{
			name:     "sub-first default: svc.host.domain",
			subFirst: true,
			host:     "mybox",
			tag:      "tag:subdomain-web",
			domain:   "example.com",
			wantFQDN: "web.mybox.example.com.",
		},
		{
			name:     "sub-first=false: host.svc.domain (upstream behaviour)",
			subFirst: false,
			host:     "mybox",
			tag:      "tag:subdomain-web",
			domain:   "example.com",
			wantFQDN: "mybox.web.example.com.",
		},
		{
			name:     "multi-segment tag: hyphens become dots in sub",
			subFirst: true,
			host:     "mybox",
			tag:      "tag:subdomain-api-v2",
			domain:   "example.com",
			wantFQDN: "api.v2.mybox.example.com.",
		},
		{
			name:     "multi-segment tag sub-first=false",
			subFirst: false,
			host:     "mybox",
			tag:      "tag:subdomain-api-v2",
			domain:   "example.com",
			wantFQDN: "mybox.api.v2.example.com.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := &Tailscale{
				Domains:  []string{tc.domain},
				records:  make(map[string]record),
				subFirst: tc.subFirst,
			}

			peer := peerWithTags(tc.host, tc.tag)
			got := make(map[string]record)
			ts.processNodeForDomain(got, peer, tc.domain)

			if _, ok := got[tc.wantFQDN]; !ok {
				t.Errorf("want FQDN %q in records; got keys: %v", tc.wantFQDN, keys(got))
			}
		})
	}
}

// base record (no tags) must always be registered under host.domain regardless of subFirst.
func TestProcessNodeForDomain_BaseRecord(t *testing.T) {
	for _, subFirst := range []bool{true, false} {
		ts := &Tailscale{
			records:  make(map[string]record),
			subFirst: subFirst,
		}
		peer := peerWithTags("mybox") // no tags
		got := make(map[string]record)
		ts.processNodeForDomain(got, peer, "example.com")

		want := "mybox.example.com."
		if _, ok := got[want]; !ok {
			t.Errorf("subFirst=%v: base record %q missing; got %v", subFirst, want, keys(got))
		}
		if len(got) != 1 {
			t.Errorf("subFirst=%v: expected exactly 1 record, got %d: %v", subFirst, len(got), keys(got))
		}
	}
}

func keys(m map[string]record) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
