# singbox-converter

All-in-one Go binary that converts Mihomo/Clash-Meta YAML configurations to sing-box JSON format. Also serves as a rule-set converter for runtime rule fetching.

## Features

- **Config Conversion**: Accepts a mihomo subscription URL → returns complete sing-box JSON config
- **Rule-set Serving**: Converts mihomo rule-provider files (classical/domain/ipcidr) to sing-box rule-set JSON on the fly
- **Self-contained**: Single binary, no external dependencies
- **Cross-platform**: Pre-built for Windows/Linux/macOS (amd64 + arm64)
- **sing-box 1.12+ compatible**: Uses new DNS server format, fakeip type, hijack-dns action (zero deprecation warnings)

## Usage

### Start the server

```bash
./singbox-converter --listen :8080 --self-url http://your-server:8080
```

- `--listen` — HTTP listen address (default `:8080`)
- `--self-url` — External URL of this converter, used in generated rule-set URLs (default `http://127.0.0.1:8080`)

### Convert a mihomo config

```bash
# From URL (subscription) — standard mode (fallback groups → urltest)
curl "http://127.0.0.1:8080/convert?url=https://example.com/mihomo.yaml" -o singbox-config.json

# From URL — fallback mode (fallback groups → native fallback type, requires forked sing-box)
curl "http://127.0.0.1:8080/convert?url=https://example.com/mihomo.yaml&fallback=true" -o singbox-config.json

# From local file
curl "http://127.0.0.1:8080/convert?file=/path/to/mihomo.yaml" -o singbox-config.json
curl "http://127.0.0.1:8080/convert?file=/path/to/mihomo.yaml&fallback=true" -o singbox-config.json
```

### `?fallback=true` parameter

| | Without `fallback=true` | With `fallback=true` |
|---|---|---|
| Mihomo `fallback` groups | converted to `urltest` | converted to native `fallback` type |
| Compatible with | standard sing-box | **forked sing-box** from `sing-box-fork/` only |
| Selection logic | lowest latency | ordered priority, first healthy wins |

### Convert a rule-set file

```bash
curl "http://127.0.0.1:8080/ruleset?url=https://example.com/rules.yaml&behavior=classical"
```

Behavior: `classical`, `domain`, or `ipcidr`.

### Health check

```bash
curl http://127.0.0.1:8080/health
```

## How it works

1. You start the converter as a persistent service
2. Request config conversion via `/convert?url=<mihomo_url>`
3. The converter downloads the mihomo YAML, converts all proxies/groups/rules/DNS to sing-box format
4. Rule-provider URLs in the generated config point back to this converter's `/ruleset` endpoint
5. When sing-box loads the config, it fetches rule-sets from the converter transparently

## Supported proxy types

| Mihomo | sing-box |
|--------|----------|
| ss | shadowsocks |
| vmess | vmess |
| socks5 | socks |
| hysteria | hysteria |
| hysteria2 | hysteria2 |
| ssh | ssh |

## Supported rule types

DOMAIN, DOMAIN-SUFFIX, DOMAIN-KEYWORD, DOMAIN-REGEX, IP-CIDR, IP-CIDR6, SRC-IP-CIDR, DST-PORT, SRC-PORT, PROCESS-NAME, PROCESS-PATH, PROCESS-NAME-REGEX, PROCESS-PATH-REGEX, NETWORK, GEOSITE, GEOIP, RULE-SET, DOMAIN-WILDCARD, IP-SUFFIX

## Known limitations

- **Proxy providers**: Subscription node lists are not fetched at conversion time. Groups using `use:` get a `direct` fallback placeholder.
- **`block` outbound**: Kept as a legacy outbound because proxy groups reference it as a selectable member. This triggers a deprecation warning in sing-box 1.11+.
- **Logical rules**: AND/OR/NOT compound rules are skipped.

## Native Fallback Support

This converter emits native `"type": "fallback"` for mihomo `fallback` groups. **You must use the forked sing-box** (from `sing-box-fork/`) which adds this type. The standard sing-box does NOT support it.

Mihomo `fallback` groups are converted with these defaults:
- `fail_threshold`: 3 (consecutive failures before marking DOWN)
- `recover_threshold`: 3 (consecutive successes before marking RECOVERED)

To manually change a `urltest` group to `fallback` in an existing config:
```json
// Change "type" from "urltest" to "fallback"
// Remove "tolerance" (not used by fallback)
// Add fail/recover thresholds
{
  "type": "fallback",
  "tag": "my-group",
  "outbounds": ["node-a", "node-b", "node-c"],
  "url": "https://www.gstatic.com/generate_204",
  "interval": "60s",
  "fail_threshold": 3,
  "recover_threshold": 3
}
```

See `sing-box-fork/FALLBACK.md` for full documentation.

## Cross-platform build

```powershell
# PowerShell
powershell -ExecutionPolicy Bypass -File build.ps1

# Make (Linux/macOS)
make all
```

Outputs in `dist/`:
- `singbox-converter-windows-amd64.exe`
- `singbox-converter-linux-amd64`
- `singbox-converter-linux-arm64`
- `singbox-converter-darwin-amd64`
- `singbox-converter-darwin-arm64`
