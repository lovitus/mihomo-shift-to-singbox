package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SingboxRuleSet represents a sing-box rule-set source format
type SingboxRuleSet struct {
	Version int              `json:"version"`
	Rules   []SingboxRuleObj `json:"rules"`
}

// SingboxRuleObj represents a single rule object in a sing-box rule-set
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

var (
	cache     = make(map[string]*cachedResult)
	cacheLock sync.RWMutex
)

type cachedResult struct {
	data      []byte
	fetchedAt time.Time
}

func main() {
	mux := http.NewServeMux()

	// /convert?url=<encoded_url>&behavior=<classical|domain|ipcidr>
	mux.HandleFunc("/convert", handleConvert)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := "127.0.0.1:8081"
	log.Printf("Rule converter listening on %s", addr)
	log.Printf("Usage: GET /convert?url=<encoded_mihomo_rule_url>&behavior=<classical|domain|ipcidr>")
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	behavior := r.URL.Query().Get("behavior")

	if rawURL == "" {
		http.Error(w, `{"error":"missing 'url' parameter"}`, http.StatusBadRequest)
		return
	}
	if behavior == "" {
		behavior = "classical" // default
	}

	// Validate behavior
	switch behavior {
	case "classical", "domain", "ipcidr":
	default:
		http.Error(w, `{"error":"invalid behavior, must be classical|domain|ipcidr"}`, http.StatusBadRequest)
		return
	}

	cacheKey := rawURL + "|" + behavior

	// Check cache (1 hour TTL)
	cacheLock.RLock()
	if cached, ok := cache[cacheKey]; ok && time.Since(cached.fetchedAt) < time.Hour {
		cacheLock.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "hit")
		w.Write(cached.data)
		return
	}
	cacheLock.RUnlock()

	// Fetch the remote rule file
	decodedURL, err := url.QueryUnescape(rawURL)
	if err != nil {
		decodedURL = rawURL
	}

	log.Printf("Fetching: %s (behavior: %s)", decodedURL, behavior)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(decodedURL)
	if err != nil {
		log.Printf("Fetch error: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":"failed to fetch: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to read body: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Parse and convert
	ruleSet, err := convertRules(string(body), behavior)
	if err != nil {
		log.Printf("Convert error for %s: %v", decodedURL, err)
		http.Error(w, fmt.Sprintf(`{"error":"conversion failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	jsonData, err := json.MarshalIndent(ruleSet, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"json marshal failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Cache the result
	cacheLock.Lock()
	cache[cacheKey] = &cachedResult{data: jsonData, fetchedAt: time.Now()}
	cacheLock.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "miss")
	w.Write(jsonData)
}

func convertRules(content string, behavior string) (*SingboxRuleSet, error) {
	// Extract payload lines
	lines := extractPayloadLines(content)

	ruleObj := SingboxRuleObj{}

	switch behavior {
	case "domain":
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Remove surrounding quotes
			line = strings.Trim(line, "'\"")
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, "+.") {
				// domain suffix: +.example.com -> example.com
				ruleObj.DomainSuffix = append(ruleObj.DomainSuffix, line[2:])
			} else if strings.HasPrefix(line, "*.") {
				// wildcard: *.example.com -> treat as domain suffix
				ruleObj.DomainSuffix = append(ruleObj.DomainSuffix, line[2:])
			} else if strings.HasPrefix(line, ".") {
				// .example.com -> domain suffix
				ruleObj.DomainSuffix = append(ruleObj.DomainSuffix, line[1:])
			} else {
				// full domain match - also add as suffix for compatibility
				ruleObj.Domain = append(ruleObj.Domain, line)
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
			// Remove ,no-resolve suffix if present
			line = strings.TrimSuffix(line, ",no-resolve")
			line = strings.TrimSpace(line)
			ruleObj.IPCidr = append(ruleObj.IPCidr, line)
		}
	case "classical":
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Remove leading "- " if present
			line = strings.TrimPrefix(line, "- ")
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			parseClassicalRule(line, &ruleObj)
		}
	}

	// Build the rule set
	result := &SingboxRuleSet{
		Version: 2,
		Rules:   []SingboxRuleObj{ruleObj},
	}

	return result, nil
}

func parseClassicalRule(line string, obj *SingboxRuleObj) {
	// Remove surrounding quotes
	line = strings.Trim(line, "'\"")
	// Classical format: TYPE,VALUE or TYPE,VALUE,no-resolve
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
		// Try to parse as port or port range
		if strings.Contains(value, "-") {
			obj.PortRange = append(obj.PortRange, value)
		} else {
			// Parse as uint16
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
		// Convert wildcard to regex: *.example.com -> .*\.example\.com
		// Simple conversion: treat as domain suffix if it starts with *.
		if strings.HasPrefix(value, "*.") {
			obj.DomainSuffix = append(obj.DomainSuffix, value[2:])
		} else {
			// Generic wildcard -> regex
			regex := strings.ReplaceAll(value, ".", "\\.")
			regex = strings.ReplaceAll(regex, "*", ".*")
			obj.DomainRegex = append(obj.DomainRegex, "^"+regex+"$")
		}
	default:
		log.Printf("Unsupported rule type: %s (full line: %s)", ruleType, line)
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
			// Check if we've left the payload section (non-indented, non-empty line that's not a list item)
			if trimmed != "" && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") {
				// Might be a new YAML key - stop
				if strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "- ") {
					break
				}
			}

			if strings.HasPrefix(trimmed, "- ") {
				val := strings.TrimPrefix(trimmed, "- ")
				val = strings.TrimSpace(val)
				lines = append(lines, val)
			} else if strings.HasPrefix(trimmed, "-") && len(trimmed) > 1 {
				val := strings.TrimPrefix(trimmed, "-")
				val = strings.TrimSpace(val)
				lines = append(lines, val)
			}
		}
	}

	return lines
}
