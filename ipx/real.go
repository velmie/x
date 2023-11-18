package ipx

import (
	"bytes"
	"net"
	"strings"
)

// RequestReader gets necessary data from request
type RequestReader interface {
	GetHeader(headerKey string) string
	GetRemoteAddr() string
}

// CIDRParser parses CIDR blocks
type CIDRParser interface {
	Parse(cidr string) (Range, error)
}

// RealIPGetter gets real IP address of the request
type RealIPGetter struct {
	cidrParser     CIDRParser
	trustedProxies []Range
	privateSubnets []Range
	ipHeaders      []string
}

func NewRealIPGetter(cidrParser CIDRParser) *RealIPGetter {
	return &RealIPGetter{cidrParser: cidrParser}
}

// GetRealIP returns real IP address of the request
func (g *RealIPGetter) GetRealIP(r RequestReader) string {
	addr := r.GetRemoteAddr()
	ip, _, _ := net.SplitHostPort(r.GetRemoteAddr())

	// for example port could be missing in addr, in this case ip will be empty
	if ip == "" {
		ip = addr
	}

	netIP := net.ParseIP(ip)

	if !g.isTrustedProxy(netIP) {
		return ip
	}

	for _, name := range g.ipHeaders {
		values := strings.Split(r.GetHeader(name), ",")
		for _, v := range values {
			v = strings.TrimSpace(v)
			valIP := net.ParseIP(v)
			if valIP == nil {
				continue
			}
			if !valIP.IsGlobalUnicast() || g.isPrivateAddress(valIP) {
				continue
			}
			return v
		}
	}

	return ip
}

func (g *RealIPGetter) AddTrustedProxies(cidr ...string) error {
	for _, c := range cidr {
		r, err := g.cidrParser.Parse(c)
		if err != nil {
			return err
		}
		g.trustedProxies = append(g.trustedProxies, r)
	}
	return nil
}

func (g *RealIPGetter) AddPrivateSubnets(cidr ...string) error {
	for _, c := range cidr {
		r, err := g.cidrParser.Parse(c)
		if err != nil {
			return err
		}
		g.privateSubnets = append(g.privateSubnets, r)
	}
	return nil
}

func (g *RealIPGetter) AddIPHeaders(headers ...string) {
	g.ipHeaders = append(g.ipHeaders, headers...)
}

func (g *RealIPGetter) isPrivateAddress(ip net.IP) bool {
	for _, pr := range g.privateSubnets {
		if pr.InRange(ip) {
			return true
		}
	}
	return false
}

func (g *RealIPGetter) isTrustedProxy(ip net.IP) bool {
	for _, tpr := range g.trustedProxies {
		if tpr.InRange(ip) {
			return true
		}
	}
	return false
}

// Range is a structure that holds the start and end of a range of IP Addresses.
type Range struct {
	Start net.IP
	End   net.IP
}

// InRange reports whether a given IP Address is within a range given.
func (r Range) InRange(ipAddress net.IP) bool {
	return bytes.Compare(ipAddress, r.Start) >= 0 && bytes.Compare(ipAddress, r.End) < 0
}
