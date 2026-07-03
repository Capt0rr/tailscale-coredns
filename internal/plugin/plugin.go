package plugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"net/netip"

	"tailscale.com/client/tailscale"
	"tailscale.com/ipn/ipnstate"

	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"tailscale-coredns/pkg/api"
)

type record struct {
	IPv4 net.IP
	IPv6 net.IP
}

type Tailscale struct {
	Next    plugin.Handler
	Domains []string // Changed from Domain to Domains (plural)
	records map[string]record
	mu      sync.RWMutex
	lc      *tailscale.LocalClient
	api     *api.Client
	// Split DNS management
	enableSplitDNS    bool
	splitDNSDomains   []string // Changed from splitDNSDomain to splitDNSDomains
	ownIP             string
	lastVerifiedIP    string
	lastSplitDNSCheck time.Time
	// subFirst controls subdomain-tag FQDN ordering:
	// true  → svc.host.domain (default in this fork)
	// false → host.svc.domain (upstream default)
	subFirst bool
}

func New(domains []string) (*Tailscale, error) {
	// Default subFirst to true; TSC_SUB_FIRST=false restores upstream order.
	subFirst := true
	if v := os.Getenv("TSC_SUB_FIRST"); v != "" {
		subFirst = strings.ToLower(v) == "true"
	}

	ts := &Tailscale{
		Domains:  domains,
		records:  make(map[string]record),
		lc:       &tailscale.LocalClient{Socket: "/run/tailscale/tailscaled.sock"},
		subFirst: subFirst,
	}

	// Initialize split DNS if enabled
	if err := ts.initializeSplitDNS(); err != nil {
		clog.Errorf("Failed to initialize split DNS: %v", err)
		// Continue without split DNS if initialization fails
	}

	go ts.periodicRefresh()
	return ts, nil
}

// initializeSplitDNS sets up split DNS functionality if enabled
func (t *Tailscale) initializeSplitDNS() error {
	// Check if split DNS is enabled
	if os.Getenv("TS_ENABLE_SPLIT_DNS") != "true" {
		return nil
	}

	// Get OAuth credentials
	clientID := os.Getenv("TS_CLIENT_ID")
	clientSecret := os.Getenv("TS_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("TS_CLIENT_ID and TS_CLIENT_SECRET are required for split DNS")
	}

	// Get tailnet from environment
	tailnet, err := api.GetTailnetFromEnv()
	if err != nil {
		return fmt.Errorf("failed to get tailnet: %w", err)
	}

	// Create API client
	t.api = api.NewClient(clientID, clientSecret, tailnet)
	t.enableSplitDNS = true
	t.splitDNSDomains = t.Domains

	clog.Infof("Split DNS enabled for domains: %v", t.splitDNSDomains)
	return nil
}

