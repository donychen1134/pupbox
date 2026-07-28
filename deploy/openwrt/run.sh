#!/bin/sh

test -r /etc/pupbox.env || exit 1
umask 077
set -a
. /etc/pupbox.env
set +a

cd /opt/pupbox || exit 1
exec ./pupbox-server
