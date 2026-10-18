#!/bin/sh
# Starts the service on the bundled fonts and checks that a PDF comes back.
# usage: smoke/smoke.sh   (run from java-service/ after mvn package)
set -eu
cd "$(dirname "$0")/.."
fonts=$(mktemp -d)
cp ../internal/font/data/LiberationSans-*.ttf "$fonts"/
TEMPLATES_DIR=smoke/templates FONTS_DIR="$fonts" PORT=18099 java -Djava.awt.headless=true -jar target/report-service.jar &
pid=$!
trap 'kill $pid; rm -rf "$fonts"' EXIT
for i in $(seq 1 30); do curl -sf localhost:18099/health >/dev/null && break; sleep 1; done
xml='<Wrap><Root><Item name="one"/><Item name="two"/></Root></Wrap>'
code=$(curl -s -o "$fonts/out.pdf" -w '%{http_code}' -X POST 'localhost:18099/pdf?template=hello.xslt&root=Root' -d "$xml")
[ "$code" = 200 ] && head -c 5 "$fonts/out.pdf" | grep -q '%PDF-' || { echo "FAIL: $code"; exit 1; }
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST 'localhost:18099/pdf?template=../x' -d '<a/>')" = 400 ] || { echo "FAIL: traversal"; exit 1; }
echo "smoke ok ($(wc -c < "$fonts/out.pdf") bytes)"