// GetOwnIP retrieves the current node's Tailscale IP
func (t *Tailscale) GetOwnIP() (string, error) {
	ctx := context.Background()
	status, err := t.lc.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get Tailscale status: %w", err)
	}

	if status == nil {
		return "", fmt.Errorf("received nil status from Tailscale - service may still be connecting")
	}

	if status.Self == nil {
		return "", fmt.Errorf("no self node found in Tailscale status - node may still be authenticating")
	}

	// Check if we have any IP addresses
	if len(status.Self.TailscaleIPs) == 0 {
		return "", fmt.Errorf("no Tailscale IPs assigned yet - node may still be connecting to the network")
	}

	// Get the first IPv4 address
	for _, ip := range status.Self.TailscaleIPs {
		if ip.Is4() {
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("no IPv4 address found for self node (only IPv6 available: %v)", status.Self.TailscaleIPs)
}

// AddToSplitDNS adds the current node's IP to split DNS for all configured domains
func (t *Tailscale) AddToSplitDNS() error {
	if !t.enableSplitDNS {
		return nil
	}

	clog.Info("Attempting to get own Tailscale IP for split DNS...")

	// Get own IP
	ownIP, err := t.GetOwnIP()
	if err != nil {
		return fmt.Errorf("failed to get own IP: %w", err)
	}

	t.ownIP = ownIP
	clog.Infof("Successfully retrieved own IP: %s", ownIP)

	clog.Infof("Adding IP %s to split DNS for domains: %v", ownIP, t.splitDNSDomains)

	ctx := context.Background()
	if err := t.api.AddIPToDomains(ctx, t.splitDNSDomains, ownIP); err != nil {
		return fmt.Errorf("failed to add IP to split DNS domains: %w", err)
	}

	clog.Infof("Successfully added IP %s to split DNS domains", ownIP)
	return nil
}

// RemoveFromSplitDNS removes the current node's IP from split DNS for all configured domains
func (t *Tailscale) RemoveFromSplitDNS() error {
	if !t.enableSplitDNS {
		return nil
	}

	// Use stored IP if available, otherwise try to get current IP
	ownIP := t.ownIP
	if ownIP == "" {
		var err error
		ownIP, err = t.GetOwnIP()
		if err != nil {
			clog.Warningf("Failed to get own IP for cleanup, using stored IP: %v", err)
			// If we can't get the IP and don't have a stored one, we can't clean up
			if t.ownIP == "" {
				return fmt.Errorf("no stored IP available for cleanup: %w", err)
			}
			ownIP = t.ownIP
		}
	}

	clog.Infof("Removing IP %s from split DNS for domains: %v", ownIP, t.splitDNSDomains)

	ctx := context.Background()
	if err := t.api.RemoveIPFromDomains(ctx, t.splitDNSDomains, ownIP); err != nil {
		return fmt.Errorf("failed to remove IP from split DNS domains: %w", err)
	}

	clog.Infof("Successfully removed IP %s from split DNS domains", ownIP)
	return nil
}

// getRefreshInterval returns the refresh interval in seconds from environment variable
// TSC_REFRESH_INTERVAL, defaulting to 30 seconds if not set or invalid.
func getRefreshInterval() time.Duration {
	if intervalStr := os.Getenv("TSC_REFRESH_INTERVAL"); intervalStr != "" {
		if interval, err := strconv.Atoi(intervalStr); err == nil && interval > 0 {
			return time.Duration(interval) * time.Second
		}
		clog.Warningf("invalid TSC_REFRESH_INTERVAL value '%s', using default 30 seconds", intervalStr)
	}
	return 30 * time.Second
}

// periodicRefresh periodically updates the DNS records.
// Using a ticker allows for better control and cleanup if needed in the future.
func (t *Tailscale) periodicRefresh() {
	interval := getRefreshInterval()
	ticker := time.NewTicker(interval)
	for range ticker.C {
		t.refresh()
	}
}

// refresh fetches the current Tailscale status and updates the local DNS records.
// This ensures that DNS queries reflect the latest network state.
func (t *Tailscale) refresh() {
	ctx := context.Background()
	status, err := t.lc.Status(ctx)
	if err != nil {
		clog.Errorf("failed to get Tailscale status: %v", err)
		return
	}
	if status == nil || status.Self == nil {
		clog.Warning("received nil status or self node from Tailscale")
		return
	}

	newRecords := make(map[string]record)

	// Process self node for all domains
	for _, domain := range t.Domains {
		t.processNodeForDomain(newRecords, status.Self, domain)
	}

	// Process peer nodes for all domains
	for _, peer := range status.Peer {
		for _, domain := range t.Domains {
			t.processNodeForDomain(newRecords, peer, domain)
		}
	}

	t.mu.Lock()
	t.records = newRecords
	t.mu.Unlock()

	// Periodically verify and update split DNS
	t.verifySplitDNS()
}

// verifySplitDNS checks if split DNS is properly configured and updates it if needed
func (t *Tailscale) verifySplitDNS() {
	if !t.enableSplitDNS {
		return
	}

	// Only check every 5 minutes to avoid excessive API calls
	now := time.Now()
	if now.Sub(t.lastSplitDNSCheck) < 5*time.Minute {
		return
	}
	t.lastSplitDNSCheck = now

	// Get current IP
	currentIP, err := t.GetOwnIP()
	if err != nil {
		clog.Errorf("Failed to get own IP for split DNS verification: %v", err)
		return
	}

	// Check if IP has changed or if we need to verify registration
	shouldUpdate := false
	if t.lastVerifiedIP != currentIP {
		if t.lastVerifiedIP == "" {
			clog.Infof("First time verification for IP %s, will verify split DNS", currentIP)
		} else {
			clog.Infof("Detected IP change from %s to %s, will verify split DNS", t.lastVerifiedIP, currentIP)
		}
		shouldUpdate = true
		t.ownIP = currentIP // Update the stored IP
	} else if t.lastVerifiedIP == "" {
		// First time verification
		shouldUpdate = true
	}

	if !shouldUpdate {
		return
	}

	// Verify all domains are in split DNS with our IP
	ctx := context.Background()
	splitDNSConfig, err := t.api.GetSplitDNS(ctx)
	if err != nil {
		clog.Errorf("Failed to get split DNS config: %v", err)
		return
	}

	// Check each domain
	needsUpdate := false
	for _, domain := range t.splitDNSDomains {
		nameservers := splitDNSConfig[domain]

		// Check if our IP is in the nameservers list
		found := false
		for _, ns := range nameservers {
			if ns == currentIP {
				found = true
				break
			}
		}

		if !found {
			clog.Warningf("IP %s not found in split DNS for domain %s, needs update", currentIP, domain)
			needsUpdate = true
			break
		}
	}

	if needsUpdate {
		clog.Info("Re-adding IP to split DNS domains...")
		if err := t.AddToSplitDNS(); err != nil {
			clog.Errorf("Failed to re-add IP to split DNS: %v", err)
			return
		}
	} else {
		clog.Debugf("Split DNS verification successful for all domains")
	}

	// Update last verified IP
	t.lastVerifiedIP = currentIP
}

// processNodeForDomain adds DNS records for a given node and domain, including any subdomain tags.
func (t *Tailscale) processNodeForDomain(records map[string]record, peer *ipnstate.PeerStatus, domain string) {
	host := strings.ToLower(peer.HostName)
	fqdn := host + "." + domain + "."
	records[fqdn] = t.ipsToRecord(peer.TailscaleIPs)

	if peer.Tags != nil {
		for _, tag := range peer.Tags.AsSlice() {
			if strings.HasPrefix(tag, "tag:subdomain-") {
				sub := strings.TrimPrefix(tag, "tag:subdomain-")
				sub = strings.ReplaceAll(sub, "-", ".")
				var subFqdn string
				if t.subFirst {
					// svc.host.domain — service label before host
					subFqdn = sub + "." + host + "." + domain + "."
				} else {
					// host.svc.domain — upstream default
					subFqdn = host + "." + sub + "." + domain + "."
				}
				records[subFqdn] = t.ipsToRecord(peer.TailscaleIPs)
			}
		}
	}
}

// ipsToRecord converts a list of IP addresses to a record struct,
// selecting the first IPv4 and IPv6 addresses found.
func (t *Tailscale) ipsToRecord(ips []netip.Addr) record {
	var ipv4, ipv6 net.IP
	for _, ip := range ips {
		if ip.Is4() && ipv4 == nil {
			ipv4 = ip.AsSlice()
		} else if ip.Is6() && ipv6 == nil {
			ipv6 = ip.AsSlice()
		}
	}
	return record{IPv4: ipv4, IPv6: ipv6}
}

func (t *Tailscale) Name() string { return "tailscale" }