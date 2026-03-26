# Deployment

Production deployment using Docker Compose + Nginx on Ubuntu.

## First-time setup

```bash
# Clone repo
git clone https://github.com/semenov/nexus-zero.git /opt/nexus-zero
cd /opt/nexus-zero/deploy

# Create env file with a strong random password
echo "DB_PASSWORD=$(openssl rand -hex 32)" > .env

# Start stack
docker-compose up -d --build

# Configure Nginx
cp nginx.conf /etc/nginx/sites-available/nexus.semenov.ai
ln -s /etc/nginx/sites-available/nexus.semenov.ai /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx

# Issue SSL certificate (DNS must point to this server first)
certbot --nginx -d nexus.semenov.ai --non-interactive --agree-tos -m admin@semenov.ai
```

## Updates

```bash
cd /opt/nexus-zero
git pull
docker-compose -f deploy/docker-compose.yml up -d --build server
```

## Logs

```bash
docker-compose -f /opt/nexus-zero/deploy/docker-compose.yml logs -f server
```
