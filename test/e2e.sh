#!/bin/sh
# e2e test runner — runs the built image in Docker, exercises every fixture,
# and compares the bytes served over HTTP with the expected fixtures.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
FIXTURES="$ROOT/test/fixtures"
TMP=$(mktemp -d /tmp/caddy-minified-e2e-XXXXXX)
PORT=9099
SLUG=$(sed -n '1s/^[^:]*: //p' "$ROOT/NOTICE")
IMAGE="${SLUG}:test"
CONTAINER_NAME="caddy-minified-e2e-runner"
PASS=0
FAIL=0
STARTED=0

cleanup() {
	docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup EXIT

die() {
	echo "FAIL: $*" >&2
	exit 1
}

pass() {
	echo "  PASS: $1"
	PASS=$((PASS + 1))
}

fail() {
	echo "  FAIL: $1"
	shift
	for line in "$@"; do
		echo "    $line"
	done
	FAIL=$((FAIL + 1))
}

equal() {
	if [ "$2" = "$3" ]; then
		pass "$1"
	else
		fail "$1" "expected: $2" "got:      $3"
	fi
}

contains() {
	if echo "$3" | grep -q -- "$2"; then
		pass "$1"
	else
		fail "$1" "expected to contain: $2" "got: $3"
	fi
}

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v curl >/dev/null 2>&1 || die "curl is required"
docker image inspect "$IMAGE" >/dev/null 2>&1 ||
	die "image $IMAGE not found; run ./build.sh first"

stop() {
	if [ "$STARTED" -eq 1 ]; then
		docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
		STARTED=0
	fi
}

start() {
	caddyfile=$1
	proxy_root=${2:-}
	stop
	volumes="-v $FIXTURES/www:/srv/www:ro"
	if [ -n "$proxy_root" ]; then
		volumes="$volumes -v $proxy_root:/srv/proxy:ro"
	fi
	docker run -d --rm --name "$CONTAINER_NAME" \
		-v "$FIXTURES/$caddyfile:/etc/caddy/Caddyfile:ro" \
		$volumes \
		-p "$PORT:8080" \
		"$IMAGE" run --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
	STARTED=1

	i=0
	while ! curl -sf -o /dev/null "http://127.0.0.1:$PORT/"; do
		i=$((i + 1))
		if [ "$i" -ge 30 ]; then
			docker logs "$CONTAINER_NAME" >&2 || true
			die "caddy did not become ready with $caddyfile"
		fi
		sleep 1
	done
	echo "Caddy ready with $caddyfile after ${i}s"
}

request() {
	: >"$TMP/body"
	curl -sS -D "$TMP/head" -o "$TMP/body" "$@"
}

status() {
	head -n 1 "$TMP/head" | cut -d' ' -f2 | tr -d '\r'
}

header() {
	grep -i "^$1:" "$TMP/head" | head -n 1 | cut -d' ' -f2- | tr -d '\r' || true
}

body_size() {
	wc -c <"$TMP/body" | tr -d ' '
}

same() {
	desc=$1
	path=$2
	want=$3
	if ! curl -sf -o "$TMP/actual" "http://127.0.0.1:$PORT$path"; then
		fail "$desc" "could not fetch $path"
		return 0
	fi
	if cmp -s "$want" "$TMP/actual"; then
		pass "$desc"
	else
		fail "$desc" "expected: $want" "got:      $TMP/actual"
		diff -u "$want" "$TMP/actual" >&2 || true
	fi
}

file_same() {
	desc=$1
	want=$2
	got=$3
	if cmp -s "$want" "$got"; then
		pass "$desc"
	else
		fail "$desc" "expected: $want" "got:      $got"
		diff -u "$want" "$got" >&2 || true
	fi
}

body_same() {
	file_same "$1" "$2" "$TMP/body"
}

smaller() {
	desc=$1
	small=$2
	large=$3
	if [ "$small" -lt "$large" ]; then
		pass "$desc ($small < $large)"
	else
		fail "$desc" "$small is not smaller than $large"
	fi
}

# ── Test 0: the image runs the versions go.mod pins ────
# The container and `go test` must resolve the same dependencies: a newer
# minifier or Caddy in the image is exactly how behavior silently drifts.
echo ""
echo "=== Test 0: dependency versions ==="
for mod in "github.com/caddyserver/caddy/v2" "github.com/tdewolff/minify/v2"; do
	want=$(awk -v m="$mod" '$1 == m && $2 ~ /^v/ {print $2}' "$ROOT/go.mod")
	got=$(docker run --rm --entrypoint /bin/cat "$IMAGE" /usr/share/licenses/versions.txt 2>/dev/null |
		awk -v m="$mod" '$1 == "dep" && $2 == m {print $3}')
	if [ -n "$want" ] && [ "$want" = "$got" ]; then
		echo "  PASS: $mod $got matches go.mod"
		PASS=$((PASS + 1))
	else
		echo "  FAIL: $mod: go.mod pins ${want:-nothing}, image has ${got:-nothing}"
		FAIL=$((FAIL + 1))
	fi
done

# ── Test 1: global options and route toggles ───────────
echo ""
echo "=== Test 1: global options and route toggles ==="
start Caddyfile.global
same "HTML keeps comments and quotes" / "$FIXTURES/expected/global-index.html"
same "CSS is minified" /style.css "$FIXTURES/expected/global-style.css"
same "SVG is minified" /icon.svg "$FIXTURES/expected/global-icon.svg"
same "JavaScript stays untouched when not listed" /app.js "$FIXTURES/www/app.js"
same "JSON is minified" /data.json "$FIXTURES/expected/global-data.json"
same "XML is minified" /data.xml "$FIXTURES/expected/global-data.xml"
same "RSS is minified as XML" /feed.rss "$FIXTURES/expected/global-feed.rss"
contains "RSS content type survives" "application/rss+xml" "$(curl -sSI "http://127.0.0.1:$PORT/feed.rss" | tr -d '\r')"
same "plain text passes through" /plain.txt "$FIXTURES/www/plain.txt"
same "path matcher turns minification off" /admin/ "$FIXTURES/www/admin/index.html"
same "no-transform responses pass through" /raw/ "$FIXTURES/www/raw/index.html"
same "nested minified off beats the enclosing on" /nested/ "$FIXTURES/www/nested/index.html"

# ── Test 2: default formats ────────────────────────────
echo ""
echo "=== Test 2: default formats ==="
start Caddyfile.default
same "HTML uses default options" / "$FIXTURES/expected/default-index.html"
same "CSS uses default options" /style.css "$FIXTURES/expected/default-style.css"
same "JavaScript is minified by default" /app.js "$FIXTURES/expected/default-app.js"
same "JSON stays untouched by default" /data.json "$FIXTURES/www/data.json"
same "XML stays untouched by default" /data.xml "$FIXTURES/www/data.xml"
same "RSS stays untouched by default" /feed.rss "$FIXTURES/www/feed.rss"
same "plain text passes through" /plain.txt "$FIXTURES/www/plain.txt"

# ── Test 3: streaming mode ─────────────────────────────
echo ""
echo "=== Test 3: streaming mode ==="
start Caddyfile.streaming
same "streamed HTML matches buffered bytes" / "$FIXTURES/expected/global-index.html"
same "streamed CSS matches buffered bytes" /style.css "$FIXTURES/expected/global-style.css"
same "streamed SVG matches buffered bytes" /icon.svg "$FIXTURES/expected/global-icon.svg"
same "streamed JSON matches buffered bytes" /data.json "$FIXTURES/expected/global-data.json"
same "streamed XML matches buffered bytes" /data.xml "$FIXTURES/expected/global-data.xml"
same "streamed RSS matches buffered bytes" /feed.rss "$FIXTURES/expected/global-feed.rss"
same "large streamed page is minified" /large.html "$FIXTURES/expected/streaming-large.html"
same "plain text passes through" /plain.txt "$FIXTURES/www/plain.txt"
same "path matcher turns streaming off" /admin/ "$FIXTURES/www/admin/index.html"
same "no-transform responses pass through" /raw/ "$FIXTURES/www/raw/index.html"
same "nested minified off beats the enclosing on" /nested/ "$FIXTURES/www/nested/index.html"

# ── Test 4: buffer limit ───────────────────────────────
echo ""
echo "=== Test 4: buffer limit ==="
start Caddyfile.limit
same "oversized response passes through unchanged" /large.html "$FIXTURES/www/large.html"

# ── Test 5: encode interaction ─────────────────────────
echo ""
echo "=== Test 5: encode interaction ==="
start Caddyfile.encode

request "http://127.0.0.1:$PORT/"
equal "identity status" 200 "$(status)"
body_same "identity body is minified" "$FIXTURES/expected/global-index.html"
identity_size=$(body_size)

request -H "Accept-Encoding: gzip" "http://127.0.0.1:$PORT/"
equal "gzip status" 200 "$(status)"
equal "gzip content encoding" gzip "$(header Content-Encoding)"
gzip -dc "$TMP/body" >"$TMP/plain"
file_same "gzip body round-trips the minified bytes" "$FIXTURES/expected/global-index.html" "$TMP/plain"
gzip_size=$(body_size)
smaller "gzip body is smaller than identity" "$gzip_size" "$identity_size"

request -H "Accept-Encoding: zstd" "http://127.0.0.1:$PORT/"
equal "zstd status" 200 "$(status)"
equal "zstd content encoding" zstd "$(header Content-Encoding)"
equal "zstd body magic" 28b52ffd "$(od -An -tx1 -N4 "$TMP/body" | tr -d ' \n')"
zstd_size=$(body_size)
smaller "zstd body is smaller than identity" "$zstd_size" "$identity_size"

request -H "Accept-Encoding: zstd" "http://127.0.0.1:$PORT/plain/"
plain_zstd_size=$(body_size)
smaller "minification runs before encoding" "$zstd_size" "$plain_zstd_size"

request "http://127.0.0.1:$PORT/plain/"
equal "plain route status" 200 "$(status)"
body_same "minified off route serves the original bytes" "$FIXTURES/www/index.html"

# ── Test 6: status handling ────────────────────────────
echo ""
echo "=== Test 6: status handling ==="
start Caddyfile.status
index_size=$(wc -c <"$FIXTURES/www/index.html" | tr -d ' ')

curl -sS -I -D "$TMP/head" -o /dev/null "http://127.0.0.1:$PORT/"
equal "HEAD status" 200 "$(status)"
equal "HEAD content length" "$index_size" "$(header Content-Length)"

request -H "Range: bytes=0-9" "http://127.0.0.1:$PORT/"
equal "range status" 206 "$(status)"
equal "range content range" "bytes 0-9/$index_size" "$(header Content-Range)"
head -c 10 "$FIXTURES/www/index.html" >"$TMP/prefix"
body_same "range response is the original prefix" "$TMP/prefix"

request -H "If-Modified-Since: Wed, 01 Jan 2030 00:00:00 GMT" "http://127.0.0.1:$PORT/"
equal "conditional status" 304 "$(status)"
equal "conditional response has an empty body" 0 "$(body_size)"

request "http://127.0.0.1:$PORT/"
origin_etag=$(header ETag)
if echo "$origin_etag" | grep -Eq '^"[0-9a-z]+-[0-9a-z]+"$'; then
	pass "minified response keeps the file validator"
else
	fail "minified response keeps the file validator" "got: $origin_etag"
fi

request -H "If-None-Match: $origin_etag" "http://127.0.0.1:$PORT/"
equal "etag conditional status" 304 "$(status)"
equal "etag conditional response has an empty body" 0 "$(body_size)"
equal "etag conditional keeps the origin validator" "$origin_etag" "$(header ETag)"

request "http://127.0.0.1:$PORT/nocontent"
equal "/nocontent status" 204 "$(status)"
equal "/nocontent body" 0 "$(body_size)"

request "http://127.0.0.1:$PORT/reset"
equal "/reset status" 205 "$(status)"
equal "/reset body" 0 "$(body_size)"

# ── Test 7: provision-time validation ──────────────────
echo ""
echo "=== Test 7: provision-time validation ==="
mkdir -p "$TMP/neg"

cat >"$TMP/neg/unknown-option.Caddyfile" <<'CADDY'
{
	admin off
	minified {
		nope
	}
}
http://:8080 {
	minified on
	respond "ok"
}
CADDY

