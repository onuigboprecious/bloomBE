#!/usr/bin/env bash
set -e

# Terminal colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}=========================================${NC}"
echo -e "${GREEN} InterServer VPS Initial Setup & Deploy  ${NC}"
echo -e "${GREEN}=========================================${NC}"

# 1. Check for root privileges
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Please run as root or with sudo.${NC}"
    exit 1
fi

# 2. Update System & Install Docker
echo -e "\n${YELLOW}==> 1. Updating system & installing Docker...${NC}"
apt-get update -y
apt-get install -y ca-certificates curl gnupg lsb-release ufw git

if ! command -v docker &> /dev/null; then
    echo -e "${YELLOW}Installing Docker...${NC}"
    curl -fsSL https://get.docker.com | sh
    systemctl enable docker
    systemctl start docker
    echo -e "${GREEN}✓ Docker installed.${NC}"
else
    echo -e "${GREEN}✓ Docker is already installed.${NC}"
fi

# 3. Configure Firewall (UFW)
echo -e "\n${YELLOW}==> 2. Configuring firewall rules (SSH: 22, HTTP: 80, API: 8080)...${NC}"
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 8080/tcp
ufw allow 443/tcp
ufw --force enable
echo -e "${GREEN}✓ Firewall configured.${NC}"

# 4. Check for .env file
if [ ! -f .env ]; then
    echo -e "\n${YELLOW}==> 3. Creating .env from .env.example...${NC}"
    if [ -f .env.example ]; then
        cp .env.example .env
        echo -e "${RED}ATTENTION: Created .env file. Please edit it with your real credentials!${NC}"
    else
        echo -e "${RED}Error: .env file missing and .env.example not found.${NC}"
        exit 1
    fi
fi

# 5. Start Production Stack
echo -e "\n${YELLOW}==> 4. Building and starting Docker containers...${NC}"
docker compose -f docker-compose.prod.yml up -d --build

echo -e "\n${GREEN}=========================================${NC}"
echo -e "${GREEN}✓ Deployment completed successfully!${NC}"
echo -e "${GREEN}=========================================${NC}"
echo -e "Check status: ${YELLOW}docker compose -f docker-compose.prod.yml ps${NC}"
echo -e "Check logs:   ${YELLOW}docker compose -f docker-compose.prod.yml logs -f${NC}"
