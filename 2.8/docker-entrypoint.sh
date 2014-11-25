#!/bin/bash
set -e

if [ "$1" = 'crowd' ]; then
  chown -R atlassian "$CROWD_HOME"
  umask 0027
  exec gosu atlassian /opt/atlassian/apache-tomcat/bin/catalina.sh run
fi

exec "$@"