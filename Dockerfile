FROM golang:1.23-alpine AS go-builder

RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY config/ ./config/
COPY scripts/ ./scripts/
COPY test/ ./test/

ARG BUILD_TARGET=server
ARG VERSION=dev
ARG COMMIT_SHA=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.commit=${COMMIT_SHA}" \
    -o /app/bin/app \
    ./cmd/${BUILD_TARGET}

FROM node:20-alpine AS node-builder

WORKDIR /app
COPY package.json package-lock.json ./

RUN npm ci --only=production

COPY client/ ./client/
COPY shared/ ./shared/
COPY vite.config.ts ./
COPY tailwind.config.ts ./
COPY postcss.config.js ./
COPY tsconfig.json ./
COPY components.json ./

RUN npm run Build

FROM python:3.11-slim AS python-deps
WORKDIR /app
RUN pip install --no-cache-dir fastapi uvicorn sentence-transformers torch transformers

FROM python-deps AS embedding-service

COPY scripts/embedding-server.py ./server.py

RUN mkdir -p /app/models
WORKDIR /app

EXPOSE 8080
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8080"]

FROM alpine:3.18 AS runtime-base
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    bash

RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup
RUN mkdir -p /app/config /app/logs /app/models /app/cache && \
    chown -R appuser:appgroup /app
ENV TZ=UTC

FROM runtime-base AS server

COPY --from=go-builder /app/bin/app /app/server
COPY --chown=appuser:appgroup config/ /app/config/
RUN chmod +x /app/server

USER appuser
WORKDIR /app

HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8000/api/health || exit 1
EXPOSE 8000

ENV SERVER_HOST=0.0.0.0
ENV SERVER_PORT=8000
ENV LOG_LEVEL=info

CMD ["/app/server"]

FROM runtime-base AS worker

COPY --from=go-builder /app/bin/app /app/worker
COPY --chown=appuser:appgroup config/ /app/config/
RUN chmod +x /app/worker

USER appuser
WORKDIR /app

HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD /app/worker --health-check || exit 1

ENV LOG_LEVEL=info
ENV WORKER_CONCURRENCY=4

CMD ["/app/worker"]

FROM runtime-base AS cli

COPY --from=go-builder /app/bin/app /app/semantic-search-engine
COPY --chown=appuser:appgroup config/ /app/config/
RUN chmod +x /app/semantic-search-engine

USER appuser
WORKDIR /app

ENV PATH="/app:${PATH}"

CMD ["/app/semantic-search-engine", "--help"]

FROM nginx:alpine AS frontend

COPY --from=node-builder /app/dist /usr/share/nginx/html
COPY <<EOF /etc/nginx/conf.d/default.conf
server {
    listen 5000;
    server_name localhost;
    root /usr/share/nginx/html;
    index index.html;

    # Enable gzip compression
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types
        text/plain
        text/css
        text/xml
        text/javascript
        application/javascript
        application/xml+rss
        application/json;

    # Handle client-side routing
    location / {
        try_files \$uri \$uri/ /index.html;
    }

    # API proxy
    location /api/ {
        proxy_pass http://server:8000;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    # WebSocket proxy
    location /ws {
        proxy_pass http://server:8000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "no-referrer-when-downgrade" always;
    add_header Content-Security-Policy "default-src 'self' http: https: data: blob: 'unsafe-inline'" always;

    # Cache static assets
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
    }
}
EOF

HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD curl -f http://localhost:5000 || exit 1
EXPOSE 5000

FROM runtime-base AS development

RUN apk add --no-cache \
    git \
    make \
    nodejs \
    npm \
    postgresql-client \
    redis

COPY --from=go-builder /app/bin/app /app/server
COPY --from=go-builder /app/bin/app /app/worker
COPY --from=go-builder /app/bin/app /app/cli
COPY --chown=appuser:appgroup . /app/src/
COPY --chown=appuser:appgroup config/ /app/config/

RUN chmod +x /app/server /app/worker /app/cli

WORKDIR /app/src
RUN npm ci

USER appuser
WORKDIR /app

ENV PATH="/app:${PATH}"
EXPOSE 8000 5000
CMD ["/app/server"]

FROM scratch AS production

COPY --from=runtime-base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=runtime-base /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=go-builder /app/bin/app /app

USER 1001
EXPOSE 8000

ENTRYPOINT ["/app"]

ARG BUILD_DATE
ARG VERSION=dev
ARG COMMIT_SHA=unknown

LABEL org.opencontainers.image.title="Semantic Search Engine"
LABEL org.opencontainers.image.description="Intelligent code analysis platform with semantic search and knowledge graphs"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${COMMIT_SHA}"
LABEL org.opencontainers.image.vendor="Semantic Search Engine"
LABEL org.opencontainers.image.source="https://github.com/MdSadiqMd/Semantic-Search-Engine"
LABEL org.opencontainers.image.documentation="https://github.com/MdSadiqMd/Semantic-Search-Engine/blob/main/README.md"
LABEL org.opencontainers.image.licenses="BSD 3-Clause License"

# Build API server:
# docker build --target server -t semantic-search-engine-api .

# Build worker:
# docker build --target worker --build-arg BUILD_TARGET=worker -t semantic-search-engine-worker .

# Build CLI:
# docker build --target cli --build-arg BUILD_TARGET=cli -t semantic-search-engine-cli .

# Build frontend:
# docker build --target frontend -t semantic-search-engine-frontend .

# Build for development:
# docker build --target development -t semantic-search-engine-dev .

# Build production (smallest):
# docker build --target production --build-arg BUILD_TARGET=server -t semantic-search-engine-prod .
