package plugin

import (
	"context"
	"strings"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

// ServeDNS handles DNS requests for the Tailscale domains.
func (t *Tailscale) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	queryName := state.Name()

	// Check if query is for any of our Tailscale domains
	matchesDomain := false
	for _, domain := range t.Domains {
		if strings.HasSuffix(queryName, domain+".") {
			matchesDomain = true
			break
		}
	}

	if !matchesDomain {
		return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
	}

	t.mu.RLock()
	rec, ok := t.records[queryName]
	t.mu.RUnlock()
	if !ok {
		return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	header := dns.RR_Header{Name: queryName, Rrtype: state.QType(), Class: state.QClass(), Ttl: 60}

	switch state.QType() {
	case dns.TypeA:
		if rec.IPv4 == nil {
			return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
		}
		m.Answer = append(m.Answer, &dns.A{Hdr: header, A: rec.IPv4})
	case dns.TypeAAAA:
		if rec.IPv6 == nil {
			// No IPv6 address — return NODATA (empty ANSWER, NOERROR) rather than
			// falling through to NextOrFailure which yields SERVFAIL. Clients
			// interpret NODATA as "no IPv6, try A" and resolve correctly.
			m.Answer = []dns.RR{}
			if err := w.WriteMsg(m); err != nil {
				return dns.RcodeServerFailure, err
			}
			return dns.RcodeSuccess, nil
		}
		m.Answer = append(m.Answer, &dns.AAAA{Hdr: header, AAAA: rec.IPv6})
	default:
		return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
	}

	if err := w.WriteMsg(m); err != nil {
		return dns.RcodeServerFailure, err
	}
	return dns.RcodeSuccess, nil
}