cat >"$TMP/neg/bad-policy.Caddyfile" <<'CADDY'
{
	admin off
	minified {
		max_body_size_policy drop
	}
}
http://:8080 {
	minified on
	respond "ok"
}
CADDY

cat >"$TMP/neg/bad-size.Caddyfile" <<'CADDY'
{
	admin off
	minified {
		max_body_size -1
	}
}
http://:8080 {
	minified on
	respond "ok"
}
CADDY

cat >"$TMP/neg/streaming-limit.Caddyfile" <<'CADDY'
{
	admin off
	minified {
		streaming
		max_body_size 10
	}
}
http://:8080 {
	minified on
	respond "ok"
}
CADDY

cat >"$TMP/neg/directive-block.Caddyfile" <<'CADDY'
{
	admin off
}
http://:8080 {
	minified on {
		css
	}
	respond "ok"
}
CADDY

cat >"$TMP/neg/directive-arg.Caddyfile" <<'CADDY'
{
	admin off
}
http://:8080 {
	minified maybe
	respond "ok"
}
CADDY

cat >"$TMP/neg/html-option.Caddyfile" <<'CADDY'
{
	admin off
	minified {
		html {
			nope
		}
	}
}
http://:8080 {
	minified on
	respond "ok"
}
CADDY

neg() {
	desc=$1
	file=$2
	expect=$3
	out=$(docker run --rm -v "$file:/etc/caddy/Caddyfile:ro" "$IMAGE" \
		validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1) && status=0 || status=$?
	if [ "$status" -ne 0 ] && echo "$out" | grep -q -- "$expect"; then
		pass "$desc"
	else
		fail "$desc" "expected non-zero exit containing: $expect" "got exit=$status: $out"
	fi
}

