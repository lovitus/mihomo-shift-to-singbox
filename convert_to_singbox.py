#!/usr/bin/env python3
"""
Convert mihomo/clash-meta YAML config to sing-box JSON config.
"""
import yaml
import json
import sys
import re
import copy

CONVERTER_BASE = "http://127.0.0.1:8081/convert"

def load_mihomo_config(path):
    with open(path, 'r', encoding='utf-8') as f:
        return yaml.safe_load(f)

def convert_ss(p):
    """Convert shadowsocks proxy"""
    out = {
        "type": "shadowsocks",
        "tag": p["name"],
        "server": p["server"],
        "server_port": int(p["port"]),
        "method": p["cipher"],
        "password": str(p.get("password", "")),
    }
    if p.get("udp", False):
        out["udp_over_tcp"] = False  # enable UDP relay
    if p.get("dialer-proxy"):
        out["detour"] = p["dialer-proxy"]
    return out

def convert_vmess(p):
    """Convert vmess proxy"""
    out = {
        "type": "vmess",
        "tag": p["name"],
        "server": p["server"],
        "server_port": int(p["port"]),
        "uuid": p["uuid"],
        "alter_id": int(p.get("alterId", 0)),
        "security": p.get("cipher", "auto"),
    }
    if p.get("tls"):
        out["tls"] = {
            "enabled": True,
            "insecure": p.get("skip-cert-verify", False),
        }
        if p.get("servername"):
            out["tls"]["server_name"] = p["servername"]
    
    network = p.get("network", "")
    if network == "ws":
        ws_opts = p.get("ws-opts", {})
        transport = {
            "type": "ws",
        }
        if ws_opts.get("path"):
            transport["path"] = ws_opts["path"]
        headers = ws_opts.get("headers", {})
        if headers.get("Host"):
            transport["headers"] = {"Host": [headers["Host"]]}
        out["transport"] = transport
    
    if p.get("udp", False):
        pass  # vmess UDP is handled by sing-box natively
    if p.get("dialer-proxy"):
        out["detour"] = p["dialer-proxy"]
    return out

def convert_socks5(p):
    """Convert socks5 proxy"""
    out = {
        "type": "socks",
        "tag": p["name"],
        "server": p["server"],
        "server_port": int(p["port"]),
        "version": "5",
    }
    if p.get("username"):
        out["username"] = p["username"]
    if p.get("password"):
        out["password"] = str(p["password"])
    if p.get("udp", False):
        out["udp_over_tcp"] = False
    if p.get("dialer-proxy"):
        out["detour"] = p["dialer-proxy"]
    return out

def convert_hysteria(p):
    """Convert hysteria v1 proxy"""
    out = {
        "type": "hysteria",
        "tag": p["name"],
        "server": p["server"],
    }
    
    # Handle ports (port hopping)
    ports_str = str(p.get("ports", p.get("port", "")))
    if "," in ports_str or "-" in ports_str:
        # Port hopping: "33891,65520-65530"
        # sing-box hysteria v1 doesn't support port hopping natively
        # Use the first port only
        parts = ports_str.split(",")
        first_port = parts[0].strip()
        if "-" in first_port:
            first_port = first_port.split("-")[0].strip()
        out["server_port"] = int(first_port)
    else:
        out["server_port"] = int(ports_str)
    
    # up/down bandwidth
    up = p.get("up", "50 Mbps")
    down = p.get("down", "300 Mbps")
    up_mbps = int(re.search(r'(\d+)', str(up)).group(1)) if re.search(r'(\d+)', str(up)) else 50
    down_mbps = int(re.search(r'(\d+)', str(down)).group(1)) if re.search(r'(\d+)', str(down)) else 300
    out["up_mbps"] = up_mbps
    out["down_mbps"] = down_mbps
    
    if p.get("obfs"):
        out["obfs"] = p["obfs"]
    
    out["tls"] = {
        "enabled": True,
        "insecure": p.get("skip-cert-verify", True),
    }
    if p.get("sni"):
        out["tls"]["server_name"] = p["sni"]
    
    if p.get("dialer-proxy"):
        out["detour"] = p["dialer-proxy"]
    return out

