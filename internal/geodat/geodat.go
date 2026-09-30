// Package geodat decodes and encodes v2ray/Xray geosite.dat and geoip.dat files.
//
// The files are protobuf messages GeoSiteList and GeoIPList defined in
// v2ray-core (app/router/routercommon/common.proto). Only the wire format is
// needed here, so the package walks the messages with protowire instead of
// depending on generated code.
package geodat

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// DomainType mirrors routercommon.Domain_Type.
type DomainType int32

const (
	// DomainPlain matches when the value is a substring of the queried name ("keyword:").
	DomainPlain DomainType = 0
	// DomainRegex matches the queried name against a regular expression ("regexp:").
	DomainRegex DomainType = 1
	// DomainRootDomain matches the value and all of its subdomains ("domain:").
	DomainRootDomain DomainType = 2
	// DomainFull matches the value exactly ("full:").
	DomainFull DomainType = 3
)

func (t DomainType) String() string {
	switch t {
	case DomainPlain:
		return "keyword"
	case DomainRegex:
		return "regexp"
	case DomainRootDomain:
		return "domain"
	case DomainFull:
		return "full"
	default:
		return fmt.Sprintf("unknown(%d)", int32(t))
	}
}

// Attribute mirrors routercommon.Domain_Attribute.
type Attribute struct {
	Key  string
	Bool *bool
	Int  *int64
}

// Domain mirrors routercommon.Domain.
type Domain struct {
	Type       DomainType
	Value      string
	Attributes []Attribute
}

// GeoSite mirrors routercommon.GeoSite.
type GeoSite struct {
	// Code is country_code (field 1) or, when that is empty, code (field 4).
	Code    string
	Domains []Domain
}

// CIDR mirrors routercommon.CIDR. IP is 4 bytes for IPv4 and 16 bytes for IPv6.
type CIDR struct {
	IP     []byte
	Prefix uint32
}

// GeoIP mirrors routercommon.GeoIP.
type GeoIP struct {
	// Code is country_code (field 1) or, when that is empty, code (field 5).
	Code  string
	CIDRs []CIDR
	// InverseMatch (field inverse_match) inverts the rule: it matches every
	// address that is NOT in CIDRs.
	InverseMatch bool
}

