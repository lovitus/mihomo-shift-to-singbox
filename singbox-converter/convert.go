package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConversionReport holds stats about the conversion
type ConversionReport struct {
	ProxyCount     int
	GroupCount     int
	RuleCount      int
	RuleSetCount   int
	Issues         []string
	ProviderGroups []string
}

// M is a shorthand for map[string]interface{}
type M = map[string]interface{}

// ConvertMihomoToSingbox converts mihomo YAML config bytes to a sing-box config map
func ConvertMihomoToSingbox(yamlData []byte, converterBaseURL string, useFallback bool, removeEmoji bool) (M, *ConversionReport, error) {
	var mihomo M
	if err := yaml.Unmarshal(yamlData, &mihomo); err != nil {
		return nil, nil, fmt.Errorf("yaml parse error: %w", err)
	}

	report := &ConversionReport{}

	finalOutbound := DefaultFinalOutbound
	if removeEmoji {
		finalOutbound = cleanTag(finalOutbound)
	}

	// ===== OUTBOUNDS =====
	outbounds := []M{
		{"type": "direct", "tag": "direct"},
		// block kept for proxy groups that reference it
		{"type": "block", "tag": "block"},
	}

	// Convert proxies
	proxyTags := map[string]bool{}
	for _, raw := range getSlice(mihomo, "proxies") {
		p := toM(raw)
		if p == nil {
			continue
		}
		sb := convertProxy(p)
		if sb != nil {
			if removeEmoji {
				if tag, ok := sb["tag"].(string); ok {
					sb["tag"] = cleanTag(tag)
				}
				if detour, ok := sb["detour"].(string); ok {
					sb["detour"] = cleanTag(detour)
				}
			}
			outbounds = append(outbounds, sb)
			proxyTags[getString(sb, "tag")] = true
		} else {
			report.Issues = append(report.Issues, fmt.Sprintf("Skipped proxy: %s (type: %s)", getString(p, "name"), getString(p, "type")))
		}
	}
	report.ProxyCount = len(proxyTags)

	// Convert proxy groups
	groupTags := map[string]bool{}
	for _, raw := range getSlice(mihomo, "proxy-groups") {
		g := toM(raw)
		if g == nil {
			continue
		}
		sbGroup := convertGroup(g, useFallback)
		if sbGroup != nil {
			if removeEmoji {
				if tag, ok := sbGroup["tag"].(string); ok {
					sbGroup["tag"] = cleanTag(tag)
				}
				if defaultOut, ok := sbGroup["default"].(string); ok {
					sbGroup["default"] = cleanTag(defaultOut)
				}
				if outboundsSlice, ok := sbGroup["outbounds"].([]interface{}); ok {
					for idx, o := range outboundsSlice {
						if obStr, ok2 := o.(string); ok2 {
							outboundsSlice[idx] = cleanTag(obStr)
						}
					}
				}
			}
			outbounds = append(outbounds, sbGroup)
			groupTags[getString(sbGroup, "tag")] = true
			if getSlice(g, "use") != nil {
				report.ProviderGroups = append(report.ProviderGroups, getString(g, "name"))
			}
		}
	}
	report.GroupCount = len(groupTags)

	// ===== ROUTE =====
	route := M{
		"auto_detect_interface":   true,
		"final":                   finalOutbound,
		"default_domain_resolver": "dns-direct",
		"rules":                   []M{},
		"rule_set":                []M{},
	}

	// Sniff + DNS hijack (replaces legacy inbound sniff + dns outbound)
	rules := []M{
		{"action": "sniff"},
		{"protocol": "dns", "action": "hijack-dns"},
	}

	// Convert mihomo rules
	for _, raw := range getSlice(mihomo, "rules") {
		ruleStr, ok := raw.(string)
		if !ok {
			continue
		}
		sbRule := parseMihomoRule(ruleStr)
		if sbRule != nil {
			if removeEmoji {
				if outbound, ok := sbRule["outbound"].(string); ok {
					sbRule["outbound"] = cleanTag(outbound)
				}
			}
			rules = append(rules, sbRule)
		}
	}
	route["rules"] = rules
	report.RuleCount = len(rules)

	// Build rule_set entries
	ruleSets := []M{}

	// Custom rule-providers via converter
	ruleProviders := getMap(mihomo, "rule-providers")
	customRuleSets := buildCustomRuleSets(ruleProviders, converterBaseURL)
	ruleSets = append(ruleSets, customRuleSets...)

	// GeoSite/GeoIP rule sets
	geoRuleSets := buildGeoRuleSets()
	ruleSets = append(ruleSets, geoRuleSets...)

	route["rule_set"] = ruleSets
	report.RuleSetCount = len(ruleSets)

	// ===== DNS =====
	dns := buildDNSConfig(finalOutbound)

	// ===== INBOUNDS =====
	inbounds := buildInbounds(true)

	// ===== EXPERIMENTAL =====
	experimental := M{
		"clash_api": M{
			"external_controller": "0.0.0.0:19090",
			"external_ui":         "ui",
			"secret":              "123456",
			"default_mode":        "Rule",
		},
		"cache_file": M{
			"enabled":      true,
			"store_fakeip": true,
		},
	}

	// ===== ASSEMBLE =====
	config := M{
		"log": M{
			"level":     "info",
			"timestamp": true,
		},
		"dns":          dns,
		"inbounds":     inbounds,
		"outbounds":    outbounds,
		"route":        route,
		"experimental": experimental,
	}

	log.Printf("Converted %d proxies, %d groups, %d rules, %d rule-sets",
		report.ProxyCount, report.GroupCount, report.RuleCount, report.RuleSetCount)

	return config, report, nil
}

