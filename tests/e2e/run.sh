#!/usr/bin/env bash
#
# End-to-end checks for the caddy-minified image built by ./build.sh.
#
# The harness never builds anything. It runs the image with different
# Caddyfiles and document roots mounted into it, then compares the bytes
# served over HTTP with the fixtures in tests/e2e/fixtures/expected.

set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
FIXTURES="$ROOT/tests/e2e/fixtures"
IMAGE=${IMAGE:-caddy-minified:test}
NAME="caddy-minified-e2e-$$"
STARTED=0
TMP=$(mktemp -d)

cleanup() {
	stop
	rm -rf "$TMP"
}

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

stop() {
	if [ "$STARTED" -eq 1 ]; then
		docker rm -f "$NAME" >/dev/null 2>&1 || true
		STARTED=0
	fi
}

trap cleanup EXIT INT TERM

command -v docker >/dev/null 2>&1 || fail "docker is required"
docker image inspect "$IMAGE" >/dev/null 2>&1 ||
	fail "image $IMAGE not found; run ./build.sh first"

start() {
	local caddyfile=$1
	local tries=0
	stop
	docker run --rm -d --name "$NAME" \
		-v "$FIXTURES/$caddyfile:/etc/caddy/Caddyfile:ro" \
		-v "$FIXTURES/www:/srv/www:ro" \
		"$IMAGE" >/dev/null
	STARTED=1

	until docker exec "$NAME" /bin/toybox wget -O /tmp/e2e-ready \
		http://127.0.0.1:8080/ >/dev/null 2>&1; do
		tries=$((tries + 1))
		if [ "$tries" -ge 30 ]; then
			docker logs "$NAME" >&2 || true
			fail "caddy did not become ready with $caddyfile"
		fi
		sleep 1
	done
}

fetch() {
	local path=$1
	docker exec "$NAME" /bin/toybox wget -O /tmp/e2e-body \
		"http://127.0.0.1:8080${path}" >/dev/null 2>&1 || return 1
	docker exec "$NAME" /bin/toybox cat /tmp/e2e-body
}

assert_file() {
	local path=$1
	local expected=$2
	fetch "$path" >"$TMP/actual" || fail "could not fetch $path"
	if ! cmp -s "$expected" "$TMP/actual"; then
		echo "FAIL: $path does not match $expected" >&2
		diff -u "$expected" "$TMP/actual" >&2 || true
		exit 1
	fi
	echo "ok: $path"
}

echo "== global options and route toggles =="
start Caddyfile.global
assert_file / "$FIXTURES/expected/global-index.html"
assert_file /style.css "$FIXTURES/expected/global-style.css"
assert_file /icon.svg "$FIXTURES/expected/global-icon.svg"
assert_file /app.js "$FIXTURES/www/app.js"
assert_file /data.json "$FIXTURES/expected/global-data.json"
assert_file /data.xml "$FIXTURES/expected/global-data.xml"
assert_file /feed.rss "$FIXTURES/expected/global-feed.rss"
assert_file /plain.txt "$FIXTURES/www/plain.txt"
assert_file /admin/ "$FIXTURES/www/admin/index.html"
assert_file /raw/ "$FIXTURES/www/raw/index.html"
assert_file /nested/ "$FIXTURES/www/nested/index.html"

echo "== default formats =="
start Caddyfile.default
assert_file / "$FIXTURES/expected/default-index.html"
assert_file /style.css "$FIXTURES/expected/default-style.css"
assert_file /app.js "$FIXTURES/expected/default-app.js"
assert_file /data.json "$FIXTURES/www/data.json"
assert_file /data.xml "$FIXTURES/www/data.xml"
assert_file /feed.rss "$FIXTURES/www/feed.rss"
assert_file /plain.txt "$FIXTURES/www/plain.txt"

echo "== buffer limit =="
start Caddyfile.limit
assert_file /large.html "$FIXTURES/www/large.html"

echo "all e2e checks passed"