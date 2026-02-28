package main

import (
	"bufio"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ===== RULE PARSING =====

func parseMihomoRule(ruleStr string) M {
	ruleStr = strings.TrimSpace(ruleStr)
	if ruleStr == "" || strings.HasPrefix(ruleStr, "#") {
		return nil
	}

	// AND/OR/NOT compound rules - skip for now
	if strings.HasPrefix(ruleStr, "AND,") || strings.HasPrefix(ruleStr, "OR,") || strings.HasPrefix(ruleStr, "NOT,") {
		log.Printf("WARNING: logical rule not converted: %s", ruleStr)
		return nil
	}

	// RULE-SET
	if strings.HasPrefix(ruleStr, "RULE-SET,") {
		parts := strings.SplitN(ruleStr, ",", 4)
		if len(parts) >= 3 {
			return M{"rule_set": []string{strings.TrimSpace(parts[1])}, "outbound": mapOutbound(strings.TrimSpace(parts[2]))}
		}
		return nil
	}

	// MATCH - handled by route.final
	if strings.HasPrefix(ruleStr, "MATCH,") {
		return nil
	}

	// GEOSITE
	if strings.HasPrefix(ruleStr, "GEOSITE,") {
		parts := strings.SplitN(ruleStr, ",", 4)
		if len(parts) >= 3 {
			tag := "geosite-" + strings.ToLower(strings.TrimSpace(parts[1]))
			return M{"rule_set": []string{tag}, "outbound": mapOutbound(strings.TrimSpace(parts[2]))}
		}
		return nil
	}

	// GEOIP
	if strings.HasPrefix(ruleStr, "GEOIP,") {
		parts := strings.SplitN(ruleStr, ",", 4)
		if len(parts) >= 3 {
			geoName := strings.ToLower(strings.TrimSpace(parts[1]))
			outbound := mapOutbound(strings.TrimSpace(parts[2]))
			if geoName == "lan" {
				return M{"ip_is_private": true, "outbound": outbound}
			}
			return M{"rule_set": []string{"geoip-" + geoName}, "outbound": outbound}
		}
		return nil
	}

	// Standard: TYPE,VALUE,OUTBOUND[,no-resolve]
	parts := strings.SplitN(ruleStr, ",", 4)
	if len(parts) < 3 {
		return nil
	}

	ruleType := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	outbound := mapOutbound(strings.TrimSpace(parts[2]))

	rule := M{"outbound": outbound}

	switch ruleType {
	case "DOMAIN":
		rule["domain"] = []string{value}
	case "DOMAIN-SUFFIX":
		rule["domain_suffix"] = []string{value}
	case "DOMAIN-KEYWORD":
		rule["domain_keyword"] = []string{value}
	case "DOMAIN-REGEX":
		rule["domain_regex"] = []string{value}
	case "IP-CIDR", "IP-CIDR6":
		rule["ip_cidr"] = []string{value}
	case "DST-PORT":
		if p, err := strconv.Atoi(value); err == nil {
			rule["port"] = []int{p}
		} else {
			rule["port_range"] = []string{value}
		}
	case "SRC-PORT":
		if p, err := strconv.Atoi(value); err == nil {
			rule["source_port"] = []int{p}
		} else {
			rule["source_port_range"] = []string{value}
		}
	case "PROCESS-NAME":
		rule["process_name"] = []string{value}
	case "PROCESS-NAME-REGEX":
		rule["process_path_regex"] = []string{value}
	case "PROCESS-PATH":
		rule["process_path"] = []string{value}
	case "PROCESS-PATH-REGEX":
		rule["process_path_regex"] = []string{value}
	case "NETWORK":
		rule["network"] = []string{value}
	case "IP-SUFFIX":
		rule["ip_cidr"] = []string{value}
	case "SRC-IP-CIDR":
		rule["source_ip_cidr"] = []string{value}
	case "IN-PORT":
		rule["inbound"] = []string{value}
	case "DOMAIN-WILDCARD":
		if strings.HasPrefix(value, "*.") {
			rule["domain_suffix"] = []string{value[2:]}
		} else {
			regex := strings.ReplaceAll(value, ".", "\\.")
			regex = strings.ReplaceAll(regex, "*", ".*")
			rule["domain_regex"] = []string{"^" + regex + "$"}
		}
	default:
		log.Printf("WARNING: unsupported rule type '%s' in: %s", ruleType, ruleStr)
		return nil
	}

	return rule
}

func mapOutbound(name string) string {
	switch name {
	case "DIRECT":
		return "direct"
	case "REJECT":
		return "block"
	default:
		return name
	}
}

// ===== RULE-SET BUILDING =====

func buildCustomRuleSets(ruleProviders M, converterBase string) []M {
	var ruleSets []M

	for name, raw := range ruleProviders {
		rp := toM(raw)
		if rp == nil {
			continue
		}

		rpURL := getString(rp, "url")
		behavior := getStringDefault(rp, "behavior", "classical")

		if rpURL == "" {
			continue
		}

		// For .mrs files, try alternate YAML URLs
		if strings.HasSuffix(rpURL, ".mrs") {
			rpURL = mrsToYamlURL(rpURL)
		}

		converterURL := fmt.Sprintf("%s/ruleset?url=%s&behavior=%s",
			converterBase,
			url.QueryEscape(rpURL),
			behavior,
		)

		ruleSets = append(ruleSets, M{
			"type":            "remote",
			"tag":             name,
			"format":          "source",
			"url":             converterURL,
			"download_detour": "direct",
			"update_interval": "24h",
		})
	}

	return ruleSets
}

func buildGeoRuleSets() []M {
	var ruleSets []M

	for _, tag := range GeositeTagsUsed {
		ruleSets = append(ruleSets, M{
			"type":            "remote",
			"tag":             "geosite-" + tag,
			"format":          "binary",
			"url":             fmt.Sprintf("https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geosite/%s.srs", tag),
			"download_detour": "direct",
			"update_interval": "7d",
		})
	}

	for _, tag := range GeoipTagsUsed {
		ruleSets = append(ruleSets, M{
			"type":            "remote",
			"tag":             "geoip-" + tag,
			"format":          "binary",
			"url":             fmt.Sprintf("https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/%s.srs", tag),
			"download_detour": "direct",
			"update_interval": "7d",
		})
	}

	return ruleSets
}

func mrsToYamlURL(mrsURL string) string {
	// Try to find a YAML alternative for .mrs binary files
	yamlURL := strings.TrimSuffix(mrsURL, ".mrs") + ".yaml"
	return yamlURL
}

// ===== RULE-SET FILE CONVERSION (for /ruleset endpoint) =====

// SingboxRuleSet for JSON output
type SingboxRuleSet struct {
	Version int              `json:"version"`
	Rules   []SingboxRuleObj `json:"rules"`
}

type SingboxRuleObj struct {
	Domain           []string `json:"domain,omitempty"`
	DomainSuffix     []string `json:"domain_suffix,omitempty"`
	DomainKeyword    []string `json:"domain_keyword,omitempty"`
	DomainRegex      []string `json:"domain_regex,omitempty"`
	IPCidr           []string `json:"ip_cidr,omitempty"`
	Port             []uint16 `json:"port,omitempty"`
	PortRange        []string `json:"port_range,omitempty"`
	ProcessName      []string `json:"process_name,omitempty"`
	ProcessNameRegex []string `json:"process_name_regex,omitempty"`
	ProcessPath      []string `json:"process_path,omitempty"`
	ProcessPathRegex []string `json:"process_path_regex,omitempty"`
	SourceIPCidr     []string `json:"source_ip_cidr,omitempty"`
	Network          []string `json:"network,omitempty"`
}

func convertRuleFile(content string, behavior string) (*SingboxRuleSet, error) {
	lines := extractPayloadLines(content)
	obj := SingboxRuleObj{}

	switch behavior {
	case "domain":
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.Trim(line, "'\"")
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "+.") {
				obj.DomainSuffix = append(obj.DomainSuffix, line[2:])
			} else if strings.HasPrefix(line, "*.") {
				obj.DomainSuffix = append(obj.DomainSuffix, line[2:])
			} else if strings.HasPrefix(line, ".") {
				obj.DomainSuffix = append(obj.DomainSuffix, line[1:])
			} else {
				obj.Domain = append(obj.Domain, line)
			}
		}
	case "ipcidr":
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.Trim(line, "'\"")
			if line == "" {
				continue
			}
			line = strings.TrimSuffix(line, ",no-resolve")
			line = strings.TrimSpace(line)
			obj.IPCidr = append(obj.IPCidr, line)
		}
	case "classical":
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "- ")
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parseClassicalRuleLine(line, &obj)
		}
	}

	return &SingboxRuleSet{
		Version: 2,
		Rules:   []SingboxRuleObj{obj},
	}, nil
}