// ===== PROXY CONVERSION =====

func convertProxy(p M) M {
	ptype := getString(p, "type")
	switch ptype {
	case "ss":
		return convertSS(p)
	case "vmess":
		return convertVMess(p)
	case "socks5":
		return convertSocks5(p)
	case "hysteria":
		return convertHysteria(p)
	case "hysteria2":
		return convertHysteria2(p)
	case "ssh":
		return convertSSH(p)
	default:
		log.Printf("WARNING: unsupported proxy type '%s' for '%s'", ptype, getString(p, "name"))
		return nil
	}
}

func convertSS(p M) M {
	out := M{
		"type":        "shadowsocks",
		"tag":         getString(p, "name"),
		"server":      getString(p, "server"),
		"server_port": getInt(p, "port"),
		"method":      getString(p, "cipher"),
		"password":    getStringDefault(p, "password", ""),
	}
	if getBool(p, "udp") {
		out["udp_over_tcp"] = false
	}
	if dp := getString(p, "dialer-proxy"); dp != "" {
		out["detour"] = dp
	}
	return out
}

func convertVMess(p M) M {
	out := M{
		"type":        "vmess",
		"tag":         getString(p, "name"),
		"server":      getString(p, "server"),
		"server_port": getInt(p, "port"),
		"uuid":        getString(p, "uuid"),
		"alter_id":    getIntDefault(p, "alterId", 0),
		"security":    getStringDefault(p, "cipher", "auto"),
	}
	if getBool(p, "tls") {
		tls := M{
			"enabled":  true,
			"insecure": getBool(p, "skip-cert-verify"),
		}
		if sn := getString(p, "servername"); sn != "" {
			tls["server_name"] = sn
		}
		out["tls"] = tls
	}
	if getString(p, "network") == "ws" {
		wsOpts := getMap(p, "ws-opts")
		transport := M{"type": "ws"}
		if path := getString(wsOpts, "path"); path != "" {
			transport["path"] = path
		}
		headers := getMap(wsOpts, "headers")
		if host := getString(headers, "Host"); host != "" {
			transport["headers"] = M{"Host": []string{host}}
		}
		out["transport"] = transport
	}
	if dp := getString(p, "dialer-proxy"); dp != "" {
		out["detour"] = dp
	}
	return out
}