neg "unknown global option fails" "$TMP/neg/unknown-option.Caddyfile" 'unrecognized minified option'
neg "unknown body-size policy fails" "$TMP/neg/bad-policy.Caddyfile" 'must be pass or reject'
neg "negative max_body_size fails" "$TMP/neg/bad-size.Caddyfile" 'invalid size'
neg "streaming with max_body_size fails" "$TMP/neg/streaming-limit.Caddyfile" 'cannot be combined with max_body_size'
neg "directive blocks fail" "$TMP/neg/directive-block.Caddyfile" 'does not take a block'
neg "unknown directive argument fails" "$TMP/neg/directive-arg.Caddyfile" 'expected on or off'
neg "unknown html option fails" "$TMP/neg/html-option.Caddyfile" 'unrecognized html option'

# ── Test 8: reverse proxy with identity upstream ───────
# The handler is middleware, so a proxied response is minified exactly like a
# file_server response when the upstream answers without Content-Encoding.
echo ""
echo "=== Test 8: reverse proxy identity bodies ==="
PROXY_ROOT="$TMP/proxy"
mkdir -p "$PROXY_ROOT"
cp "$FIXTURES/www/index.html" "$PROXY_ROOT/page.html"
cp "$FIXTURES/www/style.css" "$PROXY_ROOT/style.css"
gzip -9 -n -c "$PROXY_ROOT/page.html" >"$PROXY_ROOT/page.html.gz"
gzip -9 -n -c "$PROXY_ROOT/style.css" >"$PROXY_ROOT/style.css.gz"

