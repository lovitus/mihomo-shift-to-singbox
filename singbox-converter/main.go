package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

var (
	listenAddr string
	selfURL    string
	httpClient = &http.Client{Timeout: 30 * time.Second}
)

func main() {
	flag.StringVar(&listenAddr, "listen", ":8080", "HTTP listen address")
	flag.StringVar(&selfURL, "self-url", "http://127.0.0.1:8080", "External URL of this converter (used in generated rule-set URLs)")
	flag.Parse()

	mux := http.NewServeMux()

	// GET /convert?url=<mihomo_subscription_url> → returns sing-box JSON config
	mux.HandleFunc("/convert", handleConvertConfig)

	// GET /ruleset?url=<rule_url>&behavior=<classical|domain|ipcidr> → returns sing-box rule-set JSON
	mux.HandleFunc("/ruleset", handleConvertRuleset)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("singbox-converter listening on %s (self-url: %s)", listenAddr, selfURL)
	log.Printf("Endpoints:")
	log.Printf("  GET /convert?url=<mihomo_url>                           → sing-box config JSON")
	log.Printf("  GET /ruleset?url=<rule_url>&behavior=<classical|domain|ipcidr> → sing-box rule-set JSON")
	log.Printf("  GET /health                                             → health check")

	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func handleConvertConfig(w http.ResponseWriter, r *http.Request) {
	useFallback := r.URL.Query().Get("fallback") == "true"

	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		// If no URL, try reading from a local file path
		filePath := r.URL.Query().Get("file")
		if filePath != "" {
			data, err := os.ReadFile(filePath)
			if err != nil {
				httpError(w, fmt.Sprintf("failed to read file: %v", err), http.StatusBadRequest)
				return
			}
			convertAndRespond(w, data, useFallback)
			return
		}
		httpError(w, "missing 'url' parameter", http.StatusBadRequest)
		return
	}

	log.Printf("Converting mihomo config from: %s (fallback=%v)", rawURL, useFallback)

	resp, err := httpClient.Get(rawURL)
	if err != nil {
		httpError(w, fmt.Sprintf("failed to fetch: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		httpError(w, fmt.Sprintf("upstream returned %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		httpError(w, fmt.Sprintf("failed to read body: %v", err), http.StatusBadGateway)
		return
	}

	convertAndRespond(w, body, useFallback)
}

func convertAndRespond(w http.ResponseWriter, yamlData []byte, useFallback bool) {
	result, report, err := ConvertMihomoToSingbox(yamlData, selfURL, useFallback)
	if err != nil {
		httpError(w, fmt.Sprintf("conversion failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Log the report
	log.Printf("Conversion report:")
	log.Printf("  Proxies: %d, Groups: %d, Rules: %d, RuleSets: %d",
		report.ProxyCount, report.GroupCount, report.RuleCount, report.RuleSetCount)
	for _, issue := range report.Issues {
		log.Printf("  ISSUE: %s", issue)
	}
	for _, pg := range report.ProviderGroups {
		log.Printf("  PROVIDER GROUP (no nodes): %s", pg)
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		httpError(w, fmt.Sprintf("json marshal failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Proxy-Count", fmt.Sprintf("%d", report.ProxyCount))
	w.Header().Set("X-Group-Count", fmt.Sprintf("%d", report.GroupCount))
	w.Header().Set("X-Rule-Count", fmt.Sprintf("%d", report.RuleCount))
	w.Write(jsonData)
}

func handleConvertRuleset(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	behavior := r.URL.Query().Get("behavior")

	if rawURL == "" {
		httpError(w, "missing 'url' parameter", http.StatusBadRequest)
		return
	}
	if behavior == "" {
		behavior = "classical"
	}

	switch behavior {
	case "classical", "domain", "ipcidr":
	default:
		httpError(w, "invalid behavior, must be classical|domain|ipcidr", http.StatusBadRequest)
		return
	}

	cacheKey := rawURL + "|" + behavior
	if data := rulesetCacheGet(cacheKey); data != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "hit")
		w.Write(data)
		return
	}

	decodedURL, err := url.QueryUnescape(rawURL)
	if err != nil {
		decodedURL = rawURL
	}

	log.Printf("Fetching ruleset: %s (behavior: %s)", decodedURL, behavior)

	resp, err := httpClient.Get(decodedURL)
	if err != nil {
		httpError(w, fmt.Sprintf("failed to fetch: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		httpError(w, fmt.Sprintf("failed to read body: %v", err), http.StatusBadGateway)
		return
	}

	ruleSet, err := convertRuleFile(string(body), behavior)
	if err != nil {
		httpError(w, fmt.Sprintf("conversion failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonData, err := json.MarshalIndent(ruleSet, "", "  ")
	if err != nil {
		httpError(w, fmt.Sprintf("json marshal failed: %v", err), http.StatusInternalServerError)
		return
	}

	rulesetCacheSet(cacheKey, jsonData)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "miss")
	w.Write(jsonData)
}

func httpError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
