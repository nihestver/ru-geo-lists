// Package domain normalizes and validates domain names for plain-text lists.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

const (
	maxNameLen  = 253
	maxLabelLen = 63
)

// Normalize converts a raw value from a domain list into canonical form and
// validates it: lower case, no "domain:"/"full:" prefix, no leading "*." or
// ".", no trailing ".", internationalized labels converted to punycode.
func Normalize(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	for {
		before := s
		s = strings.TrimPrefix(s, "domain:")
		s = strings.TrimPrefix(s, "full:")
		s = strings.TrimPrefix(s, "*.")
		s = strings.TrimLeft(s, ".")
		if s == before {
			break
		}
	}
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "", errors.New("empty domain")
	}
	if !isASCII(s) {
		ascii, err := idna.Lookup.ToASCII(s)
		if err != nil {
			return "", fmt.Errorf("idna: %w", err)
		}
		s = strings.ToLower(ascii)
	}
	if err := Validate(s); err != nil {
		return "", err
	}
	return s, nil
}

// Validate checks that s is a DNS name in canonical form: lower-case ASCII,
// labels of 1..63 characters from [a-z0-9_-] that do not start or end with a
// hyphen, total length at most 253, punycode labels decodable. Single-label
// names (top-level domains) are accepted.
func Validate(s string) error {
	switch {
	case s == "":
		return errors.New("empty domain")
	case len(s) > maxNameLen:
		return fmt.Errorf("domain %q longer than %d characters", s, maxNameLen)
	case !isASCII(s):
		return fmt.Errorf("domain %q contains non-ASCII characters (expected punycode)", s)
	case strings.ContainsAny(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"):
		return fmt.Errorf("domain %q is not lower case", s)
	}
	labels := strings.Split(s, ".")
	for _, label := range labels {
		if err := validateLabel(label); err != nil {
			return fmt.Errorf("domain %q: %w", s, err)
		}
	}
	// A top-level domain is never all-numeric (RFC 3696 §2); this also rejects
	// bare IPv4 addresses that would otherwise look like valid names.
	if allDigits(labels[len(labels)-1]) {
		return fmt.Errorf("domain %q: top-level label is numeric", s)
	}
	return nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func validateLabel(label string) error {
	if label == "" {
		return errors.New("empty label")
	}
	if len(label) > maxLabelLen {
		return fmt.Errorf("label %q longer than %d characters", label, maxLabelLen)
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return fmt.Errorf("label %q contains invalid character %q", label, c)
		}
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("label %q starts or ends with a hyphen", label)
	}
	if strings.HasPrefix(label, "xn--") {
		if _, err := idna.ToUnicode(label); err != nil {
			return fmt.Errorf("label %q is not valid punycode: %w", label, err)
		}
	}
	return nil
}

// IsSingleLabel reports whether s has no dots, i.e. it is a top-level domain.
func IsSingleLabel(s string) bool {
	return !strings.Contains(s, ".")
}

// KnownTLD reports whether the top-level domain of s is listed in the ICANN
// section of the Public Suffix List. Private pseudo-TLDs such as "local" or
// "internal" are not known.
func KnownTLD(s string) bool {
	if _, icann := publicsuffix.PublicSuffix(s); icann {
		return true
	}
	// s may sit under a privately managed suffix (e.g. github.io), so look at
	// the bare TLD as well.
	tld := s[strings.LastIndexByte(s, '.')+1:]
	if _, icann := publicsuffix.PublicSuffix(tld); icann {
		return true
	}
	// Some TLDs exist only as wildcard rules ("*.jm"): the bare TLD has no
	// rule of its own, but any name below it matches the wildcard.
	_, icann := publicsuffix.PublicSuffix("x." + tld)
	return icann
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Set is a set of canonical domain names.
type Set struct {
	m map[string]struct{}
}

// NewSet returns an empty set.
func NewSet() *Set {
	return &Set{m: make(map[string]struct{})}
}

// Add inserts d, which must already be canonical.
func (s *Set) Add(d string) {
	s.m[d] = struct{}{}
}

// Has reports whether d is a member.
func (s *Set) Has(d string) bool {
	_, ok := s.m[d]
	return ok
}

// Len returns the number of members.
func (s *Set) Len() int {
	return len(s.m)
}

// Minimal returns the members that are not covered by a parent domain in the
// same set, sorted in byte order. Because every line of a list matches the
// domain and all of its subdomains, "a.example.com" is redundant when
// "example.com" is present, and "example.ru" is redundant when "ru" is.
func (s *Set) Minimal() []string {
	out := make([]string, 0, len(s.m))
	for d := range s.m {
		if !s.coveredByParent(d) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// Covered returns the members that Minimal drops, sorted, for reporting.
func (s *Set) Covered() []string {
	var out []string
	for d := range s.m {
		if s.coveredByParent(d) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Set) coveredByParent(d string) bool {
	for {
		i := strings.IndexByte(d, '.')
		if i < 0 {
			return false
		}
		d = d[i+1:]
		if _, ok := s.m[d]; ok {
			return true
		}
	}
}
