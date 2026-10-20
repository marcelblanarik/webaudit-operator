FROM node:22-bookworm-slim

# Exact Debian chromium build; security updates age out of the archive, bump here when it does.
ARG CHROMIUM_VERSION=155.0.8059.79-1~deb12u1
ARG LIGHTHOUSE_VERSION=13.5.0

RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium=${CHROMIUM_VERSION} fonts-liberation \
 && apt-get clean && rm -r /var/lib/apt/lists/* \
 && npm install -g lighthouse@${LIGHTHOUSE_VERSION} \
 && npm cache clean --force

COPY entrypoint.mjs extract.mjs /app/

LABEL org.opencontainers.image.source="https://github.com/marcelblanarik/webaudit-operator" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.description="Lighthouse audit image for webaudit-operator: runs one audit and writes LCP, TBT and CLS to the termination message"

# uid/gid 1000 is the built-in "node" user. At runtime the root fs is read-only and /tmp is the only writable path.
USER 1000:1000
ENV HOME=/tmp CHROME_PATH=/usr/bin/chromium
ENTRYPOINT ["node", "/app/entrypoint.mjs"]
