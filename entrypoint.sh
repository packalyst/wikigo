#!/bin/sh
set -e

# Create directories if they don't exist and fix permissions
for dir in /app/data /app/uploads /app/backups; do
    mkdir -p "$dir"
    chown -R wiki:wiki "$dir"
done

# Drop to wiki user and exec the application
exec su-exec wiki ./gowiki "$@"
