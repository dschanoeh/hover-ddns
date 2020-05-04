# hover-ddns

[![Travis (master)](https://travis-ci.com/dschanoeh/hover-ddns.svg?branch=master)](https://travis-ci.com/dschanoeh/hover-ddns)

hover-ddns is a DDNS client that will update a DNS A and/or AAAA record at hover with the current public IP address(es) of the machine.

This is an unofficial client using the non-supported Hover API.

It will:

1. Determine the current public IP address of the host machine
2. Authenticate against the Hover API
3. Create or update the DNS entry for the specified hostname

It doesn't do anything beyond that and if you need more features or different services, I suggest to look at tools like [lexicon](https://github.com/AnalogJ/lexicon).

## Features

* IPv4 and IPv6 supported
* Supports public IP lookup by:
  * Using the ipify API
  * Issuing OpenDNS DNS queries
  * Extracting the address from a lokal network interface
* Cron syntax can be used to schedule periodic updates

## Usage

Create a config file with your credentials and domain info (see the provided example.yaml) and then run hover-ddns:

    $ hover-ddns --config config.yaml

## Installation

This is an example setup on Linux using the provided systemd service and timer.

Download the latest release from https://github.com/dschanoeh/hover-ddns/releases.

    $ tar xvf [downloaded_archive.tar.gz]
    $ sudo mv hover-ddns /usr/local/bin
    $ vim example.yaml

    [Add your credentials and domain info]

    $ sudo mv example.yaml /etc/hover-ddns.yaml

    # Make sure only root can read the file with sensitive information
    $ sudo chown root:root /etc/hover-ddns.yaml
    $ sudo chmod 600 /etc/hover-ddns.yaml

    $ sudo mv hover-ddns.service /etc/systemd/system/
    $ sudo systemctl daemon-reload
    $ sudo systemctl enable hover-ddns.service
    $ sudo systemctl start hover-ddns.service
