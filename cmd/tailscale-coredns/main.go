package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"tailscale-coredns/internal/config"
	"tailscale-coredns/internal/plugin"
	"tailscale-coredns/internal/process"
	"tailscale-coredns/internal/template"
)

const banner = `
████████╗ █████╗ ██╗██╗     ███████╗ ██████╗ █████╗ ██╗     ███████╗     ██████╗ ██████╗ ██████╗ ███████╗██████╗ ███╗   ██╗███████╗
╚══██╔══╝██╔══██╗██║██║     ██╔════╝██╔════╝██╔══██╗██║     ██╔════╝    ██╔════╝██╔═══██╗██╔══██╗██╔════╝██╔══██╗████╗  ██║██╔════╝
   ██║   ███████║██║██║     ███████╗██║     ███████║██║     █████╗      ██║     ██║   ██║██████╔╝█████╗  ██║  ██║██╔██╗ ██║███████╗
   ██║   ██╔══██║██║██║     ╚════██║██║     ██╔══██║██║     ██╔══╝      ██║     ██║   ██║██╔══██╗██╔══╝  ██║  ██║██║╚██╗██║╚════██║
   ██║   ██║  ██║██║███████╗███████║╚██████╗██║  ██║███████╗███████╗    ╚██████╗╚██████╔╝██║  ██║███████╗██████╔╝██║ ╚████║███████║
   ╚═╝   ╚═╝  ╚═╝╚═╝╚══════╝╚══════╝ ╚═════╝╚═╝  ╚═╝╚══════╝╚══════╝     ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═════╝ ╚═╝  ╚═══╝╚══════╝

A CoreDNS plugin that allows you to resolve DNS names to Tailscale IPs.

By Christian De Leon (https://github.com/christian-deleon/tailscale-coredns)
`

func main() {
	log.Print(banner)
	log.Println("Starting tailscale-coredns service...")

	// Load configuration from environment
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Load additional configuration if available
	additionalConfig, err := template.LoadAdditionalConfig("")
	if err != nil {
		log.Printf("Warning: Failed to load additional config: %v", err)
	} else if additionalConfig != "" {
		log.Println("Found additional configuration")
		cfg.AdditionalConfig = additionalConfig
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	log.Printf("Configuration loaded:")
	log.Printf("  Domains: %s", strings.Join(cfg.Domains, ", "))
	log.Printf("  Hostname: %s", cfg.Hostname)
	log.Printf("  Split DNS: %t", cfg.EnableSplitDNS)
	if cfg.Tailnet != "" && cfg.Tailnet != "-" {
		log.Printf("  Tailnet: %s", cfg.Tailnet)
	} else {
		log.Printf("  Tailnet: %s (default)", cfg.Tailnet)
	}
	log.Printf("  Ephemeral: %t", cfg.Ephemeral)
	log.Printf("  Hosts file: %s", cfg.HostsFile)
	log.Printf("  Forward to: %s", cfg.ForwardTo)
	if cfg.RewriteFile != "" {
		log.Printf("  Rewrite file: %s", cfg.RewriteFile)
	}
	log.Printf("  Refresh interval: %d seconds", cfg.RefreshInterval)
	log.Printf("  Subdomain order: sub-first=%t", cfg.SubFirst)

	// Generate Corefile
	generator, err := template.NewGenerator()
	if err != nil {
		log.Fatalf("Failed to create template generator: %v", err)
	}

	corefilePath := "/Corefile"
	if err := generator.WriteCorefile(cfg, corefilePath); err != nil {
		log.Fatalf("Failed to generate Corefile: %v", err)
	}

	// Print generated Corefile
	corefileContent, _ := generator.GenerateCorefile(cfg)
	log.Println("Generated Corefile:")
	log.Println(corefileContent)

	// Create process manager
	processManager := process.NewManager(cfg)

	// Start tailscaled
	if err := processManager.StartTailscaled(); err != nil {
		log.Fatalf("Failed to start tailscaled: %v", err)
	}

	// Wait for tailscaled socket
	if err := processManager.WaitForTailscaledSocket(); err != nil {
		log.Fatalf("Failed to wait for tailscaled socket: %v", err)
	}

	// Authenticate with Tailscale
	if err := processManager.AuthenticateTailscale(); err != nil {
		log.Fatalf("Failed to authenticate with Tailscale: %v", err)
	}

	// Wait for Tailscale connection
	if err := processManager.WaitForTailscaleConnection(); err != nil {
		log.Fatalf("Failed to establish Tailscale connection: %v", err)
	}

	log.Println("Tailscale connection established successfully")

	// Initialize split DNS if enabled (after authentication and connection)
	var splitDNSManager *plugin.SplitDNSManager
	if cfg.EnableSplitDNS {
		log.Println("Initializing split DNS...")

		// Create plugin instance for split DNS
		ts, err := plugin.New(cfg.Domains)
		if err != nil {
			log.Fatalf("Failed to create plugin instance: %v", err)
		}

		splitDNSManager = plugin.NewSplitDNSManager(ts)

		// Initialize split DNS now that Tailscale is connected
		if err := splitDNSManager.Initialize(); err != nil {
			log.Fatalf("Failed to initialize split DNS: %v", err)
		}
	}

	// Start CoreDNS
	log.Println("Starting CoreDNS...")
	if err := processManager.StartCoreDNS(corefilePath); err != nil {
		log.Fatalf("Failed to start CoreDNS: %v", err)
	}

	log.Println("All services started successfully")

	// Set up cleanup function that runs even if the process is terminated
	defer func() {
		// Cleanup split DNS if enabled
		if splitDNSManager != nil {
			log.Println("Executing split DNS cleanup...")
			if err := splitDNSManager.Cleanup(); err != nil {
				log.Printf("Error during split DNS cleanup: %v", err)
			} else {
				log.Println("Split DNS cleanup completed successfully")
			}
		}
	}()

	// Run with signal handling
	log.Println("Service is ready and waiting for connections...")
	if err := processManager.RunWithSignalHandling(); err != nil {
		log.Printf("Process manager error: %v", err)
	}

	log.Println("Service shutdown completed successfully")
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: %s

Environment Variables:
  TS_CLIENT_ID         Tailscale OAuth client ID (required)
  TS_CLIENT_SECRET     Tailscale OAuth client secret (required)
  TS_DOMAINS           Comma-separated list of domains for DNS resolution (required)
  TS_DOMAIN            Single domain for DNS resolution (deprecated, use TS_DOMAINS)
  TS_HOSTNAME          Hostname for this instance (required)
  TS_ENABLE_SPLIT_DNS  Enable split DNS management (default: false)
  TS_TAILNET           Explicit tailnet name (optional, uses "-" for default if not set)
  TS_HOSTS_FILE        Path to custom hosts file (optional)
  TS_REWRITE_FILE      Path to rewrite rules file (optional)
  TS_FORWARD_TO        Forward server for unresolved queries (default: /etc/resolv.conf)
  TS_EPHEMERAL         Enable ephemeral mode (default: true)
  TSC_REFRESH_INTERVAL Refresh interval in seconds (default: 30)
  TSC_SUB_FIRST        Subdomain-tag ordering: true=svc.host.domain, false=host.svc.domain (default: true)

`, os.Args[0])
}