start Caddyfile.proxy "$PROXY_ROOT"
same "proxied HTML is minified" / "$FIXTURES/expected/default-index.html"
same "proxied CSS is minified" /style.css "$FIXTURES/expected/default-style.css"
same "proxied JavaScript is minified" /app.js "$FIXTURES/expected/default-app.js"
same "proxied SVG is minified" /icon.svg "$FIXTURES/expected/global-icon.svg"
same "proxied JSON stays untouched" /data.json "$FIXTURES/www/data.json"
same "proxied XML stays untouched" /data.xml "$FIXTURES/www/data.xml"
same "proxied plain text passes through" /plain.txt "$FIXTURES/www/plain.txt"
same "proxied minified off serves the origin bytes" /off/ "$FIXTURES/www/index.html"
same "proxied no-transform origin response passes through" /notransform/raw/index.html "$FIXTURES/www/raw/index.html"

request "http://127.0.0.1:$PORT/"
equal "proxied identity status" 200 "$(status)"
equal "proxied minified length describes the minified body" \
	"$(wc -c <"$FIXTURES/expected/default-index.html" | tr -d ' ')" \
	"$(header Content-Length)"
equal "proxied identity response has no encoding" "" "$(header Content-Encoding)"
if echo "$(header ETag)" | grep -Eq '^"[0-9a-z]+-[0-9a-z]+"$'; then
	pass "proxied minified response keeps the origin validator"