def convert_hysteria2(p):
    """Convert hysteria2 proxy"""
    out = {
        "type": "hysteria2",
        "tag": p["name"],
        "server": p["server"],
        "server_port": int(p.get("port", 443)),
        "password": str(p.get("password", "")),
    }
    
    up = p.get("up", "30 Mbps")
    down = p.get("down", "300 Mbps")
    up_mbps = int(re.search(r'(\d+)', str(up)).group(1)) if re.search(r'(\d+)', str(up)) else 30
    down_mbps = int(re.search(r'(\d+)', str(down)).group(1)) if re.search(r'(\d+)', str(down)) else 300
    out["up_mbps"] = up_mbps
    out["down_mbps"] = down_mbps
    
    tls_cfg = {
        "enabled": True,
        "insecure": p.get("skip-cert-verify", False),
    }
    if p.get("sni"):
        tls_cfg["server_name"] = p["sni"]
    alpn = p.get("alpn", [])
    if alpn:
        tls_cfg["alpn"] = alpn
    out["tls"] = tls_cfg
    
    if p.get("dialer-proxy"):
        out["detour"] = p["dialer-proxy"]
    return out

def convert_ssh(p):
    """Convert SSH proxy"""
    out = {
        "type": "ssh",
        "tag": p["name"],
        "server": p["server"],
        "server_port": int(p.get("port", 22)),
        "user": p.get("username", ""),
        "password": str(p.get("password", "")),
    }
    if p.get("private-key"):
        out["private_key"] = p["private-key"]
    if p.get("private-key-passphrase"):
        out["private_key_passphrase"] = p["private-key-passphrase"]
    if p.get("host-key"):
        out["host_key"] = p["host-key"]
    if p.get("host-key-algorithms"):
        out["host_key_algorithms"] = p["host-key-algorithms"]
    if p.get("dialer-proxy"):
        out["detour"] = p["dialer-proxy"]
    return out

def convert_proxy(p):
    """Convert a single proxy to sing-box outbound"""
    ptype = p.get("type", "")
    if ptype == "ss":
        return convert_ss(p)
    elif ptype == "vmess":
        return convert_vmess(p)
    elif ptype == "socks5":
        return convert_socks5(p)
    elif ptype == "hysteria":
        return convert_hysteria(p)
    elif ptype == "hysteria2":
        return convert_hysteria2(p)
    elif ptype == "ssh":
        return convert_ssh(p)
    else:
        print(f"WARNING: Unsupported proxy type '{ptype}' for proxy '{p.get('name', 'unknown')}'", file=sys.stderr)
        return None

def map_group_type(mihomo_type):
    """Map mihomo group type to sing-box type"""
    if mihomo_type == "select":
        return "selector"
    elif mihomo_type in ("url-test", "fallback"):
        return "urltest"
    return "selector"

def convert_group(g, proxy_tags, group_tags):
    """Convert a proxy group to sing-box outbound"""
    sb_type = map_group_type(g.get("type", "select"))
    tag = g["name"]
    
    out = {
        "type": sb_type,
        "tag": tag,
    }
    
    # Build outbounds list from proxies
    outbounds = []
    for pname in (g.get("proxies") or []):
        # Map special names
        if pname == "DIRECT":
            outbounds.append("direct")
        elif pname == "REJECT":
            outbounds.append("block")
        else:
            outbounds.append(pname)
    
    # For groups that use proxy-providers (use: field), add a placeholder
    if g.get("use") and not outbounds:
        outbounds.append("direct")
    elif g.get("use") and outbounds:
        pass  # keep existing outbounds, providers can't be loaded
    elif not outbounds:
        outbounds.append("direct")
    
    out["outbounds"] = outbounds
    
    if sb_type == "urltest":
        url = g.get("url", "https://www.gstatic.com/generate_204")
        interval = g.get("interval", "300")
        tolerance = g.get("tolerance", "150")
        out["url"] = url
        # Cap interval at 3600s (sing-box max)
        interval_sec = int(str(interval).rstrip("s"))
        if interval_sec > 3600:
            interval_sec = 3600
        out["interval"] = f"{interval_sec}s"
        out["tolerance"] = int(tolerance)
        # idle_timeout for lazy groups - must be >= interval
        if g.get("lazy", False):
            idle_sec = max(interval_sec * 2, 1800)
            out["idle_timeout"] = f"{idle_sec}s"
    
    if sb_type == "selector" and g.get("url"):
        # selector doesn't need url-test params, but we can add interrupt_exist_connections
        pass
    
    # For select type, set default if store-selected was true
    if sb_type == "selector" and outbounds:
        out["default"] = outbounds[0]
    
    return out

