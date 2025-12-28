#!/bin/bash
# Manual endpoint testing script for kinder services
# This script verifies kinder service endpoints are reachable

set -e

echo "🧪 Testing Kinder Service Endpoints"
echo "===================================="
echo

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}Services on Localhost:${NC}"
echo

# Test Step CA (HTTPS on port 9000)
echo -n "Step CA (https://localhost:9000/health)... "
if curl -k -s -f --max-time 5 https://localhost:9000/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Reachable${NC}"
else
    echo -e "${RED}✗ Not reachable (firewall issue)${NC}"
fi

# Test Zot Registry (HTTP on port 5000)
echo -n "Zot Registry (http://localhost:5000/v2/)... "
if curl -s -f --max-time 5 http://localhost:5000/v2/ > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Reachable${NC}"
else
    echo -e "${RED}✗ Not reachable (firewall issue)${NC}"
fi

echo
echo -e "${BLUE}Services on Docker Bridge (172.28.28.1):${NC}"
echo

# Test Gatus
echo -n "Gatus (http://172.28.28.1:8080)... "
if curl -s -f --max-time 5 http://172.28.28.1:8080/ > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Reachable${NC}"
else
    echo -e "${RED}✗ Not reachable${NC}"
fi

# Test Traefik
echo -n "Traefik (http://172.28.28.1:8081/dashboard/)... "
if curl -s -L --max-time 5 http://172.28.28.1:8081/dashboard/ 2>&1 | grep -q "Traefik\|dashboard" 2>/dev/null; then
    echo -e "${GREEN}✓ Reachable${NC}"
else
    echo -e "${RED}✗ Not reachable${NC}"
fi

# Test CoreDNS
echo -n "CoreDNS (172.28.28.1:53)... "
if timeout 2 bash -c "cat < /dev/null > /dev/tcp/172.28.28.1/53" 2>/dev/null; then
    echo -e "${GREEN}✓ Reachable${NC}"
else
    echo -e "${RED}✗ Not reachable${NC}"
fi

echo
echo "===================================="
echo "✅ Endpoint testing complete!"
