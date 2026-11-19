# caddy-minified

`caddy-minified` is an HTTP middleware for Caddy v2 that minifies response
bodies after the rest of the handler chain has produced them. It is built on
[`github.com/tdewolff/minify/v2`](https://github.com/tdewolff/minify) and
supports HTML, CSS, JavaScript, SVG, and opt-in JSON and XML. RSS and Atom
feeds are XML and are covered by the XML format.

The plugin is deliberately small:

- Global options choose which formats are eligible and tune the HTML minifier.
- A route directive enables or disables minification for a site, a path, or a
  nested route.
- A `minified off` anywhere inside an enabled route wins over an enclosing
  `minified on`.
- Responses that cannot safely be rewritten (compressed, partial, empty,
  `no-transform`, or unsupported content types) pass through untouched.
- An optional buffer limit stops buffering oversized responses and either
  streams them unchanged or rejects them.

The project also ships a reproducible build and test harness:

- `./build.sh` builds the Caddy image from `Dockerfile`.
- `test/e2e.sh` runs that image with different Caddyfiles and document
  roots mounted into it. It never builds the image itself.

## Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Global options](#global-options)
- [Buffer limits](#buffer-limits)
- [Streaming mode](#streaming-mode)
- [Route directive](#route-directive)
- [JSON configuration](#json-configuration)
- [Response eligibility](#response-eligibility)
- [Ordering and interactions](#ordering-and-interactions)
- [Caddyfile recipes](#caddyfile-recipes)
- [Building the image](#building-the-image)
- [Testing](#testing)
- [Project layout](#project-layout)
- [Operational notes](#operational-notes)
- [Troubleshooting](#troubleshooting)
- [Maintaining this repository](#maintaining-this-repository)
- [License](#license)

## Requirements

- Caddy v2.11.9 or newer.
- Go 1.26 or newer for local builds.
- Docker for the image and end-to-end harness.

The module path is `github.com/frinknet/caddy-minified`. The Caddy module ID is
`http.handlers.minified`. The Caddyfile directive and global option are both
named `minified`.

## Installation

### Build with xcaddy

```sh
xcaddy build --with github.com/frinknet/caddy-minified
```

### Build from a checkout

```sh
xcaddy build \
  --with github.com/frinknet/caddy-minified \
  --replace github.com/frinknet/caddy-minified="$PWD"
```

### Use the container image

```sh
./build.sh
docker run --rm -p 8080:80 caddy-minified:test
```

`build.sh` derives the image name from the first line of `NOTICE`. For this
repository it builds `caddy-minified:test`.

The image is a scratch image containing:

- the Caddy binary with this plugin compiled in,
- a toybox multicall binary, so `sh`, `ls`, `wget`, and friends exist,
- CA certificates, tzdata, and the licenses of Caddy and linked modules,
- the demo site from `www/`.

The demo Caddyfile is installed at `/etc/caddy/Caddyfile` and serves the files
in `/srv/www`. The image entrypoint is `/bin/caddy`, and the default command is
`run --config /etc/caddy/Caddyfile --adapter caddyfile`.

## Quick start

```caddyfile
{
	minified {
		html {
			keep_quotes
		}
		css
		js
		json
		xml
		svg
	}
}

example.com {
	minified on
	root * /srv/www
	file_server
}
```

The global block configures the minifier. The route directive activates it.
Without `minified on`, nothing is minified even if a global block exists.

## Global options

The global option is optional. It controls which response formats may be
minified and how HTML is minified.

```caddyfile
{
	minified {
		html {
			keep_comments
			keep_quotes
			keep_whitespace false
		}
		css
		js
		json
		xml
		svg
	}
}
```

### Format selection

| Token  | Enables minification of |
| ------ | ----------------------- |
| `html` | `text/html` and `application/xhtml+xml` |
| `css`  | `text/css` |
| `js`   | `text/javascript`, `application/javascript`, `application/x-javascript` |
| `json` | `application/json` and `application/*+json` media types |
| `xml`  | `application/xml`, `text/xml`, and `application/*+xml` media types, including RSS and Atom feeds |
| `svg`  | `image/svg+xml` |

Rules:

- If the global block lists any format, only the listed formats are eligible.
- `html` is enabled by the `html` token even when it has no sub-options.
- A bare `minified` global option with no block enables the default formats:
  HTML, CSS, JavaScript, and SVG. JSON and XML stay disabled unless listed.
- An empty block, or a block with no format tokens, enables the default
  formats.
- JSON is opt-in. Add `json` to the global block to enable JSON minification.
  It is not part of the default format set, and listing `json` alone enables
  only JSON.
- XML is opt-in. Add `xml` to the global block to enable XML minification. It
  covers `application/xml`, `text/xml`, and `application/*+xml` media types, so
  RSS and Atom feeds are included. It is not part of the default format set,
  and listing `xml` alone enables only XML.
- `image/svg+xml` and `application/xhtml+xml` keep using the `svg` and `html`
  formats even when `xml` is enabled.
- Multiple `minified` global blocks are merged in order.
- Global options never enable the handler. Use `minified on` in a site block.

This means a global block listing only `css` and `svg` will leave JavaScript,
HTML, and JSON responses untouched, even on routes with `minified on`.

### HTML options

All HTML options are booleans. Writing the option by itself means `true`.
Writing `false`, `off`, `no`, or `0` after it disables the option.

| Option | Default | Effect |
| ------ | ------- | ------ |
| `keep_comments` | `false` | Keep ordinary HTML comments. |
| `keep_special_comments` | `false` | Keep conditional and special comments. |
| `keep_default_attr_vals` | `false` | Keep attribute values that equal their HTML default. |
| `keep_document_tags` | `false` | Keep `<html>`, `<head>`, and `<body>` tags. |
| `keep_end_tags` | `false` | Keep optional end tags. |
| `keep_quotes` | `false` | Keep quotes around attribute values. |
| `keep_whitespace` | `false` | Keep whitespace instead of collapsing it. |

Example:

```caddyfile
{
	minified {
		html {
			keep_comments
			keep_quotes
			keep_document_tags true
		}
	}
}
```

### Buffer limits

By default the engine buffers each eligible response in full and has no size
limit. Set `max_body_size` to bound that buffer. When a response grows past the
limit, buffering stops immediately and `max_body_size_policy` decides what
happens next.

```caddyfile
{
	minified {
		max_body_size 1MiB
		max_body_size_policy pass
	}
}
```

| Option | Default | Effect |
| ------ | ------- | ------ |
| `max_body_size` | unlimited | Maximum number of response bytes buffered per eligible response. |
| `max_body_size_policy` | `pass` | What to do when the limit is exceeded. |

Policies:

- `pass` streams the response unchanged. Headers, status, and the bytes already
  buffered are written as-is, and every remaining byte is streamed directly to
  the client. The response is not minified.
- `reject` discards the buffered bytes and answers `413 Request Entity Too
  Large` with a short `text/plain` body. The original response is not sent.

Sizes accept a plain byte count or a unit suffix. Units are case-insensitive:
`b`, `k`/`kb` (1000), `ki`/`kib` (1024), `m`/`mb` (1000²), `mi`/`mib` (1024²),
`g`/`gb` (1000³), and `gi`/`gib` (1024³). For example, `524288`,
`512KiB`, and `1mb` are all valid.

Rules:

- `0` or an omitted `max_body_size` means unlimited.
- The limit applies only to responses that would otherwise be buffered.
  Ineligible responses stream normally and are never rejected.
- A response that is exactly at the limit is still buffered and may be
  minified; only a response larger than the limit triggers the policy.
- `max_body_size_policy` is ignored when the limit is unlimited.
- The limit is configured globally and is inherited by route handlers.

### Streaming mode

By default the engine buffers an eligible response, minifies it, and sends the
result only when it is smaller than the original. That costs memory
proportional to the body and delays the first byte. `streaming` minifies the
response while it is written instead.

```caddyfile
{
	minified {
		streaming
	}
}
```

| Option | Default | Effect |
| ------ | ------- | ------ |
| `streaming` | `false` | Minify eligible responses incrementally instead of buffering them. |

Rules:

- Write the bare option to enable it, or `streaming false` to turn it off.
- Streaming mode is configured globally and inherited by route handlers. Enable
  the handler with `minified on` as usual.
- Bytes reach the minifier as the upstream handler writes them, so the body is
  never held in memory as a whole.
- The size guarantee is dropped. The minified bytes are sent even when they are
  not smaller than the origin body, because the origin size is not known in
  advance.
- There is no fallback. If the minifier fails mid-stream, the response is
  aborted: the client receives a truncated or empty body and the connection is
  closed.
- `Content-Length`, `Content-MD5`, and `Accept-Ranges` are removed as soon as
  streaming starts, so the response is sent with chunked encoding. `ETag` is
  kept, see [ETags and caching](#etags-and-caching).
- `max_body_size` cannot be combined with `streaming`. Configuring both is a
  startup error.
- A response with no `Content-Type` is sniffed from its first bytes. The engine
  holds at most 512 bytes before deciding, then streams the remainder.
- `minified off` still overrides an enclosing streaming handler, and
  ineligible responses pass through untouched.

Streaming reduces memory use, not document length requirements. The minifier
still needs a complete document, so event streams and long polling responses
should stay disabled:

```caddyfile
@stream path /events
minified @stream off
```

## Route directive

The route directive accepts exactly one of `on` or `off`. A bare `minified`
is treated as `minified on`.

```caddyfile
example.com {
	minified on
}
```

`minified off` disables minification for the matched route. It also prevents an
enclosing `minified on` from minifying the response, so this works as expected:

```caddyfile
example.com {
	minified on

	@admin path /admin/*
	minified @admin off

	reverse_proxy localhost:8080
}
```

The same directive can be used inside a `route` block:

```caddyfile
example.com {
	minified on

	route /admin/* {
		minified off
		reverse_proxy localhost:9000
	}

	reverse_proxy localhost:8080
}
```

Matchers are standard Caddy matchers, so path, host, header, method, and
expression matchers all work:

```caddyfile
@api {
	path /api/*
	header Accept application/json
}
minified @api off
```

The directive does not accept inline format lists. Select formats in the global
block instead.

## JSON configuration

Caddy's JSON configuration can use the handler directly. The module ID is
`http.handlers.minified`.

```json
{
  "handler": "minified",
  "config": {
    "enabled": true,
    "html": {
      "enabled": true,
      "keep_comments": true,
      "keep_quotes": true
    },
    "css": true,
    "js": false,
    "json": true,
    "xml": true,
    "svg": true,
    "max_body_size": 1048576,
    "max_body_size_policy": "pass",
    "streaming": false
  }
}
```

`max_body_size` is a byte count. `max_body_size_policy` accepts `pass` or
`reject`; an omitted or empty policy means `pass`. These fields are optional and
default to an unlimited buffer. `streaming` switches the handler to incremental
minification and cannot be combined with `max_body_size`.

If `config` is omitted, the handler defaults to the HTML, CSS, JavaScript, and
SVG formats enabled but the handler itself disabled. Set `enabled` to `true` to
activate it. JSON and XML remain disabled until `"json": true` or
`"xml": true` is set. If `enabled` is true and no format is enabled, the
default formats are enabled during provisioning.

The `config` field is part of the public JSON API. Keep its field names stable.

## Response eligibility

A response is buffered and considered for minification only when all of these
are true:

- The request method is not `HEAD`.
- The status code is 2xx.
- The status code is not `204`, `205`, or `206`.
- There is no `Content-Range` header.
- There is no `Upgrade` header.
- `Content-Encoding` is empty or `identity`.
- `Cache-Control` does not contain `no-transform`.
- `Content-Type` names a format enabled by the configuration.

Responses with no `Content-Type` are not buffered. Handlers that write a body
without setting a content type will pass through untouched. In
[streaming mode](#streaming-mode) such responses are sniffed from their first
bytes instead.

When minification succeeds and the result is smaller, the plugin:

- writes the minified body,
- removes `Content-Length`, `Content-MD5`, and `Accept-Ranges`.

The origin `ETag` is kept, see [ETags and caching](#etags-and-caching).

If the minifier returns an error, or produces a body that is not smaller, the
original buffered response is written unchanged.

In [streaming mode](#streaming-mode) these rules are applied to the response
headers, the body is minified as it is written, and the size checks described
below do not apply.

When `max_body_size` is configured, the limit is enforced while the response is
being written. A response larger than the limit never reaches the minifier; it
is streamed or rejected according to `max_body_size_policy`. See
[Buffer limits](#buffer-limits).

Compressed responses are skipped. Place `encode` outside `minified` if you want
both. This plugin registers its directive order after `encode`, so the usual
Caddy ordering already does that: `minified` sees the uncompressed body and
`encode` compresses the minified result.

## Ordering and interactions

`minified` is registered after the standard `encode` directive and before
responders such as `respond`, `reverse_proxy`, and `file_server`.

Two properties matter:

1. An enabled handler is applied at most once per request, even if several
   enabled `minified` directives match.
2. A disabled handler marks the request, and any enclosing enabled handler
   writes the original body.

That second property is what makes nested `minified off` work. It does not rely
on response headers or on route order beyond the normal middleware nesting.

### Upstream compression

If a reverse proxy forwards a compressed response, the plugin skips it. Tell
the upstream to send identity encoding when minification is required:

```caddyfile
reverse_proxy localhost:8080 {
	header_up Accept-Encoding identity
}
```

Alternatively, remove `Accept-Encoding` at the proxy and let Caddy's `encode`
directive compress the final response.

### ETags and caching

Revalidation is always decided by the origin. A client does not check an
`ETag` itself; it stores the tag and sends it back in `If-None-Match` on the
next request, and the server that issued the tag answers `304` or `200`. So
the only thing that matters is that the tag handed out is one the origin can
still validate.

The plugin therefore leaves `ETag` alone. A minified response carries the
origin validator, the client sends it back unchanged, and the origin compares
it against its own entity:

- unchanged origin entity: the origin answers `304 Not Modified`, the engine
  passes it through, and the client keeps the minified bytes it already has;
- changed origin entity: the origin answers `200` with a new tag, and the body
  is minified again.

`Last-Modified` is left alone as well, so `If-Modified-Since` revalidation keeps
working unchanged. Nothing is derived and nothing is rewritten, so `HEAD`,
range, and non-minified responses all agree with the GET they describe.

The validator names the origin entity, not the transformed bytes, so it stays
strong while the minified representation is only semantically identical.
Conditional `GET`/`HEAD` is the only place it is used, and `Accept-Ranges` is
removed from minified responses so clients do not range over the transformed
body.

### Streaming responses

By default the engine buffers the entire response body, and
[streaming mode](#streaming-mode) still needs complete documents. Server-sent
events, long polling, and other streaming responses should not be minified.
Disable the handler for those routes:

```caddyfile
@stream path /events
minified @stream off
```

## Caddyfile recipes

### Static site with an admin area

```caddyfile
{
	minified {
		html {
			keep_quotes
		}
		css
		js
		svg
	}
}

example.com {
	root * /srv/www
	minified on

	@admin path /admin/*
	minified @admin off

	file_server
}
```

### Reverse proxy with a JSON API exception

```caddyfile
example.com {
	minified on

	@api path /api/*
	minified @api off

	reverse_proxy localhost:8080 {
		header_up Accept-Encoding identity
	}
}
```

### Keep comments for a marketing page

```caddyfile
{
	minified {
		html {
			keep_comments
			keep_quotes
		}
		css
		svg
	}
}

marketing.example.com {
	minified on
	reverse_proxy localhost:3000
}
```

### Disable transformation for downloads

```caddyfile
example.com {
	minified on

	@downloads path /downloads/*
	minified @downloads off

	root * /srv/www
	file_server
}
```

## Building the image

```sh
./build.sh
```

The script reads `NOTICE`, computes the repository slug, and builds the image
as `<slug>:test`. It also supports a one-time initialization mode:

```sh
./build.sh --init
```

The Dockerfile has three stages:

1. Acquire toybox, CA certificates, tzdata, and licenses. This stage has no
   source dependency and is cache friendly.
2. Build Caddy with `xcaddy`, replacing the module with the local checkout.
   Go module and build caches are mounted as BuildKit caches.
3. Compose a scratch image with the Caddy binary, toybox, assets, and the demo
   site.

The image runs as the scratch filesystem with `/bin/caddy` as the entrypoint
and `run` as the default command. It exposes ports 80, 443, and 443/udp.

## Testing

Run unit tests from the repository root:

```sh
sh test/unit.sh
```

`test/unit.sh` runs `gofmt`, `go vet`, and `go test ./...` in the pinned
builder image with the shared caches, so pass/fail is visible without touching
the host Go toolchain.

Build the image, then run the end-to-end suite:

```sh
./build.sh
sh test/e2e.sh
```

`test/e2e.sh` requires `caddy-minified:test` to exist. It does not build it.
The harness starts the image with a Caddyfile and document root mounted from
`test/e2e/fixtures/`, publishes the site on a host port, fetches responses
with `curl`, and compares the exact bytes with the expected fixtures. It also
checks that the image resolves the same Caddy and minifier versions as
`go.mod`, and validates deliberately broken Caddyfiles to prove
provision-time errors.

The suite covers:

- global HTML options, including kept comments and quotes,
- format selection, including JavaScript staying untouched when not listed,
- `minified off` with a path matcher,
- `minified off` nested inside an enclosing `minified on`,
- `Cache-Control: no-transform` responses,
- plain text pass-through,
- oversized responses passing through when `max_body_size` is exceeded,
- streaming mode, including byte-identical output with buffered mode, content
  type sniffing, nested `minified off`, and `no-transform` pass-through,
- JSON minification when explicitly enabled, and JSON pass-through otherwise,
- XML and RSS minification when explicitly enabled, and XML pass-through
  otherwise,
- default format behavior when no global options are present,
- `encode` interaction with gzip and zstd, including a byte-exact gzip
  round-trip and compressed sizes that prove minification runs before encoding,
- `HEAD`, 204, 205, 206, and 304 responses,
- directive ordering after `encode`,
- dependency versions matching `go.mod`,
- provision-time rejection of invalid options and combinations.

## Project layout

```text
caddy-minified/
├── minified.go              # only Go file at the repository root
├── pkg/
│   ├── config/
│   │   ├── config.go        # configuration, defaults, media type support
│   │   ├── caddyfile.go     # global option and route directive parsers
│   │   └── *_test.go        # parser and adaptation tests
│   └── minify/
│       ├── engine.go        # response buffering and tdewolff integration
│       ├── stream.go        # incremental minification
│       └── engine_test.go   # engine tests
├── www/                     # demo site baked into the image
├── test/
│   ├── e2e.sh               # runs the image built by build.sh
│   ├── unit.sh              # gofmt, vet, and tests in the builder image
│   └── e2e/fixtures/        # Caddyfiles, document roots, expected bytes
├── Dockerfile
├── build.sh
├── README.md
├── BACKLOG.md
└── AGENTS.md
```

Only `minified.go` is allowed at the repository root. Every other Go source
file and test belongs under `pkg/`.

## Operational notes

- Memory use is proportional to the largest eligible response body buffered at
  the same time. Set `max_body_size` to cap the buffer, enable
  [streaming mode](#streaming-mode) to avoid buffering entirely, or disable
  minification on routes that serve very large pages.
- The plugin does not cache minified output. It minifies per response.
- The plugin does not alter `Content-Type`. If the origin lies about the media
  type, minification will either be skipped or operate on the wrong format.
- The minifier is safe for concurrent use. Each module instance owns one
  configured engine.
- The plugin does not minify request bodies.
- The plugin does not decompress or recompress responses.

## Troubleshooting

### Nothing is minified

Check, in order:

1. Is `minified on` present in the matching route?
2. Does the response have a supported `Content-Type`?
3. Is the format enabled in the global block?
4. Is `Content-Encoding` non-identity?
5. Does `Cache-Control` contain `no-transform`?
6. Is the status code 2xx and not 204, 205, or 206?
7. Is a more specific `minified off` matching the request?
8. Is the body larger than `max_body_size`? Raise the limit or use the `pass`
   policy to stream it unchanged.
9. Did minification make the body longer? If so, the original is kept, except
   in streaming mode, where the minified bytes are always sent.

### A streamed response is truncated or empty

Streaming mode has no fallback. If the minifier rejects the document, it stops
mid-response and the client sees a truncated or empty body. Look for
incomplete or invalid markup, JSON, or XML in the origin response, or disable
`streaming` so the buffered path can fall back to the original body.

### A nested `minified off` is not working

Make sure the `off` directive is actually inside the same middleware chain as
the `on` directive. Different `handle` blocks are alternatives, not nesting.
The state marker only flows through the chain that actually executes.

### HTML loses tags that matter

The tdewolff HTML minifier removes optional document tags and end tags by
default. Enable `keep_document_tags`, `keep_end_tags`, or both in the global
options if the surrounding tooling requires them.

### Reverse proxy responses are never minified

The upstream is probably sending compressed responses. Set
`header_up Accept-Encoding identity` on the proxy, or let Caddy `encode` the
final response.

## Maintaining this repository

- `README.md` is the user-facing source of truth. Update it in the same change
  as any behavior, configuration, layout, build, or test change.
- `BACKLOG.md` contains only pending work. When an item is completed and its
  tests pass, delete it from the backlog. Do not check it off or keep a done
  section. Git history is the record.
- `AGENTS.md` contains repository rules for automated contributors.
- Keep `Dockerfile`, `build.sh`, `.dockerignore`, and `test/e2e/` shape
  consistent with the other `caddy-*` plugins unless a real difference is
  required.
- Do not edit `LICENSE` or `NOTICE` except for legal or copyright changes.

## License

Apache-2.0. See `LICENSE` and `NOTICE`.