def build_rule_set_entry(tag, url, behavior, download_detour="direct"):
    """Build a rule_set entry for the route section"""
    # Determine if we should use the converter or direct .srs
    if url.endswith(".srs"):
        return {
            "type": "remote",
            "tag": tag,
            "format": "binary",
            "url": url,
            "download_detour": download_detour,
            "update_interval": "24h",
        }
    else:
        # Use converter
        import urllib.parse
        converter_url = f"{CONVERTER_BASE}?url={urllib.parse.quote(url, safe='')}&behavior={behavior}"
        return {
            "type": "remote",
            "tag": tag,
            "format": "source",
            "url": converter_url,
            "download_detour": download_detour,
            "update_interval": "24h",
        }

def convert_rules(mihomo_rules):
    """Convert mihomo rules to sing-box route rules"""
    sb_rules = []
    
    for rule_str in mihomo_rules:
        rule_str = rule_str.strip()
        if not rule_str or rule_str.startswith("#"):
            continue
        
        r = parse_mihomo_rule(rule_str)
        if r:
            sb_rules.append(r)
    
    return sb_rules

def parse_mihomo_rule(rule_str):
    """Parse a single mihomo rule string to sing-box rule object"""
    rule_str = rule_str.strip()
    if not rule_str or rule_str.startswith("#"):
        return None
    
    # Handle AND/OR/NOT compound rules
    if rule_str.startswith("AND,") or rule_str.startswith("OR,") or rule_str.startswith("NOT,"):
        # These are complex logical rules - sing-box supports logical rules
        return parse_logical_rule(rule_str)
    
    # Handle RULE-SET
    if rule_str.startswith("RULE-SET,"):
        parts = rule_str.split(",")
        if len(parts) >= 3:
            tag = parts[1].strip()
            outbound = map_outbound_name(parts[2].strip())
            return {"rule_set": [tag], "outbound": outbound}
        return None
    
    # Handle MATCH
    if rule_str.startswith("MATCH,"):
        # This is the final/default rule, handled by route.final
        return None
    
    # Handle GEOSITE
    if rule_str.startswith("GEOSITE,"):
        parts = rule_str.split(",")
        if len(parts) >= 3:
            geo_tag = f"geosite-{parts[1].strip().lower()}"
            outbound = map_outbound_name(parts[2].strip())
            return {"rule_set": [geo_tag], "outbound": outbound}
        return None
    
    # Handle GEOIP
    if rule_str.startswith("GEOIP,"):
        parts = rule_str.split(",")
        if len(parts) >= 3:
            geo_name = parts[1].strip().lower()
            outbound = map_outbound_name(parts[2].strip())
            # GEOIP,lan → use ip_is_private directly
            if geo_name == "lan":
                return {"ip_is_private": True, "outbound": outbound}
            geo_tag = f"geoip-{geo_name}"
            return {"rule_set": [geo_tag], "outbound": outbound}
        return None
    
    # Standard rules: TYPE,VALUE,OUTBOUND[,no-resolve]
    parts = rule_str.split(",")
    if len(parts) < 3:
        return None
    
    rule_type = parts[0].strip()
    value = parts[1].strip()
    outbound = map_outbound_name(parts[2].strip())
    
    rule = {"outbound": outbound}
    
    if rule_type == "DOMAIN":
        rule["domain"] = [value]
    elif rule_type == "DOMAIN-SUFFIX":
        # Remove trailing spaces
        value = value.strip()
        rule["domain_suffix"] = [value]
    elif rule_type == "DOMAIN-KEYWORD":
        rule["domain_keyword"] = [value]
    elif rule_type == "DOMAIN-REGEX":
        rule["domain_regex"] = [value]
    elif rule_type == "IP-CIDR" or rule_type == "IP-CIDR6":
        rule["ip_cidr"] = [value]
    elif rule_type == "DST-PORT":
        try:
            rule["port"] = [int(value)]
        except ValueError:
            rule["port_range"] = [value]
    elif rule_type == "SRC-PORT":
        try:
            rule["source_port"] = [int(value)]
        except ValueError:
            rule["source_port_range"] = [value]
    elif rule_type == "PROCESS-NAME":
        rule["process_name"] = [value]
    elif rule_type == "PROCESS-NAME-REGEX":
        rule["process_path_regex"] = [value]
    elif rule_type == "PROCESS-PATH":
        rule["process_path"] = [value]
    elif rule_type == "PROCESS-PATH-REGEX":
        rule["process_path_regex"] = [value]
    elif rule_type == "NETWORK":
        rule["network"] = [value]
    elif rule_type == "IP-SUFFIX":
        rule["ip_cidr"] = [value]
    elif rule_type == "SRC-IP-CIDR":
        rule["source_ip_cidr"] = [value]
    elif rule_type == "IN-PORT":
        rule["inbound"] = [value]  # not exact match but close
    elif rule_type == "DOMAIN-WILDCARD":
        if value.startswith("*."):
            rule["domain_suffix"] = [value[2:]]
        else:
            regex = value.replace(".", "\\.").replace("*", ".*")
            rule["domain_regex"] = ["^" + regex + "$"]
    else:
        print(f"WARNING: Unsupported rule type '{rule_type}' in rule: {rule_str}", file=sys.stderr)
        return None
    
    return rule

