#!/usr/bin/env bash
set -e

# Terminal colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}==> 1. Running Go tests...${NC}"
if go test ./...; then
    echo -e "${GREEN}✓ Tests passed.${NC}\n"
else
    echo -e "${RED}✗ Tests failed! Aborting deployment.${NC}"
    exit 1
fi

echo -e "${YELLOW}==> 2. Building server binary...${NC}"
mkdir -p bin
if go build -o bin/server main.go; then
    echo -e "${GREEN}✓ Build completed: bin/server${NC}\n"
else
    echo -e "${RED}✗ Build failed! Aborting deployment.${NC}"
    exit 1
fi

echo -e "${YELLOW}==> 3. Staging changes for Git...${NC}"
git add .

COMMIT_MSG="$1"

if [ -z "$COMMIT_MSG" ]; then
    if [ -t 0 ]; then
        read -r -p "Enter commit message: " COMMIT_MSG
    fi
fi

if [ -z "$COMMIT_MSG" ]; then
    COMMIT_MSG="deploy: update build and deploy $(date +'%Y-%m-%d %H:%M:%S')"
fi

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

if git diff --staged --quiet; then
    echo -e "${YELLOW}No staged changes to commit. Pushing branch '$CURRENT_BRANCH'...${NC}"
else
    echo -e "${YELLOW}==> 4. Committing changes: \"$COMMIT_MSG\"...${NC}"
    git commit -m "$COMMIT_MSG"
fi

echo -e "${YELLOW}==> 5. Pushing to remote ($CURRENT_BRANCH)...${NC}"
git push origin "$CURRENT_BRANCH"

echo -e "\n${GREEN}=========================================${NC}"
echo -e "${GREEN}✓ Deployment script finished successfully!${NC}"
echo -e "${GREEN}=========================================${NC}"
