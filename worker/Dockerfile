FROM node:20-slim

# Install system dependencies for Playwright Chromium
RUN apt-get update && apt-get install -y \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy package files first for better layer caching
COPY package.json ./

# Install Node dependencies
RUN npm install --omit=dev

# Install Playwright Chromium with its system dependencies
RUN npx playwright install --with-deps chromium

# Copy application source
COPY . .

EXPOSE 3001

CMD ["node", "index.mjs"]

# Notes for docker-compose:
# - Add shm_size: '1gb' to the worker service in docker-compose.yml
#   to prevent Chromium OOM crashes in containerized environments
# - The --no-sandbox flag is passed in shared/generate-pdf.mjs
# - Example compose service config:
#   worker:
#     build: ./worker
#     shm_size: '1gb'
#     environment:
#       - DATABASE_URL
#       - ANTHROPIC_API_KEY
#       - R2_ENDPOINT
#       - R2_ACCESS_KEY_ID
#       - R2_SECRET_ACCESS_KEY
#       - R2_BUCKET
