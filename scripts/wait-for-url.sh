#!/bin/sh
# Waits until a URL answers successfully (up to 60 seconds). Usage: wait-for-url.sh <url>
url=$1
tries=0
while [ "$tries" -lt 60 ]; do
  if curl -fsS "$url" >/dev/null 2>&1; then
    exit 0
  fi
  tries=$((tries + 1))
  sleep 1
done
echo "$url not reachable" >&2
exit 1
