#!/bin/bash

set -x
set -e

# The daemon needs to be running, otherwise we will always flush on
# each command (that closes the MFS file descriptors triggering
# the sync).
ipfs swarm addrs > /dev/null || (echo "daemon not running" && exit 1)
# FIXME: Is there a nice way to check this?

HASH=QmNzVQoBR7wQjSNXFrcJHZ29PMsEDfF6iZB1QEhKD4uZpV

# Test if $HASH is present ($1=1) or absent ($1=0) in the repo
# and fail with error string $2 if not.
test_hash() {
  local IS_PRESENT ERROR_STR IN_REPO
  IS_PRESENT=$1
  ERROR_STR=$2

  IN_REPO=1
  # Check if present or not with grep, if grep fails it will
  # leave the IN_REPO as is, otherwise it will clear it.
  ipfs refs local | grep $HASH || IN_REPO=0

  if [[ $IN_REPO != $IS_PRESENT ]]; then
    echo $ERROR_STR
    exit 1
  fi
}

# Clean previous run.
ipfs files rm -rf /cats || true
ipfs repo gc > /dev/null

test_hash 0 "hash is present before write"

# Sharness test write.
ipfs files mkdir -p /cats
echo "testing" | ipfs files write -f=false -e /cats/walrus

test_hash 0 "hash is present after write with no flush (is the daemon running?)"

ipfs files stat --hash /cats/walrus # stat flushes

test_hash 1 "hash is not present after stat"

echo "SUCCESS"
exit 0
