# Tailscale CoreDNS High-Availability Service

This project provides a high-availability Go service that integrates a CoreDNS plugin for resolving DNS names to Tailscale IPs, using tags for nested subdomains. The service includes automatic process management, graceful shutdown, and split DNS functionality for HA deployments.

## Fork changes

This is a fork of [christian-deleon/tailscale-coredns](https://github.com/christian-deleon/tailscale-coredns).
It contains **one change** from upstream: the `TSC_SUB_FIRST` environment variable.

By default (and in upstream), subdomain tags produce FQDNs in `host.svc.domain` order.
This fork flips the default to `svc.host.domain`, which is more natural for service discovery
(the service label is the first label you look up, not the machine name).

| Variable | Default | Behaviour |
|---|---|---|
| `TSC_SUB_FIRST=true` | **default** | `svc.host.domain` — service label first |
| `TSC_SUB_FIRST=false` | | `host.svc.domain` — upstream order |

Example: a node `mybox` tagged `tag:subdomain-web` with domain `example.com`:
- `TSC_SUB_FIRST=true`  → `web.mybox.example.com`
- `TSC_SUB_FIRST=false` → `mybox.web.example.com`

## Features

- **High Availability**: Full Go service with automatic process management and graceful shutdown
- **Tailscale Integration**: Automatically resolves Tailscale hostnames to their IP addresses
- **Multiple Domains**: Support for managing multiple domains in a single instance
- **Subdomain Tags**: Support for custom subdomains using Tailscale tags (`tag:subdomain-*`)
- **Hosts File Support**: Works with CoreDNS's built-in `hosts` plugin for custom DNS entries
- **Forward Server**: Works with CoreDNS's built-in `forward` plugin for unresolved queries
- **IPv4/IPv6 Support**: Full support for both IPv4 and IPv6 addresses
- **Periodic Refresh**: Configurable refresh interval to keep DNS records up-to-date
- **Process Management**: Monitors and manages CoreDNS and Tailscale processes
- **Graceful Shutdown**: Proper cleanup and signal handling for container orchestration
- **Split DNS Support**: Optional split DNS management for high availability deployments
- **OAuth Authentication**: Secure authentication using Tailscale OAuth credentials
- **Go Native**: Pure Go implementation
- **Configuration Templating**: Built-in Corefile generation with Go templates

## Authentication

The plugin uses Tailscale OAuth for authentication. You can create OAuth credentials from the [Tailscale admin console](https://login.tailscale.com/admin/settings/oauth).

The OAuth client requires the following permissions:

- `dns:read` - Read DNS configuration
- `dns:write` - Write DNS configuration (for split DNS functionality)

## Split DNS Functionality

The plugin supports optional split DNS management for high availability deployments. When enabled, each instance will:

- Add its own Tailscale IP to the split DNS configuration on startup
- Remove its IP from the split DNS configuration on shutdown
- Only manage its own IP, preserving other instances' IPs in the split DNS configuration

**Note**: Currently, split DNS management uses a read-modify-write pattern that can lead to race conditions when multiple instances start/stop simultaneously. Future versions will implement proper synchronization.

## Installation

### Docker Deployment (Recommended)

1. **Clone the repository**:

   ```bash
   git clone https://github.com/christian-deleon/tailscale-coredns.git
   cd tailscale-coredns
   ```

2. **Create environment file**:

   ```bash
   cp docker/example.env docker/.env
   ```

3. **Configure custom files** (optional):

   ```bash
   # Custom hosts file
   cp docker/ts-dns/hosts/custom_hosts docker/ts-dns/hosts/custom_hosts.example
   # Edit the file with your custom DNS entries

   # Additional configuration
   cp docker/additional.conf.example docker/ts-dns/additional/additional.conf
   # Edit the file with your additional CoreDNS configuration
   ```

   **Hosts File Format**: The hosts file uses standard hosts file format:

   ```text
   # Custom DNS entries
   192.168.1.100    serviceA.mydomain.com
   192.168.1.101    serviceB.mydomain.com
   192.168.1.102    serviceC.mydomain.com
   ```

   **Rewrite Rules Format**: The rewrite file uses CoreDNS rewrite plugin syntax:

   ```text
   # Rewrite rules for CoreDNS
   # Each line should contain a rewrite rule in the format expected by CoreDNS rewrite plugin
   # Lines starting with # are comments and will be ignored

   # Example: Rewrite www.example.com to example.com
   name www.example.com example.com

   # Example: Rewrite with regex pattern
   name regex (.*)\.example\.com {1}.example.com

   # Example: Rewrite with response rewrite
   answer name example.com www.example.com

   # Example: Rewrite with CNAME
   name example.com cname.example.com
   ```

4. **Edit the environment file** with your Tailscale credentials:

   ```bash
   # Required: Tailscale OAuth credentials
   # Get these from https://login.tailscale.com/admin/settings/oauth
   TS_CLIENT_ID=tskey-client-abc123def456ghi
   TS_CLIENT_SECRET=tskey-client-abc123def456ghi-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

   # Required: Domains for DNS resolution (comma-separated list)
   # Examples:
   # TS_DOMAINS=mydomain.com                           # Single domain
   # TS_DOMAINS=mydomain.com,staging.mydomain.com      # Multiple domains
   TS_DOMAINS=mydomain.com

   # Required: Hostname for this CoreDNS instance
   TS_HOSTNAME=ts-dns

   # Optional: Enable split DNS functionality (default: false)
   # When enabled, this instance will add its IP to the split DNS configuration
   TS_ENABLE_SPLIT_DNS=false

   # Optional: Tailnet name (uses "-" for default if not set)
   # Set this to your Tailscale organization name if needed:
   # TS_TAILNET=mydomain.com                  # Domain format
   # TS_TAILNET=name@mydomain.com             # Email format
   TS_TAILNET=

   # Optional: Path to hosts file (default: /etc/ts-dns/hosts/custom_hosts)
   TS_HOSTS_FILE=/etc/ts-dns/hosts/custom_hosts

   # Optional: Path to rewrite rules file (default: /etc/ts-dns/rewrite/rewrite.conf)
   TS_REWRITE_FILE=/etc/ts-dns/rewrite/rewrite.conf

   # Optional: Forward server for unresolved queries (default: /etc/resolv.conf)
   TS_FORWARD_TO=8.8.8.8

   # Optional: Enable ephemeral mode for Tailscale (default: true)
   TS_EPHEMERAL=true

   # Optional: Refresh interval in seconds (default: 30)
   TSC_REFRESH_INTERVAL=30
   ```

5. **Start the service**:

   ```bash
   cd docker
   docker compose --env-file .env up --build
   ```

### Manual Build

1. **Prerequisites**:
   - Go 1.22 or later
   - Git

2. **Build the service**:

   ```bash
   # Clone the repository
   git clone https://github.com/christian-deleon/tailscale-coredns.git
   cd tailscale-coredns

   # Build the main service
   go build -o tailscale-coredns ./cmd/tailscale-coredns

   # Build the split DNS tool
   go build -o splitdns ./cmd/splitdns

   # Build custom CoreDNS with plugin (optional, for standalone use)
   git clone https://github.com/coredns/coredns.git
   cd coredns
   git checkout v1.11.3
   sed -i '/^forward:/i tailscale:tailscale-coredns' plugin.cfg
   go mod edit -require=tailscale-coredns@v0.0.0
   go mod edit -replace=tailscale-coredns=/path/to/tailscale-coredns
   go generate
   go mod tidy
   go build -o coredns .
   ```

3. **Run the service**:

   ```bash
   # Set required environment variables
   export TS_CLIENT_ID="your-client-id"
   export TS_CLIENT_SECRET="your-client-secret"
   export TS_DOMAINS="your-domain.com"
   export TS_HOSTNAME="ts-dns"

   # Run the service
   ./tailscale-coredns
   ```

## Configuration

### Environment Variables

- `TS_CLIENT_ID` (required): Tailscale OAuth client ID
- `TS_CLIENT_SECRET` (required): Tailscale OAuth client secret
- `TS_DOMAINS` (required): Comma-separated list of domains for DNS resolution
- `TS_DOMAIN` (deprecated): Single domain for DNS resolution (use TS_DOMAINS instead)
- `TS_HOSTNAME` (required): Hostname for this CoreDNS instance
- `TS_ENABLE_SPLIT_DNS` (optional): Enable split DNS functionality (default: false)
- `TS_TAILNET` (optional): Your Tailscale organization name (e.g., `mydomain.com` or `name@mydomain.com`). If not set, uses "-" for default tailnet
- `TS_HOSTS_FILE` (optional): Path to hosts file for custom DNS entries (default: /etc/ts-dns/hosts/custom_hosts)
- `TS_REWRITE_FILE` (optional): Path to rewrite rules file (default: /etc/ts-dns/rewrite/rewrite.conf)
- `TS_FORWARD_TO` (optional): Forward server for unresolved queries (default: /etc/resolv.conf)
- `TS_EPHEMERAL` (optional): Enable ephemeral mode for Tailscale (default: true). When set to true, the node will be automatically removed when it goes offline and the service will logout on shutdown
- `TSC_REFRESH_INTERVAL` (optional): Refresh interval in seconds (default: 30)
- `TSC_SUB_FIRST` (optional): Subdomain-tag FQDN ordering — `true` = `svc.host.domain`, `false` = `host.svc.domain` (default: true; see [Fork changes](#fork-changes))

### Split DNS Configuration

When `TS_ENABLE_SPLIT_DNS` is set to `true`, the plugin will:

1. **On Startup**: Add the current instance's Tailscale IP to the nameserver list for each configured domain
2. **On Shutdown**: Remove the current instance's IP from the nameserver list for each configured domain
3. **High Availability**: Multiple instances can run simultaneously, each managing only its own IP

**Requirements for Split DNS**:

- The OAuth client must have `dns:read` and `dns:write` permissions
- Each domain must have at least a second-level domain and TLD (e.g., `example.com`)

**Finding Your Tailnet Name**:

If split DNS operations fail with 404 errors, you may need to explicitly set your tailnet name. The tailnet name is your Tailscale organization name, which can be:

1. **Domain format**: `mydomain.com`
2. **Email format**: `name@mydomain.com`
3. **Default**: Use `-` or leave empty for the default tailnet

You can find your tailnet name in the Tailscale admin console or by checking your organization settings.

### Directory Structure

The plugin uses `/etc/ts-dns/` as the base directory for configuration files:

```text
/etc/ts-dns/
├── hosts/
│   └── custom_hosts          # Custom DNS entries (hosts file format)
├── rewrite/
│   └── rewrite.conf          # Rewrite rules for CoreDNS rewrite plugin
└── additional/
    └── additional.conf       # Additional CoreDNS configuration for plugins
```

### Volume Mounts

- `/etc/ts-dns/hosts/custom_hosts` (optional): Custom hosts file for DNS entries
- `/etc/ts-dns/rewrite/rewrite.conf` (optional): Rewrite rules file for CoreDNS rewrite plugin
- `/etc/ts-dns/additional/additional.conf` (optional): Additional CoreDNS configuration for built-in plugins like route53, etcd, kubernetes

### Corefile Configuration

The plugin supports a simple Corefile configuration:

```corefile
. {
    tailscale mydomain.com staging.mydomain.com
    hosts /etc/ts-dns/hosts/custom_hosts {
        fallthrough
    }
    rewrite name www.mydomain.com mydomain.com
    forward . 8.8.8.8
    log
    errors
}
```

### Additional Plugins

You can extend the CoreDNS configuration with additional built-in plugins by mounting an `additional.conf` file. This allows you to use plugins like `route53`, `etcd`, `kubernetes`, `cache`, and `prometheus`.

#### Setup

1. **Create an additional configuration file**:

   ```bash
   cp docker/additional.conf.example docker/ts-dns/additional/additional.conf
   ```

2. **Edit the configuration** with your specific plugin settings:

   ```corefile
   # Example: Route53 plugin
   example.private. {
       route53 example.private.:Z0123456789ABCDEF
       fallthrough
       log
       errors
   }
   ```

3. **Mount the file** in your compose.yml (already configured):

   ```yaml
   volumes:
     - ./ts-dns/additional/additional.conf:/etc/ts-dns/additional/additional.conf:ro
   ```

#### Examples

**Route53 Plugin**:

```corefile
example.private. {
    route53 example.private.:Z0123456789ABCDEF
    fallthrough
    log
    errors
}
```

## Usage

### DNS Resolution

Once the service is running, you can resolve Tailscale hostnames:

```bash
# Resolve a Tailscale hostname
dig hostname.mydomain.com @localhost

# Resolve with subdomain tags
dig hostname.subdomain.mydomain.com @localhost
```

### Subdomain Tags

Devices can be tagged in Tailscale to create custom subdomains:

1. Tag a device with `tag:subdomain-web-server` in Tailscale
2. The device will be resolvable at `web.server.hostname.mydomain.com` (with `TSC_SUB_FIRST=true`, the default in this fork)

Tags are converted from hyphens to dots to create the subdomain hierarchy.

### Split DNS Management

When split DNS is enabled, you can manage it using the `splitdns` tool:

```bash
# Check split DNS status
./splitdns -action=status -domains=mydomain.com

# Initialize split DNS manually
./splitdns -action=init -domains=mydomain.com,staging.mydomain.com

# Cleanup split DNS manually
./splitdns -action=cleanup -domains=mydomain.com
```

### High Availability Deployment

For high availability deployments with split DNS:

1. Deploy multiple instances of the service
2. Set `TS_ENABLE_SPLIT_DNS=true` on each instance
3. Each instance will add its IP to the split DNS configuration
4. Tailscale will load balance DNS queries across all registered IPs
5. When an instance shuts down, it automatically removes its IP

**Note**: The `TS_HOSTNAME` value doesn't need to be unique across instances. Tailscale automatically handles the identification and management of each instance's IP address in the split DNS configuration. You can use the same hostname for all instances or different hostnames - it doesn't affect the split DNS functionality.

#### Demo Configuration

The included `compose.yml` file is configured with 3 replicas for demonstration and testing purposes. For production deployments, comment out the `deploy` section to run a single instance, or deploy across multiple hosts for true high availability.

**Important**: The demo configuration with 3 replicas is **NOT** true high availability because all replicas run on the same host. If the host fails, all instances will be unavailable.

#### Production HA Best Practices

For true high availability in production environments:

1. **Deploy across multiple hosts/servers**: Each instance should run on a separate physical or virtual machine
2. **Use different availability zones**: Deploy instances across different data centers or cloud availability zones
3. **Monitor instance health**: Implement health checks
4. **Use container orchestration**: Consider Kubernetes, or similar for production deployments

**Load Balancing Note**: When using split DNS management (`TS_ENABLE_SPLIT_DNS=true`), Tailscale automatically routes DNS traffic to the registered ts-dns instances. Tailscale queries multiple nameservers sequentially - it only falls back to the next server if the previous one fails to respond (e.g., timeout or connection error), not if it returns a "no such domain" (NXDOMAIN) response. No external load balancer is needed when using split DNS.

If you prefer to self-manage DNS traffic without split DNS, you can use an external load balancer to direct traffic directly to ts-dns instances, but this project is designed to work with Tailscale's split DNS functionality.

For production deployments, deploy the same compose.yml file across multiple hosts. Tailscale will automatically handle the identification and management of each instance's IP address in the split DNS configuration.

## Advanced Configuration

### Corefile Generation

The service automatically generates a Corefile based on your configuration:

```text
. {
    tailscale mydomain.com staging.mydomain.com
    hosts /etc/ts-dns/hosts/custom_hosts {
        fallthrough
    }
    forward . 8.8.8.8
    log
    errors
}
```

### Additional Configuration

You can add custom CoreDNS configuration by creating a file at `/etc/ts-dns/additional/additional.conf`:

```text
# Custom server block for internal domain
internal.local {
    file /etc/coredns/zones/internal.local
    log
}
```

### Custom Plugin Usage

If you need to use the plugin in a custom CoreDNS build:

```text
# Corefile
. {
    tailscale mydomain.com staging.mydomain.com {
        # Plugin accepts multiple domains
    }
    forward . 8.8.8.8
    log
    errors
}
```

### Docker Compose Commands

The Docker deployment includes helpful commands via `just`:

```bash
# Build the Docker image
just build

# Start the service
just start

# Stop the service
just stop

# Clean up (remove containers, volumes, and images)
just clean
```

## Development

### Project Structure

```text
tailscale-coredns/
├── cmd/                      # Command-line applications
│   ├── tailscale-coredns/    # Main HA service application
│   │   └── main.go
│   └── splitdns/             # Split DNS management tool
│       └── main.go
├── internal/                 # Internal packages
│   ├── config/               # Configuration management
│   │   └── config.go
│   ├── plugin/               # CoreDNS plugin implementation
│   │   ├── plugin.go         # Main plugin logic
│   │   ├── serve.go          # DNS request handler
│   │   ├── setup.go          # Plugin initialization
│   │   └── splitdns.go       # Split DNS management
│   ├── process/              # Process management
│   │   └── manager.go
│   └── template/             # Configuration templating
│       └── corefile.go
├── pkg/                      # Public packages
│   └── api/                  # Tailscale API client
│       └── client.go
├── docker/                   # Docker deployment files
│   ├── Dockerfile            # Go-based container
│   ├── compose.yml
│   ├── example.env
│   ├── justfile              # Common commands
│   └── ts-dns/               # Default configuration files
│       ├── hosts/
│       │   └── custom_hosts
│       └── additional/
│           └── additional.conf
├── go.mod                    # Go module dependencies
├── go.sum                    # Go module checksums
└── README.md
```

### Local Development

1. **Run tests**:

   ```bash
   go test ./...
   ```

2. **Build the applications**:

   ```bash
   # Build the main service
   go build ./cmd/tailscale-coredns

   # Build the split DNS tool
   go build ./cmd/splitdns
   ```

3. **Test locally**:

   ```bash
   # Set environment variables
   export TS_CLIENT_ID="your-client-id"
   export TS_CLIENT_SECRET="your-client-secret"
   export TS_DOMAINS="your-domain.com,staging.your-domain.com"
   export TS_HOSTNAME="test-server"

   # Run the service
   ./tailscale-coredns
   ```

4. **Development tools**:

   ```bash
   # Check split DNS status
   ./splitdns -action=status -domains=your-domain.com

   # Initialize split DNS manually
   ./splitdns -action=init -domains=your-domain.com,staging.your-domain.com

   # Cleanup split DNS manually
   ./splitdns -action=cleanup -domains=your-domain.com
   ```

## Troubleshooting

### Common Issues

1. **Plugin not resolving Tailscale hostnames**:
   - Verify Tailscale is running and authenticated
   - Check the Tailscale socket path: `/run/tailscale/tailscaled.sock`
   - Ensure the OAuth key has the correct permissions

2. **DNS queries timing out**:
   - Check the forward server configuration
   - Verify network connectivity
   - Review CoreDNS logs for errors

3. **Subdomain tags not working**:
   - Ensure tags are properly formatted: `tag:subdomain-web-server`
   - Check that devices have the correct tags applied
   - Verify the tag conversion (hyphens become dots)

4. **Split DNS not working**:
   - Verify `TS_ENABLE_SPLIT_DNS=true` is set
   - Check that the OAuth client has `dns:read` and `dns:write` permissions
   - Ensure domains are valid (at least second-level domain + TLD)
   - Check if `TS_TAILNET` needs to be set explicitly

### Debug Commands

```bash
# Check Tailscale status
tailscale status

# Test DNS resolution
dig hostname.mydomain.com @localhost

# View CoreDNS logs
docker logs tailscale-coredns

# Check Tailscale connectivity
tailscale ping hostname

# View split DNS configuration (if enabled)
./splitdns -action=status -domains=mydomain.com
```

## Roadmap

### Planned Features

- **CNAME Support**: Support for CNAME records
  - Ability to resolve CNAME records to Tailscale devices or a custom domain
- **Built-in DNS Manager**: Automated health monitoring and IP management for split DNS instances
  - Automatic removal of unhealthy instance IPs from split DNS configuration
  - API request locking to prevent race conditions during concurrent instance startup/shutdown
  - Health check integration with container orchestration platforms
  - Improved reliability for high-availability deployments

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature-name`
3. Make your changes
4. Submit a pull request

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Author

Created by [Christian De Leon](https://github.com/christian-deleon)

## Support

For issues and questions:

- [GitHub Issues](https://github.com/christian-deleon/tailscale-coredns/issues)
- [Tailscale Documentation](https://tailscale.com/docs/)
- [CoreDNS Documentation](https://coredns.io/manual/toc/)