func parseClassicalRuleLine(line string, obj *SingboxRuleObj) {
	line = strings.Trim(line, "'\"")
	parts := strings.SplitN(line, ",", 3)
	if len(parts) < 2 {
		return
	}

	ruleType := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	switch ruleType {
	case "DOMAIN":
		obj.Domain = append(obj.Domain, value)
	case "DOMAIN-SUFFIX":
		obj.DomainSuffix = append(obj.DomainSuffix, value)
	case "DOMAIN-KEYWORD":
		obj.DomainKeyword = append(obj.DomainKeyword, value)
	case "DOMAIN-REGEX":
		obj.DomainRegex = append(obj.DomainRegex, value)
	case "IP-CIDR", "IP-CIDR6":
		obj.IPCidr = append(obj.IPCidr, value)
	case "SRC-IP-CIDR":
		obj.SourceIPCidr = append(obj.SourceIPCidr, value)
	case "DST-PORT":
		if strings.Contains(value, "-") {
			obj.PortRange = append(obj.PortRange, value)
		} else {
			var port uint16
			fmt.Sscanf(value, "%d", &port)
			if port > 0 {
				obj.Port = append(obj.Port, port)
			}
		}
	case "PROCESS-NAME":
		obj.ProcessName = append(obj.ProcessName, value)
	case "PROCESS-NAME-REGEX":
		obj.ProcessNameRegex = append(obj.ProcessNameRegex, value)
	case "PROCESS-PATH":
		obj.ProcessPath = append(obj.ProcessPath, value)
	case "PROCESS-PATH-REGEX":
		obj.ProcessPathRegex = append(obj.ProcessPathRegex, value)
	case "NETWORK":
		obj.Network = append(obj.Network, value)
	case "DOMAIN-WILDCARD":
		if strings.HasPrefix(value, "*.") {
			obj.DomainSuffix = append(obj.DomainSuffix, value[2:])
		} else {
			regex := strings.ReplaceAll(value, ".", "\\.")
			regex = strings.ReplaceAll(regex, "*", ".*")
			obj.DomainRegex = append(obj.DomainRegex, "^"+regex+"$")
		}
	default:
		log.Printf("Unsupported rule type in ruleset: %s (line: %s)", ruleType, line)
	}
}