func convertSocks5(p M) M {
	out := M{
		"type":        "socks",
		"tag":         getString(p, "name"),
		"server":      getString(p, "server"),
		"server_port": getInt(p, "port"),
		"version":     "5",
	}
	if u := getString(p, "username"); u != "" {
		out["username"] = u
	}
	if pw := getString(p, "password"); pw != "" {
		out["password"] = pw
	}
	if getBool(p, "udp") {
		out["udp_over_tcp"] = false
	}
	if dp := getString(p, "dialer-proxy"); dp != "" {
		out["detour"] = dp
	}
	return out
}

func convertHysteria(p M) M {
	out := M{
		"type":   "hysteria",
		"tag":    getString(p, "name"),
		"server": getString(p, "server"),
	}

	// Handle port hopping - use first port only
	portsStr := getStringDefault(p, "ports", getStringDefault(p, "port", "443"))
	out["server_port"] = parseFirstPort(portsStr)

	out["up_mbps"] = parseMbps(p, "up", 50)
	out["down_mbps"] = parseMbps(p, "down", 300)

	if obfs := getString(p, "obfs"); obfs != "" {
		out["obfs"] = obfs
	}
	tls := M{
		"enabled":  true,
		"insecure": getBoolDefault(p, "skip-cert-verify", true),
	}
	if sni := getString(p, "sni"); sni != "" {
		tls["server_name"] = sni
	}
	out["tls"] = tls

	if dp := getString(p, "dialer-proxy"); dp != "" {
		out["detour"] = dp
	}
	return out
}

func convertHysteria2(p M) M {
	out := M{
		"type":        "hysteria2",
		"tag":         getString(p, "name"),
		"server":      getString(p, "server"),
		"server_port": getIntDefault(p, "port", 443),
		"password":    getStringDefault(p, "password", ""),
	}
	out["up_mbps"] = parseMbps(p, "up", 30)
	out["down_mbps"] = parseMbps(p, "down", 300)

	tls := M{
		"enabled":  true,
		"insecure": getBool(p, "skip-cert-verify"),
	}
	if sni := getString(p, "sni"); sni != "" {
		tls["server_name"] = sni
	}
	if alpn := getSlice(p, "alpn"); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	out["tls"] = tls

	if dp := getString(p, "dialer-proxy"); dp != "" {
		out["detour"] = dp
	}
	return out
}

func convertSSH(p M) M {
	out := M{
		"type":        "ssh",
		"tag":         getString(p, "name"),
		"server":      getString(p, "server"),
		"server_port": getIntDefault(p, "port", 22),
		"user":        getStringDefault(p, "username", ""),
		"password":    getStringDefault(p, "password", ""),
	}
	if pk := getString(p, "private-key"); pk != "" {
		out["private_key"] = pk
	}
	if pkp := getString(p, "private-key-passphrase"); pkp != "" {
		out["private_key_passphrase"] = pkp
	}
	if hk := getString(p, "host-key"); hk != "" {
		out["host_key"] = hk
	}
	if hka := getSlice(p, "host-key-algorithms"); len(hka) > 0 {
		out["host_key_algorithms"] = hka
	}
	if dp := getString(p, "dialer-proxy"); dp != "" {
		out["detour"] = dp
	}
	return out
}

// ===== GROUP CONVERSION =====

