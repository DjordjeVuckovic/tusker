package reader

import (
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// NormalizeURL trims and validates raw, returning a cleaned absolute http(s)
// URL. The bool is false when raw is empty or not a valid http(s) URL.
func NormalizeURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return u.String(), true
}

// PublisherDomain returns the registrable domain of a normalised url's host
// by the Public Suffix List, so uk.reuters.com becomes reuters.com while
// x.blogspot.com, itself under a public suffix, stays whole. A host with no
// registrable domain, such as an IP address, is returned as is.
func PublisherDomain(normalizedURL string) string {
	u, err := url.Parse(normalizedURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return domain
}
