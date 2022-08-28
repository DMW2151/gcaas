#! /bin/sh

# install docker and docker-compose
sudo apt-get install -y docker.io docker-compose

# install doctl - digital ocean command line utility
sudo wget https://github.com/digitalocean/doctl/releases/download/v1.79.0/doctl-1.79.0-linux-amd64.tar.gz
sudo tar xf doctl-1.79.0-linux-amd64.tar.gz
sudo mv doctl /usr/local/bin

# start the geocoder services by pulling from the repo
# NOTE/TODO :: this via scp on gh action, good enough for now -> easier than configuring 
# ssh from tf...
sudo wget https://raw.githubusercontent.com/DMW2151/gcaas/main/deploy-prod/docker-compose.yml 
