hover-ddns
==========

[![Travis (master)](https://travis-ci.com/dschanoeh/hover-ddns.svg?branch=master)](https://travis-ci.com/dschanoeh/hover-ddns)

hover-ddns is a DDNS client that will update a DNS A record at hover with the current public IP address of the machine.

This is an unofficial client using the non-supported Hover API.

It will:

1. Determine the current public IP address of the host machine
2. Authenticate against the Hover API
3. Create or update the DNS entry for the specified hostname

It doesn't do anything beyond that and if you need more features or different services, I suggest to look at tools like [lexicon](https://github.com/AnalogJ/lexicon).

Usage
-----
Create a config file with your credentials and domain info (see the provided example.yaml) and then run hover-ddns:

    $ hover-ddns --config config.yaml


