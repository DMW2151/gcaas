#! /bin/sh

apt-get install -y docker.io docker-compose
wget https://github.com/digitalocean/doctl/releases/download/v1.79.0/doctl-1.79.0-linux-amd64.tar.gz
tar xf doctl-1.79.0-linux-amd64.tar.gz
mv doctl /usr/local/bin