// ReadGeoSiteList reads and parses a geosite.dat file.
func ReadGeoSiteList(path string) ([]GeoSite, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, err := ParseGeoSiteList(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return entries, nil
}

// ReadGeoIPList reads and parses a geoip.dat file.
func ReadGeoIPList(path string) ([]GeoIP, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, err := ParseGeoIPList(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return entries, nil
}

// ParseGeoSiteList parses the body of a GeoSiteList message.
func ParseGeoSiteList(b []byte) ([]GeoSite, error) {
	var out []GeoSite
	err := walk(b, func(f field) error {
		if f.num != 1 || f.typ != protowire.BytesType {
			return nil
		}
		gs, err := parseGeoSite(f.bytes)
		if err != nil {
			return fmt.Errorf("entry %d: %w", len(out), err)
		}
		out = append(out, gs)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("geosite: %w", err)
	}
	return out, nil
}

// ParseGeoIPList parses the body of a GeoIPList message.
func ParseGeoIPList(b []byte) ([]GeoIP, error) {
	var out []GeoIP
	err := walk(b, func(f field) error {
		if f.num != 1 || f.typ != protowire.BytesType {
			return nil
		}
		g, err := parseGeoIP(f.bytes)
		if err != nil {
			return fmt.Errorf("entry %d: %w", len(out), err)
		}
		out = append(out, g)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("geoip: %w", err)
	}
	return out, nil
}

func parseGeoSite(b []byte) (GeoSite, error) {
	var gs GeoSite
	var altCode string
	err := walk(b, func(f field) error {
		switch {
		case f.num == 1 && f.typ == protowire.BytesType:
			gs.Code = string(f.bytes)
		case f.num == 2 && f.typ == protowire.BytesType:
			d, err := parseDomain(f.bytes)
			if err != nil {
				return fmt.Errorf("domain %d: %w", len(gs.Domains), err)
			}
			gs.Domains = append(gs.Domains, d)
		case f.num == 4 && f.typ == protowire.BytesType:
			altCode = string(f.bytes)
		}
		return nil
	})
	if err != nil {
		return gs, err
	}
	if gs.Code == "" {
		gs.Code = altCode
	}
	return gs, nil
}

func parseDomain(b []byte) (Domain, error) {
	var d Domain
	err := walk(b, func(f field) error {
		switch {
		case f.num == 1 && f.typ == protowire.VarintType:
			d.Type = DomainType(f.varint)
		case f.num == 2 && f.typ == protowire.BytesType:
			d.Value = string(f.bytes)
		case f.num == 3 && f.typ == protowire.BytesType:
			a, err := parseAttribute(f.bytes)
			if err != nil {
				return err
			}
			d.Attributes = append(d.Attributes, a)
		}
		return nil
	})
	return d, err
}

func parseAttribute(b []byte) (Attribute, error) {
	var a Attribute
	err := walk(b, func(f field) error {
		switch {
		case f.num == 1 && f.typ == protowire.BytesType:
			a.Key = string(f.bytes)
		case f.num == 2 && f.typ == protowire.VarintType:
			v := f.varint != 0
			a.Bool = &v
		case f.num == 3 && f.typ == protowire.VarintType:
			v := int64(f.varint)
			a.Int = &v
		}
		return nil
	})
	return a, err
}

func parseGeoIP(b []byte) (GeoIP, error) {
	var g GeoIP
	var altCode string
	err := walk(b, func(f field) error {
		switch {
		case f.num == 1 && f.typ == protowire.BytesType:
			g.Code = string(f.bytes)
		case f.num == 2 && f.typ == protowire.BytesType:
			c, err := parseCIDR(f.bytes)
			if err != nil {
				return fmt.Errorf("cidr %d: %w", len(g.CIDRs), err)
			}
			g.CIDRs = append(g.CIDRs, c)
		case f.num == 3 && f.typ == protowire.VarintType:
			g.InverseMatch = f.varint != 0
		case f.num == 5 && f.typ == protowire.BytesType:
			altCode = string(f.bytes)
		}
		return nil
	})
	if err != nil {
		return g, err
	}
	if g.Code == "" {
		g.Code = altCode
	}
	return g, nil
}

func parseCIDR(b []byte) (CIDR, error) {
	var c CIDR
	err := walk(b, func(f field) error {
		switch {
		case f.num == 1 && f.typ == protowire.BytesType:
			c.IP = append([]byte(nil), f.bytes...)
		case f.num == 2 && f.typ == protowire.VarintType:
			c.Prefix = uint32(f.varint)
		}
		return nil
	})
	if err != nil {
		return c, err
	}
	if n := len(c.IP); n != 4 && n != 16 {
		return c, fmt.Errorf("ip has %d bytes, want 4 or 16", n)
	}
	return c, nil
}

// field is one decoded protobuf field. Only varint and length-delimited
// fields are surfaced; fixed-width and group fields are skipped because the
// routercommon schema does not use them.
type field struct {
	num    protowire.Number
	typ    protowire.Type
	varint uint64
	bytes  []byte
}

func walk(b []byte, fn func(f field) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]
		f := field{num: num, typ: typ}
		switch typ {
		case protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			f.varint = v
			b = b[n:]
		case protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			f.bytes = v
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
			continue
		}
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

// ErrNotFound is returned by FindGeoSite and FindGeoIP when no entry matches.
var ErrNotFound = errors.New("category not found")

// FindGeoSite returns the entry whose code equals code, ignoring case.
func FindGeoSite(entries []GeoSite, code string) (*GeoSite, error) {
	for i := range entries {
		if strings.EqualFold(entries[i].Code, code) {
			return &entries[i], nil
		}
	}
	return nil, notFound(code, geoSiteCodes(entries))
}

// FindGeoIP returns the entry whose code equals code, ignoring case.
func FindGeoIP(entries []GeoIP, code string) (*GeoIP, error) {
	for i := range entries {
		if strings.EqualFold(entries[i].Code, code) {
			return &entries[i], nil
		}
	}
	return nil, notFound(code, geoIPCodes(entries))
}

func geoSiteCodes(entries []GeoSite) []string {
	codes := make([]string, 0, len(entries))
	for _, e := range entries {
		codes = append(codes, e.Code)
	}
	return codes
}

func geoIPCodes(entries []GeoIP) []string {
	codes := make([]string, 0, len(entries))
	for _, e := range entries {
		codes = append(codes, e.Code)
	}
	return codes
}

func notFound(code string, codes []string) error {
	similar := SimilarCodes(codes, code, 10)
	if len(similar) == 0 {
		return fmt.Errorf("%w: %q (no similar codes among %d available)", ErrNotFound, code, len(codes))
	}
	return fmt.Errorf("%w: %q; similar codes: %s", ErrNotFound, code, strings.Join(similar, ", "))
}

// SimilarCodes returns up to max codes that look like want: codes that contain
// want (or vice versa) come first, then codes within a small edit distance.
func SimilarCodes(codes []string, want string, max int) []string {
	want = strings.ToLower(want)
	type scored struct {
		code  string
		score int
	}
	var found []scored
	for _, c := range codes {
		lc := strings.ToLower(c)
		switch {
		case lc == want:
			found = append(found, scored{lc, 0})
		case strings.Contains(lc, want) || strings.Contains(want, lc):
			found = append(found, scored{lc, 1})
		default:
			if d := levenshtein(lc, want); d <= 3 {
				found = append(found, scored{lc, 1 + d})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score < found[j].score
		}
		return found[i].code < found[j].code
	})
	out := make([]string, 0, max)
	for _, s := range found {
		if len(out) == max {
			break
		}
		out = append(out, s.code)
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