func convertGroup(g M, useFallback bool) M {
	mihomoType := getStringDefault(g, "type", "select")
	sbType := mapGroupType(mihomoType, useFallback)
	tag := getString(g, "name")

	out := M{
		"type": sbType,
		"tag":  tag,
	}

	// Build outbounds list
	var outbounds []interface{}
	for _, raw := range getSlice(g, "proxies") {
		name, ok := raw.(string)
		if !ok {
			continue
		}
		switch name {
		case "DIRECT":
			outbounds = append(outbounds, "direct")
		case "REJECT":
			outbounds = append(outbounds, "block")
		default:
			outbounds = append(outbounds, name)
		}
	}

	// For groups using proxy-providers with no proxies, add direct placeholder
	if getSlice(g, "use") != nil && len(outbounds) == 0 {
		outbounds = append(outbounds, "direct")
	} else if len(outbounds) == 0 {
		outbounds = append(outbounds, "direct")
	}

	out["outbounds"] = outbounds

	if sbType == "urltest" || sbType == "fallback" {
		testURL := getStringDefault(g, "url", "https://www.gstatic.com/generate_204")
		interval := getIntDefault(g, "interval", 300)

		// Cap interval at 3600s
		if interval > 3600 {
			interval = 3600
		}

		out["url"] = testURL
		out["interval"] = fmt.Sprintf("%ds", interval)

		if sbType == "urltest" {
			tolerance := getIntDefault(g, "tolerance", 150)
			out["tolerance"] = tolerance
		}

		if sbType == "fallback" {
			out["fail_threshold"] = 3
			out["recover_threshold"] = 3
		}

		// idle_timeout for lazy groups - must be >= interval
		if getBool(g, "lazy") {
			idleSec := interval * 2
			if idleSec < 1800 {
				idleSec = 1800
			}
			out["idle_timeout"] = fmt.Sprintf("%ds", idleSec)
		}
	}

	if sbType == "selector" && len(outbounds) > 0 {
		out["default"] = outbounds[0]
	}

	return out
}

func mapGroupType(t string, useFallback bool) string {
	switch t {
	case "select":
		return "selector"
	case "url-test":
		return "urltest"
	case "fallback":
		if useFallback {
			return "fallback"
		}
		return "urltest"
	default:
		return "selector"
	}
}

// ===== HELPERS =====

func parseFirstPort(s string) int {
	s = strings.TrimSpace(s)
	// Handle "33891,65520-65530" → 33891
	parts := strings.Split(s, ",")
	first := strings.TrimSpace(parts[0])
	if idx := strings.Index(first, "-"); idx >= 0 {
		first = first[:idx]
	}
	port, _ := strconv.Atoi(first)
	if port == 0 {
		port = 443
	}
	return port
}

var mbpsRe = regexp.MustCompile(`(\d+)`)

func parseMbps(p M, key string, defaultVal int) int {
	s := getString(p, key)
	if s == "" {
		return defaultVal
	}
	m := mbpsRe.FindString(s)
	if m == "" {
		return defaultVal
	}
	v, _ := strconv.Atoi(m)
	if v == 0 {
		return defaultVal
	}
	return v
}

// Generic map helpers
func getString(m M, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func getStringDefault(m M, key, def string) string {
	s := getString(m, key)
	if s == "" {
		return def
	}
	return s
}

func getInt(m M, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(strings.TrimSuffix(val, "s"))
		return n
	default:
		n, _ := strconv.Atoi(fmt.Sprintf("%v", v))
		return n
	}
}

func getIntDefault(m M, key string, def int) int {
	v := getInt(m, key)
	if v == 0 {
		return def
	}
	return v
}

func getBool(m M, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1"
	default:
		return false
	}
}

func getBoolDefault(m M, key string, def bool) bool {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	b, isBool := v.(bool)
	if isBool {
		return b
	}
	return def
}

func getSlice(m M, key string) []interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return s
}

func getMap(m M, key string) M {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	return toM(v)
}

func toM(v interface{}) M {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case map[string]interface{}:
		return val
	case map[interface{}]interface{}:
		// yaml.v3 might produce this
		result := M{}
		for k, v := range val {
			result[fmt.Sprintf("%v", k)] = v
		}
		return result
	default:
		return nil
	}
}

var emojiStripRegex = regexp.MustCompile(`[^\p{L}\p{N}\p{P}\p{Z}\p{Sm}\p{Sc}\p{Sk}]+`)
var spaceCleanupRegex = regexp.MustCompile(`\s+`)

func cleanTag(tag string) string {
	cleaned := emojiStripRegex.ReplaceAllString(tag, "")
	cleaned = spaceCleanupRegex.ReplaceAllString(cleaned, " ")
	return strings.TrimSpace(cleaned)
}
