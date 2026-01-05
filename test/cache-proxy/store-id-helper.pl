#!/usr/bin/perl
# SPDX-License-Identifier: Apache-2.0
# Store ID helper for Squid - normalizes URLs by stripping query parameters
# This allows caching of GitHub release assets and container registry blobs
# that have authentication tokens or signatures in the URL

use strict;
use warnings;

$|=1;  # Unbuffered output

# Debug log file
open(my $log, '>>', '/var/log/squid/store-id-debug.log') or die "Cannot open log: $!";
$log->autoflush(1);
print $log "=== Store ID Helper Started at " . localtime() . " ===\n";

while (<>) {
    chomp;
    print $log "INPUT: $_\n";

    # Squid's store_id protocol sends just: URL [kv-pairs]
    # The first field IS the URL (not a channel ID)
    my @fields = split(/\s+/);

    # Need at least the URL
    if (@fields < 1) {
        print $log "ERROR: No URL in input\n";
        print "$_\n";
        next;
    }

    # First field is the URL itself
    my $url = $fields[0];

    print $log "URL: $url\n";

    my $new_url = $url;

    # Strip query params from GitHub release assets
    if ($url =~ m{^(https?://release-assets\.githubusercontent\.com/[^?]+)}) {
        $new_url = $1;
        print $log "MATCHED release-assets, normalized to: $new_url\n";
    }
    # Strip query params from pkg-containers (GitHub Container Registry)
    elsif ($url =~ m{^(https?://pkg-containers\.githubusercontent\.com/[^?]+)}) {
        $new_url = $1;
        print $log "MATCHED pkg-containers, normalized to: $new_url\n";
    }
    else {
        print $log "NO MATCH for URL\n";
    }

    # Output format: just the result
    # Either: OK store-id=URL (rewritten) or OK (pass-through)
    my $output;
    if ($new_url ne $url) {
        # URL was rewritten - return normalized version
        $output = "OK store-id=$new_url\n";
        print $log "OUTPUT (rewritten): $output";
    } else {
        # No rewrite needed - return OK without store-id
        $output = "OK\n";
        print $log "OUTPUT (passthrough): $output";
    }
    print $output;
}

close $log;
