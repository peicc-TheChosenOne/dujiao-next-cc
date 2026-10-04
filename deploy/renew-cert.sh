#!/bin/bash
set -euo pipefail
base=/opt/dujiao-next
source "$base/.env"

docker run --rm --memory 256m \
  -v "$base/tls/letsencrypt:/etc/letsencrypt" \
  -v "$base/tls/work:/var/lib/letsencrypt" \
  -v "$base/tls/logs:/var/log/letsencrypt" \
  -v /www/wwwroot/aiccpay.com:/var/www/acme \
  "${CERTBOT_IMAGE:?Set CERTBOT_IMAGE in .env}" renew --quiet
/www/server/nginx/sbin/nginx -t
/www/server/nginx/sbin/nginx -s reload
