package config

import (
	"errors"
	"fmt"
	"net"
	"regexp"
)

// iapAudience is the form of an IAP audience for a backend service.
var iapAudience = regexp.MustCompile(`^/projects/[0-9]+/global/backendServices/[0-9]+$`)

// loopbackListen reports whether a listen address binds to loopback only.
func loopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateAuth refuses an API reachable from other hosts without IAP:
// the tool authenticates no one itself, so a listen address beyond
// loopback needs iap_audience, or allow_unauthenticated said explicitly.
func (c Config) validateAuth() []error {
	var errs []error
	if c.IAPAudience != "" && !iapAudience.MatchString(c.IAPAudience) {
		errs = append(errs, fmt.Errorf("iap_audience %q is not /projects/<project number>/global/backendServices/<id>", c.IAPAudience))
	}
	if c.Listen != "" && !loopbackListen(c.Listen) && c.IAPAudience == "" && !c.AllowUnauthenticated {
		errs = append(errs, errors.New("listen is not a loopback address and iap_audience is empty: set iap_audience, "+
			"or allow_unauthenticated: true to serve the API without IAP"))
	}
	return errs
}
