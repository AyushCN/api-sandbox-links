# Caddy with the Cloudflare DNS plugin, required to issue wildcard
# certificates for *.SANDBOX_BASE_DOMAIN. Rebuild when changing providers.
FROM caddy:2-builder AS build
RUN xcaddy build --with github.com/caddy-dns/cloudflare

FROM caddy:2-alpine
COPY --from=build /usr/bin/caddy /usr/bin/caddy
