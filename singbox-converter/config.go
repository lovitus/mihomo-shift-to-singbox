package main

// ===== HARDCODED MAPPING TABLES & DEFAULTS =====

// DefaultFinalOutbound is the default route final outbound
var DefaultFinalOutbound = "🔰 节点选择"

// GeositeTagsUsed lists all geosite tags referenced in rules
var GeositeTagsUsed = []string{
	"github", "twitter", "youtube", "google", "telegram",
	"netflix", "bilibili", "spotify", "cn", "geolocation-!cn",
	"gfw",
}

// GeoipTagsUsed lists all geoip tags referenced in rules
var GeoipTagsUsed = []string{
	"twitter", "google", "telegram", "netflix", "cn",
}

// ProxyTypeMap maps mihomo proxy types to sing-box outbound types
var ProxyTypeMap = map[string]string{
	"ss":        "shadowsocks",
	"vmess":     "vmess",
	"socks5":    "socks",
	"hysteria":  "hysteria",
	"hysteria2": "hysteria2",
	"ssh":       "ssh",
	"trojan":    "trojan",
	"vless":     "vless",
}

// GroupTypeMap maps mihomo group types to sing-box outbound types
var GroupTypeMap = map[string]string{
	"select":   "selector",
	"url-test": "urltest",
	"fallback": "urltest", // sing-box has no native fallback; urltest is closest
}

// RuleTypeMap maps mihomo rule types to sing-box route rule fields
var RuleTypeMap = map[string]string{
	"DOMAIN":             "domain",
	"DOMAIN-SUFFIX":      "domain_suffix",
	"DOMAIN-KEYWORD":     "domain_keyword",
	"DOMAIN-REGEX":       "domain_regex",
	"IP-CIDR":            "ip_cidr",
	"IP-CIDR6":           "ip_cidr",
	"DST-PORT":           "port",
	"SRC-PORT":           "source_port",
	"PROCESS-NAME":       "process_name",
	"PROCESS-NAME-REGEX": "process_path_regex",
	"PROCESS-PATH":       "process_path",
	"PROCESS-PATH-REGEX": "process_path_regex",
	"NETWORK":            "network",
	"IP-SUFFIX":          "ip_cidr",
	"SRC-IP-CIDR":        "source_ip_cidr",
}

// ===== DNS CONFIG (sing-box 1.12+ new format) =====

func buildDNSConfig(finalOutbound string) M {
	return M{
		"servers": []M{
			{
				"type":            "https",
				"tag":             "dns-direct",
				"server":          "doh.pub",
				"server_port":     443,
				"path":            "/dns-query",
				"domain_resolver": "dns-bootstrap",
			},
			{
				"type":            "https",
				"tag":             "dns-direct-ali",
				"server":          "dns.alidns.com",
				"server_port":     443,
				"path":            "/dns-query",
				"domain_resolver": "dns-bootstrap",
			},
			{
				"type":            "https",
				"tag":             "dns-remote",
				"server":          "dns.cloudflare.com",
				"server_port":     443,
				"path":            "/dns-query",
				"detour":          finalOutbound,
				"domain_resolver": "dns-bootstrap",
			},
			{
				"type":            "https",
				"tag":             "dns-remote-google",
				"server":          "dns.google",
				"server_port":     443,
				"path":            "/dns-query",
				"detour":          finalOutbound,
				"domain_resolver": "dns-bootstrap",
			},
			{
				"type":        "fakeip",
				"tag":         "dns-fakeip",
				"inet4_range": "198.18.0.0/15",
				"inet6_range": "fc00::/18",
			},
			{
				"type":   "udp",
				"tag":    "dns-bootstrap",
				"server": "223.5.5.5",
			},
		},
		"rules": []M{
			{"clash_mode": "Direct", "server": "dns-direct"},
			{"clash_mode": "Global", "server": "dns-remote"},
			{"rule_set": []string{"geosite-cn"}, "server": "dns-direct"},
			{"rule_set": []string{"geosite-gfw", "geosite-geolocation-!cn"}, "server": "dns-fakeip"},
		},
		"final":             "dns-remote",
		"strategy":          "prefer_ipv4",
		"independent_cache": true,
	}
}

// ===== INBOUNDS =====

func buildInbounds(testingMode bool) []M {
	if testingMode {
		return []M{
			{
				"type":             "mixed",
				"tag":              "mixed-in",
				"listen":           "::",
				"listen_port":      17893,
				"set_system_proxy": false,
			},
		}
	}
	return []M{
		{
			"type":                     "tun",
			"tag":                      "tun-in",
			"inet4_address":            "172.19.0.1/30",
			"auto_route":               true,
			"strict_route":             true,
			"endpoint_independent_nat": true,
		},
		{
			"type":        "mixed",
			"tag":         "mixed-in",
			"listen":      "::",
			"listen_port": 17893,
		},
	}
}
