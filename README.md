# kinder

A CLI tool to stand up a local Kind Kubernetes cluster with integrated infrastructure services for development.

## Features

- **Kind Cluster** - Kubernetes IN Docker with CA trust and registry mirrors pre-configured
- **Step CA** - Certificate authority with ACME support for automatic TLS certificates
- **Zot Registry** - OCI-compliant registry with pull-through caching for major registries
- **Traefik** - Reverse proxy with automatic HTTPS via ACME
- **Gatus** - Health monitoring dashboard for all services

## Quick Start

```bash
# Start all infrastructure services
kinder start

# Create a Kind cluster (optional)
kinder kind start

# Verify everything is working
kinder diagnostics

# Get kubeconfig for kubectl
export KUBECONFIG=$(kinder kind kubeconfig)
kubectl get nodes
```

## Architecture

```
                    ┌─────────────────────────────────────────────────────┐
                    │                   Docker Network                     │
                    │                     (kind)                           │
                    │                                                      │
   Browser ─────────┼──► Traefik (:8443) ──┬──► Step CA (ACME)            │
                    │         │            ├──► Zot Registry               │
                    │         │            ├──► Gatus Dashboard            │
                    │         │            └──► Traefik Dashboard          │
                    │         │                                            │
                    │         └── ACME certs from Step CA                  │
                    │                                                      │
                    │   Kind Cluster ──────► Zot (pull-through cache)     │
                    │         │                    │                       │
                    │         └── Trusts CA cert   ├── docker.io           │
                    │                              ├── ghcr.io             │
                    │                              ├── quay.io             │
                    │                              └── registry.k8s.io    │
                    └─────────────────────────────────────────────────────┘
```

## Services

| Service | URL | Description |
|---------|-----|-------------|
| Step CA | https://ca.c0000201.sslip.io:8443 | Certificate authority with ACME |
| Zot Registry | https://registry.c0000201.sslip.io:8443 | Container registry with UI |
| Zot (direct) | http://localhost:5000 | Direct registry access |
| Gatus | https://gatus.c0000201.sslip.io:8443 | Health monitoring dashboard |
| Traefik | https://traefik.c0000201.sslip.io:8443 | Reverse proxy dashboard |

## Commands

### Core Commands

```bash
kinder start              # Start all services
kinder stop               # Stop all services
kinder restart            # Restart with updated config
kinder status             # Show service status
kinder diagnostics        # Run comprehensive health checks
kinder clean              # Remove all data (keeps CA cert)
```

### Kind Cluster

```bash
kinder kind start         # Create Kind cluster
kinder kind stop          # Delete Kind cluster
kinder kind status        # Show cluster status
kinder kind kubeconfig    # Print kubeconfig
```

### Certificate Authority

```bash
kinder ca generate        # Generate CA certificate
kinder ca print           # Display CA certificate info
```

### Configuration

```bash
kinder config show        # Display current config as YAML
kinder config path        # Show config file location
kinder config init        # Create default config file
```

### ArgoCD (Optional)

```bash
kinder argocd show        # Show ArgoCD install manifest
kinder argocd initial-app # Generate bootstrap Application
```

## Configuration

Configuration uses Viper with the following precedence (highest to lowest):
1. CLI flags
2. Environment variables (`KINDER_*`)
3. Config file (`~/.config/kinder/config.yaml`)
4. Built-in defaults

### Example Config File

```yaml
appName: kinder
domain: c0000201.sslip.io
network:
  name: kind
  cidr: 172.28.28.0/24
  bridge: kindbr0
traefik:
  port: "8443"
argocd:
  version: v3.1.10
images:
  stepca: smallstep/step-ca:latest
  zot: ghcr.io/project-zot/zot-linux-amd64:latest
  gatus: twinproduction/gatus:latest
  traefik: traefik:latest
registryMirrors:
  - ghcr.io
  - registry-1.docker.io
  - quay.io
  - registry.k8s.io
```

### Environment Variables

```bash
KINDER_DATADIR=/path/to/data    # Data directory
KINDER_DOMAIN=example.sslip.io  # Base domain
KINDER_TRAEFIK_PORT=9443        # HTTPS port
KINDER_ARGOCD_VERSION=v3.2.3    # ArgoCD version
```

## Browser Certificate Trust

To access services without security warnings, import the CA certificate:

1. Locate: `~/.local/share/kinder/ca.crt`
2. Import into your browser's certificate authorities:
   - **Firefox**: Settings > Privacy & Security > Certificates > View Certificates > Authorities > Import
   - **Chrome/Edge**: Settings > Privacy and security > Security > Manage certificates > Authorities > Import
   - **Safari**: Open file, add to Keychain, mark as trusted for SSL

## Requirements

- Docker (Docker CE or Docker Desktop)
- Go 1.21+ (for building from source)
- `kind` CLI (for Kind cluster management)
- `kubectl` (for Kubernetes interaction)
- `skopeo` (optional, for registry diagnostics)

## Building from Source

```bash
git clone https://codeberg.org/hipkoi/kinder.git
cd kinder
go build -o kinder .
```

## Data Storage

| Type | Location |
|------|----------|
| Data (CA certs, configs) | `$XDG_DATA_HOME/kinder/` or `~/.local/share/kinder/` |
| Config file | `$XDG_CONFIG_HOME/kinder/config.yaml` or `~/.config/kinder/config.yaml` |

## Network Configuration

The default network uses CIDR `172.28.28.0/24`:
- `172.28.28.0/25` - Container DHCP range
- `172.28.28.128-255` - Reserved for MetalLB (Kind cluster)

## License

MIT