else
	fail "proxied minified response keeps the origin validator" "got: $(header ETag)"
fi

request "http://127.0.0.1:$PORT/missing.html"
equal "proxied origin 404 status survives" 404 "$(status)"

# ── Test 9: reverse proxy with compressed upstream ─────
# A compressed origin response is never minified, whatever the format.
echo ""
echo "=== Test 9: reverse proxy compressed bodies ==="
request "http://127.0.0.1:$PORT/gzip/page.html"
equal "compressed origin status" 200 "$(status)"
equal "compressed origin keeps its content encoding" gzip "$(header Content-Encoding)"
body_same "compressed HTML body is passed through byte for byte" "$PROXY_ROOT/page.html.gz"
gzip -dc "$TMP/body" >"$TMP/plain"
file_same "compressed HTML round-trips the origin bytes" "$PROXY_ROOT/page.html" "$TMP/plain"

request "http://127.0.0.1:$PORT/gzip/style.css"
equal "compressed CSS origin status" 200 "$(status)"
equal "compressed CSS origin keeps its content encoding" gzip "$(header Content-Encoding)"
body_same "compressed CSS body is passed through byte for byte" "$PROXY_ROOT/style.css.gz"
gzip -dc "$TMP/body" >"$TMP/plain"
file_same "compressed CSS stays unminified" "$PROXY_ROOT/style.css" "$TMP/plain"

# ── Test 10: reverse proxy in streaming mode ───────────
echo ""
echo "=== Test 10: reverse proxy streaming ==="
start Caddyfile.proxy-stream "$PROXY_ROOT"
same "streamed proxied HTML matches buffered bytes" / "$FIXTURES/expected/default-index.html"
same "streamed proxied CSS matches buffered bytes" /style.css "$FIXTURES/expected/default-style.css"
same "streamed proxied JavaScript matches buffered bytes" /app.js "$FIXTURES/expected/default-app.js"
same "streamed proxied SVG matches buffered bytes" /icon.svg "$FIXTURES/expected/global-icon.svg"
same "streamed proxied JSON stays untouched" /data.json "$FIXTURES/www/data.json"
same "streamed proxied minified off serves the origin bytes" /off/ "$FIXTURES/www/index.html"
same "streamed proxied no-transform response passes through" /notransform/raw/index.html "$FIXTURES/www/raw/index.html"
request "http://127.0.0.1:$PORT/gzip/page.html"
body_same "streamed proxied compressed body is passed through" "$PROXY_ROOT/page.html.gz"

# ── Summary ────────────────────────────────────────────
echo ""
echo "=========================================="
echo "  PASS: $PASS   FAIL: $FAIL"
echo "=========================================="
[ "$FAIL" -eq 0 ] && echo "ALL TESTS PASSED" || echo "SOME TESTS FAILED"
exit $FAIL