def parse_logical_rule(rule_str):
    """Parse AND/OR/NOT compound rules - simplified conversion"""
    # These are complex, just skip them for now and log
    print(f"WARNING: Logical rule not fully converted: {rule_str}", file=sys.stderr)
    return None

def map_outbound_name(name):
    """Map mihomo outbound names to sing-box names"""
    if name == "DIRECT":
        return "direct"
    elif name == "REJECT":
        return "block"
    return name

def build_dns_config(mihomo_dns):
    """Convert mihomo DNS config to sing-box 1.12+ new DNS format"""
    dns = {
        "servers": [
            {
                "type": "https",
                "tag": "dns-direct",
                "server": "doh.pub",
                "server_port": 443,
                "path": "/dns-query",
                "detour": "direct",
                "domain_resolver": "dns-bootstrap",
            },
            {
                "type": "https",
                "tag": "dns-direct-ali",
                "server": "dns.alidns.com",
                "server_port": 443,
                "path": "/dns-query",
                "detour": "direct",
                "domain_resolver": "dns-bootstrap",
            },
            {
                "type": "https",
                "tag": "dns-remote",
                "server": "dns.cloudflare.com",
                "server_port": 443,
                "path": "/dns-query",
                "detour": "🔰 节点选择",
                "domain_resolver": "dns-bootstrap",
            },
            {
                "type": "https",
                "tag": "dns-remote-google",
                "server": "dns.google",
                "server_port": 443,
                "path": "/dns-query",
                "detour": "🔰 节点选择",
                "domain_resolver": "dns-bootstrap",
            },
            {
                "type": "fakeip",
                "tag": "dns-fakeip",
                "inet4_range": "198.18.0.0/15",
                "inet6_range": "fc00::/18",
            },
            {
                "type": "udp",
                "tag": "dns-bootstrap",
                "server": "223.5.5.5",
                "detour": "direct",
            },
        ],
        "rules": [
            {
                "clash_mode": "Direct",
                "server": "dns-direct",
            },
            {
                "clash_mode": "Global",
                "server": "dns-remote",
            },
            {
                "rule_set": ["geosite-cn"],
                "server": "dns-direct",
            },
            {
                "rule_set": ["geosite-gfw", "geosite-geolocation-!cn"],
                "server": "dns-fakeip",
            },
        ],
        "final": "dns-remote",
        "strategy": "prefer_ipv4",
        "independent_cache": True,
    }
    return dns