func extractPayloadLines(content string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	inPayload := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "payload:" {
			inPayload = true
			continue
		}

		if inPayload {
			if trimmed != "" && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") {
				if strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "- ") {
					break
				}
			}
			if strings.HasPrefix(trimmed, "- ") {
				val := strings.TrimPrefix(trimmed, "- ")
				lines = append(lines, strings.TrimSpace(val))
			} else if strings.HasPrefix(trimmed, "-") && len(trimmed) > 1 {
				val := strings.TrimPrefix(trimmed, "-")
				lines = append(lines, strings.TrimSpace(val))
			}
		}
	}

	return lines
}

// ===== RULESET CACHE =====

var (
	rsCache     = make(map[string]*rsCacheEntry)
	rsCacheLock sync.RWMutex
)

type rsCacheEntry struct {
	data      []byte
	fetchedAt time.Time
}

func rulesetCacheGet(key string) []byte {
	rsCacheLock.RLock()
	defer rsCacheLock.RUnlock()
	if e, ok := rsCache[key]; ok && time.Since(e.fetchedAt) < time.Hour {
		return e.data
	}
	return nil
}

func rulesetCacheSet(key string, data []byte) {
	rsCacheLock.Lock()
	defer rsCacheLock.Unlock()
	rsCache[key] = &rsCacheEntry{data: data, fetchedAt: time.Now()}
}
