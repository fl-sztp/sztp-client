#!/bin/sh

# SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
# SPDX-License-Identifier: Apache-2.0

set -eu

role=${1:-}
if [ "$#" -gt 0 ]; then shift; fi
case "$role" in
    fixture) link_local=fe80::2 ;;
    tester) link_local=fe80::3 ;;
    *) echo "Expected fixture or tester role" >&2; exit 2 ;;
esac

accept_ra=$(cat /proc/sys/net/ipv6/conf/eth0/accept_ra)
if [ "$accept_ra" != 0 ]; then
    echo "Container isolation requires eth0 IPv6 accept_ra=0; got $accept_ra" >&2
    exit 1
fi
echo "$role eth0 IPv6 accept_ra: $accept_ra"

# Keep subnet peers and link-local multicast reachable, with no external route.
ip -4 route flush default
ip -6 route flush default
if [ -n "$(ip -4 route show default)" ] || [ -n "$(ip -6 route show default)" ]; then
    echo "Container isolation requires no IPv4 or IPv6 default route" >&2
    exit 1
fi

usable_link_local() {
    ip -6 -o addr show dev eth0 scope link |
        awk '$0 !~ /tentative|dadfailed/ { found = 1 } END { exit !found }'
}

if ! usable_link_local; then
    ip -6 addr replace "$link_local/64" dev eth0 nodad
fi
if ! usable_link_local; then
    echo "No usable IPv6 link-local address on eth0" >&2
    exit 1
fi
echo "$role eth0 addresses:"
ip -o addr show dev eth0
echo "$role routes (no default routes):"
ip -4 route show
ip -6 route show

case "$role" in
    fixture)
        exec /usr/local/bin/dhcp-fixture
        ;;
    tester)
        /usr/local/bin/client.test -test.v -test.timeout=90s
        /usr/local/bin/device.test -test.v -test.timeout=30s
        /usr/local/bin/dhcp.test -test.v -test.timeout=30s
        /usr/local/bin/serveraddr.test -test.v -test.timeout=30s
        /usr/local/bin/control.test -test.v -test.timeout=30s
        /usr/local/bin/fixture.test -test.v -test.timeout=30s
        exec /usr/local/bin/dhcp-integration.test -test.v -test.timeout=5m "$@"
        ;;
esac