def build_inbounds(testing_mode=True):
    """Build sing-box inbounds"""
    inbounds = []
    
    if testing_mode:
        # Use different ports to avoid conflict with running mihomo
        inbounds.append({
            "type": "mixed",
            "tag": "mixed-in",
            "listen": "::",
            "listen_port": 17893,
            "set_system_proxy": False,
        })
    else:
        inbounds.append({
            "type": "tun",
            "tag": "tun-in",
            "inet4_address": "172.19.0.1/30",
            "inet6_address": "fdfe:dcba:9876::1/126",
            "auto_route": True,
            "strict_route": True,
        })
        inbounds.append({
            "type": "mixed",
            "tag": "mixed-in",
            "listen": "::",
            "listen_port": 17893,
        })
    
    return inbounds

def build_geosite_geoip_rule_sets():
    """Build rule_set entries for geosite and geoip references"""
    # All geosite/geoip tags used in rules
    geosite_tags = [
        "github", "twitter", "youtube", "google", "telegram",
        "netflix", "bilibili", "spotify", "cn", "geolocation-!cn",
        "gfw",  # for gfw-ruleset replacement
    ]
    geoip_tags = [
        "twitter", "google", "telegram", "netflix", "cn",
    ]
    
    rule_sets = []
    
    for tag in geosite_tags:
        url_tag = tag
        rule_sets.append({
            "type": "remote",
            "tag": f"geosite-{tag}",
            "format": "binary",
            "url": f"https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geosite/{url_tag}.srs",
            "download_detour": "direct",
            "update_interval": "7d",
        })
    
    for tag in geoip_tags:
        rule_sets.append({
            "type": "remote",
            "tag": f"geoip-{tag}",
            "format": "binary",
            "url": f"https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/{tag}.srs",
            "download_detour": "direct",
            "update_interval": "7d",
        })
    
    return rule_sets

def build_custom_rule_sets(rule_providers):
    """Build rule_set entries for mihomo rule-providers"""
    import urllib.parse
    
    rule_sets = []
    
    for name, rp in rule_providers.items():
        url = rp.get("url", "")
        behavior = rp.get("behavior", "classical")
        fmt = rp.get("format", "")
        
        if not url:
            continue
        
        # Skip .mrs files - need alternative
        if url.endswith(".mrs") or fmt == "mrs":
            # Use alternative sources for adblock
            if "217heidai" in url or "adblock" in url.lower():
                # Use the YAML version instead via converter
                alt_url = url.replace(".mrs", ".yaml").replace("adblockmihomo.yaml", "adblockmihomolite.yaml")
                rule_sets.append({
                    "type": "remote",
                    "tag": name,
                    "format": "source",
                    "url": f"{CONVERTER_BASE}?url={urllib.parse.quote(alt_url, safe='')}&behavior=domain",
                    "download_detour": "direct",
                    "update_interval": "24h",
                })
            elif "anti-ad" in url.lower() or "anti-AD" in url:
                # Use anti-ad clash yaml version
                alt_url = "https://raw.githubusercontent.com/privacy-protection-tools/anti-AD/master/anti-ad-clash.yaml"
                alt_url_cdn = f"https://krcdn.lovis.us/{alt_url}"
                rule_sets.append({
                    "type": "remote",
                    "tag": name,
                    "format": "source",
                    "url": f"{CONVERTER_BASE}?url={urllib.parse.quote(alt_url_cdn, safe='')}&behavior=domain",
                    "download_detour": "direct",
                    "update_interval": "24h",
                })
            continue
        
        # For all other rule-providers, use the converter
        converter_url = f"{CONVERTER_BASE}?url={urllib.parse.quote(url, safe='')}&behavior={behavior}"
        rule_sets.append({
            "type": "remote",
            "tag": name,
            "format": "source",
            "url": converter_url,
            "download_detour": "direct",
            "update_interval": "24h",
        })
    
    return rule_sets

