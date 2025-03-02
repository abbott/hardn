#!/bin/bash
set -e

# Set appropriate permissions for config files
chmod 644 /etc/hardn/hardn.yml
chmod 644 /etc/hardn/hardn.yml.example

# Set ownership
chown root:root /etc/hardn/hardn.yml
chown root:root /etc/hardn/hardn.yml.example

# Create backup of original config if this is an upgrade
if [ "$1" = "upgrade" ] || [ "$1" = "2" ]; then
    if [ -f /etc/hardn/hardn.yml.bak ]; then
        # Append timestamp to avoid overwriting existing backups
        cp /etc/hardn/hardn.yml "/etc/hardn/hardn.yml.bak.$(date +%Y%m%d%H%M%S)"
    else
        cp /etc/hardn/hardn.yml /etc/hardn/hardn.yml.bak
    fi
    echo "Backup of previous configuration created at /etc/hardn/hardn.yml.bak"
fi

echo "Hardn configuration installed at /etc/hardn/hardn.yml"
echo "Example configuration with all options is available at /etc/hardn/hardn.yml.example"