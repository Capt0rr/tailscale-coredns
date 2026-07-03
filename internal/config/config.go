package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"tailscale-coredns/pkg/api"
)

// Config holds all configuration for the tailscale-coredns service
type Config struct {
	// Tailscale OAuth credentials
	ClientID     string
	ClientSecret string

	// DNS configuration
	Domains     []string // Changed from Domain to Domains (plural)
	Hostname    string
	HostsFile   string
	ForwardTo   string
	RewriteFile string

	// Split DNS settings
	EnableSplitDNS bool
	Tailnet        string

	// Tailscale settings
	Ephemeral bool

	// Refresh interval
	RefreshInterval int

	// Additional configuration
	AdditionalConfig string

	// DNS format: when true, subdomain tags produce svc.host.domain
	// instead of the upstream default host.svc.domain
	SubFirst bool
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	config := &Config{}

	// Required: OAuth credentials
	config.ClientID = os.Getenv("TS_CLIENT_ID")
	config.ClientSecret = os.Getenv("TS_CLIENT_SECRET")

	if config.ClientID == "" || config.ClientSecret == "" {
		return nil, fmt.Errorf("TS_CLIENT_ID and TS_CLIENT_SECRET are required")
	}

	// Handle domains - check TS_DOMAINS first, fall back to TS_DOMAIN for backward compatibility
	domainsStr := os.Getenv("TS_DOMAINS")
	if domainsStr == "" {
		// Fall back to TS_DOMAIN for backward compatibility
		domain := os.Getenv("TS_DOMAIN")
		if domain == "" {
			return nil, fmt.Errorf("TS_DOMAINS or TS_DOMAIN is required")
		}
		domainsStr = domain
	}

	// Parse and validate domains
	domains, err := api.ParseDomains(domainsStr)
	if err != nil {
		return nil, fmt.Errorf("invalid domains: %w", err)
	}
	config.Domains = domains

	config.Hostname = os.Getenv("TS_HOSTNAME")
	if config.Hostname == "" {
		return nil, fmt.Errorf("TS_HOSTNAME is required")
	}

	// Optional: Hosts file
	config.HostsFile = os.Getenv("TS_HOSTS_FILE")
	if config.HostsFile == "" {
		// Check for default hosts file
		defaultHostsFile := "/etc/ts-dns/hosts/custom_hosts"
		if fileExists(defaultHostsFile) {
			config.HostsFile = defaultHostsFile
		}
	}

	// Optional: Forward server
	config.ForwardTo = os.Getenv("TS_FORWARD_TO")
	if config.ForwardTo == "" {
		config.ForwardTo = "/etc/resolv.conf"
	}

	// Optional: Rewrite file
	config.RewriteFile = os.Getenv("TS_REWRITE_FILE")

	// Optional: Split DNS
	config.EnableSplitDNS = strings.ToLower(os.Getenv("TS_ENABLE_SPLIT_DNS")) == "true"

	// Tailnet - use GetTailnetFromEnv which handles the "-" default
	tailnet, err := api.GetTailnetFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to get tailnet: %w", err)
	}
	config.Tailnet = tailnet

	// Optional: Ephemeral mode
	ephemeralStr := os.Getenv("TS_EPHEMERAL")
	if ephemeralStr == "" {
		config.Ephemeral = true // Default to true
	} else {
		config.Ephemeral = strings.ToLower(ephemeralStr) == "true"
	}

	// Optional: Refresh interval
	if intervalStr := os.Getenv("TSC_REFRESH_INTERVAL"); intervalStr != "" {
		if interval, err := strconv.Atoi(intervalStr); err == nil && interval > 0 {
			config.RefreshInterval = interval
		} else {
			return nil, fmt.Errorf("invalid TSC_REFRESH_INTERVAL value: %s", intervalStr)
		}
	} else {
		config.RefreshInterval = 30 // Default 30 seconds
	}

	// Optional: Additional configuration
	config.AdditionalConfig = os.Getenv("TS_ADDITIONAL_CONFIG")

	// Optional: subdomain-tag ordering; default true (svc.host.domain)
	subFirstStr := os.Getenv("TSC_SUB_FIRST")
	if subFirstStr == "" {
		config.SubFirst = true
	} else {
		config.SubFirst = strings.ToLower(subFirstStr) == "true"
	}

	return config, nil
}

// fileExists checks if a file exists
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

// GetPrimaryDomain returns the first domain in the list (for backward compatibility)
func (c *Config) GetPrimaryDomain() string {
	if len(c.Domains) > 0 {
		return c.Domains[0]
	}
	return ""
}

// Validate ensures all required configuration is present and valid
func (c *Config) Validate() error {
	if c.ClientID == "" {
		return fmt.Errorf("TS_CLIENT_ID is required")
	}

	if c.ClientSecret == "" {
		return fmt.Errorf("TS_CLIENT_SECRET is required")
	}

	if len(c.Domains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}

	if c.Hostname == "" {
		return fmt.Errorf("TS_HOSTNAME is required")
	}

	if c.RefreshInterval <= 0 {
		return fmt.Errorf("refresh interval must be positive")
	}

	// Validate hosts file exists if specified
	if c.HostsFile != "" && !fileExists(c.HostsFile) {
		return fmt.Errorf("hosts file does not exist: %s", c.HostsFile)
	}

	// Validate rewrite file exists if specified
	if c.RewriteFile != "" && !fileExists(c.RewriteFile) {
		return fmt.Errorf("rewrite file does not exist: %s", c.RewriteFile)
	}

	return nil
}

// normalizeTailnetName processes the organization name used to create the Tailscale account
func normalizeTailnetName(tailnet string) string {
	// Trim any whitespace
	tailnet = strings.TrimSpace(tailnet)

	return tailnet
}