def main():
    config_path = "nohkmanual-llf-172166.masked.yml"
    output_path = "singbox-config.json"
    
    print(f"Loading mihomo config from {config_path}...")
    mihomo = load_mihomo_config(config_path)
    
    # ===== OUTBOUNDS =====
    outbounds = []
    
    # Special outbounds
    outbounds.append({"type": "direct", "tag": "direct"})
    # NOTE: block outbound kept because proxy groups reference it as a member
    outbounds.append({"type": "block", "tag": "block"})
    
    # Convert individual proxies
    proxy_tags = set()
    issues = []
    
    for p in mihomo.get("proxies", []):
        sb = convert_proxy(p)
        if sb:
            outbounds.append(sb)
            proxy_tags.add(sb["tag"])
        else:
            issues.append(f"Skipped proxy: {p.get('name', 'unknown')} (type: {p.get('type', 'unknown')})")
    
    print(f"Converted {len(proxy_tags)} proxies")
    
    # Convert proxy groups
    group_tags = set()
    provider_groups = []
    
    for g in mihomo.get("proxy-groups", []):
        sb_group = convert_group(g, proxy_tags, group_tags)
        if sb_group:
            outbounds.append(sb_group)
            group_tags.add(sb_group["tag"])
            if g.get("use"):
                provider_groups.append(g["name"])
    
    print(f"Converted {len(group_tags)} proxy groups")
    if provider_groups:
        print(f"WARNING: Groups using proxy-providers (subscription not loaded): {provider_groups}")
    
    # ===== ROUTE =====
    route = {
        "auto_detect_interface": True,
        "final": "🔰 节点选择",
        "default_domain_resolver": "dns-direct",
        "rules": [],
        "rule_set": [],
    }
    
    # Sniff + DNS hijack rules (must be first, replaces legacy inbound sniff + dns outbound)
    route["rules"].insert(0, {"action": "sniff"})
    route["rules"].insert(1, {"protocol": "dns", "action": "hijack-dns"})
    
    # Convert mihomo rules
    for rule_str in mihomo.get("rules", []):
        sb_rule = parse_mihomo_rule(rule_str)
        if sb_rule:
            route["rules"].append(sb_rule)
    
    # Build rule_set entries
    # 1. Custom rule-providers via converter
    custom_rule_sets = build_custom_rule_sets(mihomo.get("rule-providers", {}))
    route["rule_set"].extend(custom_rule_sets)
    
    # 2. GeoSite/GeoIP rule sets
    geo_rule_sets = build_geosite_geoip_rule_sets()
    route["rule_set"].extend(geo_rule_sets)
    
    # No need to map geoip-lan - handled as ip_is_private in rule parser
    
    # Also need gfw-ruleset - it's both a rule-provider AND referenced as RULE-SET
    # The rule-provider version is already in custom_rule_sets as "gfw-ruleset"
    # But GEOSITE,gfw also creates a "geosite-gfw" reference
    # The RULE-SET,gfw-ruleset references the rule-provider, which is fine
    
    print(f"Generated {len(route['rules'])} route rules, {len(route['rule_set'])} rule sets")
    
    # ===== DNS =====
    dns = build_dns_config(mihomo.get("dns", {}))
    
    # ===== INBOUNDS =====
    inbounds = build_inbounds(testing_mode=True)
    
    # ===== EXPERIMENTAL =====
    experimental = {
        "clash_api": {
            "external_controller": "0.0.0.0:19090",
            "external_ui": "ui",
            "secret": "123456",
            "default_mode": "Rule",
        },
        "cache_file": {
            "enabled": True,
            "store_fakeip": True,
        },
    }
    
    # ===== ASSEMBLE =====
    singbox_config = {
        "log": {
            "level": "info",
            "timestamp": True,
        },
        "dns": dns,
        "inbounds": inbounds,
        "outbounds": outbounds,
        "route": route,
        "experimental": experimental,
    }
    
    # Write output
    with open(output_path, 'w', encoding='utf-8') as f:
        json.dump(singbox_config, f, ensure_ascii=False, indent=2)
    
    print(f"\nSing-box config written to {output_path}")
    print(f"\nSummary:")
    print(f"  Proxies: {len(proxy_tags)}")
    print(f"  Groups: {len(group_tags)}")
    print(f"  Route rules: {len(route['rules'])}")
    print(f"  Rule sets: {len(route['rule_set'])}")
    
    if issues:
        print(f"\nIssues:")
        for issue in issues:
            print(f"  - {issue}")
    
    if provider_groups:
        print(f"\nProxy-provider groups (subscription nodes NOT loaded, using DIRECT fallback):")
        for pg in provider_groups:
            print(f"  - {pg}")

if __name__ == "__main__":
    main()
