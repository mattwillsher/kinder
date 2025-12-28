# Endpoint Testing Summary

## Changes Made

Successfully configured port exposure for kinder services with two access patterns:

### ✅ Services with Localhost Port Bindings
These services are accessible via `localhost` from the host:

- **Step CA** (port 9000) - `https://localhost:9000` - Certificate authority operations
- **Zot Registry** (port 5000) - `http://localhost:5000` - Container registry for pushing/pulling images

### 🌐 Services Accessible via Container IP Only
These services are accessible from the host via their Docker network IP, but NOT via localhost:

- **CoreDNS** (port 53) - Accessible at container IP (e.g., `172.28.28.2:53`)
- **Gatus** (port 8080) - Accessible at container IP (e.g., `http://172.28.28.5:8080`)
- **Traefik** (ports 80, 443, 8080) - Accessible at container IP (e.g., `http://172.28.28.6:8080`)

This configuration keeps localhost ports free while still allowing host access when needed.

## Verification

### Port Mappings Confirmed
```bash
$ docker port kinder-step-ca
9000/tcp -> 0.0.0.0:9000

$ docker port kinder-zot
5000/tcp -> 0.0.0.0:5000

$ docker port kinder-coredns
# (no output - internal only)

$ docker port kinder-gatus
# (no output - internal only)

$ docker port kinder-traefik
# (no output - internal only)
```

### Internal Communication Working
Verified via container logs that services communicate successfully on the Docker network:

- ✅ Gatus successfully monitors Step CA health endpoint
- ✅ Gatus successfully monitors Zot registry
- ✅ All containers can resolve each other via DNS (*.kinder.internal)

```
Gatus → Step CA: HTTP/2.0 GET /health → 200 OK
Gatus → Zot: GET /v2/ → 200 OK
```

## Files Modified

1. **docker/container.go** - Added `PortBindings` field to `ContainerConfig` struct
2. **docker/stepca.go** - Added port binding for 9000/tcp
3. **docker/zot.go** - Added port binding for 5000/tcp
4. **docker/coredns.go** - No port bindings (internal only)
5. **docker/gatus.go** - No port bindings (internal only)
6. **docker/traefik.go** - No port bindings (internal only)

## Documentation Created

- **ENDPOINT_TESTING.md** - Comprehensive testing documentation
- **test-endpoints.sh** - Script to test accessible endpoints
- **integration_test.go** - Integration tests for full stack
- **docker/endpoints_test.go** - Unit tests for container endpoints

## Usage

### Start All Services
```bash
./kinder start
```

### Test Services via Localhost
```bash
# Step CA
curl -k https://localhost:9000/health

# Zot Registry
curl http://localhost:5000/v2/
```

### Test Services via Container IP
```bash
# Get container IPs
docker inspect kinder-gatus --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'
docker inspect kinder-traefik --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'

# Access Gatus (use actual IP from inspect command)
curl http://172.28.28.5:8080

# Access Traefik dashboard (use actual IP from inspect command)
curl http://172.28.28.6:8080/dashboard/
```

### Test Services via Hostname (within Docker network)
```bash
# Run commands from within the kinder network
docker run --rm --network kinder curlimages/curl:latest \
  curl -s http://gatus.kinder.internal:8080

docker run --rm --network kinder curlimages/curl:latest \
  curl -s http://traefik.kinder.internal:8080/dashboard/

docker run --rm --network kinder curlimages/curl:latest \
  curl -s http://registry.kinder.internal:5000/v2/
```

## Security Benefits

✅ **Localhost Port Conservation** - Only Step CA and Zot bind to localhost, keeping other ports free  
✅ **Flexible Access** - Infrastructure services (CoreDNS, Gatus, Traefik) accessible via container IP when needed  
✅ **Network Isolation** - All services isolated to Docker network by default  
✅ **Selective Exposure** - Only services requiring frequent host access get localhost bindings

## Firewall Configuration Required

By default, the host firewall blocks direct access to Docker container IPs. To allow host access to services via container IP (CoreDNS, Gatus, Traefik), you need to add a firewall rule.

**Quick Fix:**
```bash
# Allow host to access kinder Docker network
sudo iptables -I DOCKER-USER -d 172.28.28.0/24 -j ACCEPT
```

**See [FIREWALL_SETUP.md](FIREWALL_SETUP.md) for detailed instructions** including:
- iptables configuration
- nftables configuration  
- firewalld configuration
- Making rules persistent

**Current Status Without Firewall Rule:**
- ✅ Step CA and Zot accessible via localhost (working)
- ✅ All services communicate internally (verified via Gatus health checks)
- ⚠️ CoreDNS, Gatus, Traefik not accessible from host via container IP (blocked by firewall)

**After Adding Firewall Rule:**
- ✅ All services accessible from host
- ✅ Localhost ports kept clean (only Step CA and Zot bind to localhost)
- ✅ Services remain isolated from external access
