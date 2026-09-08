// Package metrics provides bot and crawler detection for user agent segmentation.
package metrics

import (
	"strings"
)

// ClientType categorizes a client request based on User-Agent.
type ClientType string

const (
	ClientHuman   ClientType = "human"
	ClientGoodBot ClientType = "good_bot"
	ClientBadBot  ClientType = "bad_bot"
)

// BotTrafficStats holds request counters segmented by client classification.
type BotTrafficStats struct {
	HumanRequests   int64 `json:"human"`
	GoodBotRequests int64 `json:"good_bot"`
	BadBotRequests  int64 `json:"bad_bot"`
}

// Known good search engine crawlers and monitoring bots (lowercase substrings)
var goodBotSignatures = []string{
	"googlebot",
	"bingbot",
	"yandexbot",
	"duckduckbot",
	"baiduspider",
	"slurp",           // Yahoo
	"facebookexternalhit",
	"facebot",
	"twitterbot",
	"linkedinbot",
	"embedly",
	"quora link preview",
	"showyoubot",
	"outbrain",
	"pinterest",
	"applebot",
	"uptimerobot",
	"pingdom",
	"statuscake",
	"datadog",
	"newrelic",
	"semrushbot",
	"ahrefsbot",
	"mj12bot",
	"dotbot",
	"archive.org_bot",
}

// Known malicious scanners, vulnerability probes, and exploit tools (lowercase substrings)
var badBotSignatures = []string{
	"sqlmap",
	"nikto",
	"nessus",
	"acunetix",
	"nmap",
	"zgrab",
	"masscan",
	"gobuster",
	"dirbuster",
	"wpscan",
	"nuclei",
	"censysinspect",
	"shodan",
	"zoomeye",
	"netsparker",
	"openvas",
	"qualys",
	"arachni",
	"scrapy",
	"python-requests",
	"python-urllib",
	"go-http-client",
	"curl/",
	"wget/",
	"libwww-perl",
	"httpclient",
	"winhttp",
	"petalbot",
	"bytespider",
}

// ClassifyClient analyzes a User-Agent string and classifies it into Human, GoodBot, or BadBot.
func ClassifyClient(ua string) ClientType {
	if ua == "" || ua == "-" {
		// Empty user agents or "-" from automated scripts are typically scanner probes
		return ClientBadBot
	}

	lower := strings.ToLower(ua)

	// Check verified / known good search engines and monitors first
	for _, sig := range goodBotSignatures {
		if strings.Contains(lower, sig) {
			return ClientGoodBot
		}
	}

	// Check known scanner, exploit tools, or headless script libraries
	for _, sig := range badBotSignatures {
		if strings.Contains(lower, sig) {
			return ClientBadBot
		}
	}

	// Generic browser heuristics
	if strings.Contains(lower, "mozilla") ||
		strings.Contains(lower, "chrome") ||
		strings.Contains(lower, "safari") ||
		strings.Contains(lower, "firefox") ||
		strings.Contains(lower, "edge") ||
		strings.Contains(lower, "opera") {
		return ClientHuman
	}

	// Any non-browser user agent with "bot", "crawl", "spider", "scan"
	if strings.Contains(lower, "bot") ||
		strings.Contains(lower, "crawl") ||
		strings.Contains(lower, "spider") {
		return ClientGoodBot
	}

	if strings.Contains(lower, "scan") ||
		strings.Contains(lower, "exploit") ||
		strings.Contains(lower, "attack") {
		return ClientBadBot
	}

	return ClientHuman
}
