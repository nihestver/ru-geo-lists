// Package custom reads hand-maintained source files with extra domains, IP
// addresses and CIDRs.
//
// Unlike the published lists, these files are written by people, so the
// format is forgiving: one entry per line, "#" starts a comment (whole line
// or after an entry), blank lines, CRLF line endings and a UTF-8 BOM are
// accepted. An entry is
//
//   - a domain in any form domain.Normalize understands (upper case,
//     "domain:"/"full:" prefix, "*.", leading or trailing dot, IDN);
//   - an IP address (stored as a /32 or /128 prefix);
//   - a CIDR (host bits are cleared: 10.1.2.3/8 becomes 10.0.0.0/8).
//
// Anything else is an error that names the file and the line.
package custom

import (
	"bytes"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/nihestver/ru-geo-lists/internal/domain"
)

// Entries is the parsed content of one custom file, in file order and
// without deduplication.
type Entries struct {
	Domains  []string
	Prefixes []netip.Prefix
}

// ReadFile parses the custom file at path.
func ReadFile(path string) (Entries, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entries{}, err
	}
	e, err := Parse(data)
	if err != nil {
		return Entries{}, fmt.Errorf("%s:%w", path, err)
	}
	return e, nil
}

// Parse parses custom file data. Errors start with the line number
// ("12: ...") so that callers can prefix the file name.
func Parse(data []byte) (Entries, error) {
	var e Entries
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	for i, line := range strings.Split(string(data), "\n") {
		if j := strings.IndexByte(line, '#'); j >= 0 {
			line = line[:j]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, " \t") {
			return Entries{}, fmt.Errorf("%d: %q: expected one domain, IP address or CIDR per line", i+1, line)
		}
		if strings.Contains(line, "/") {
			p, err := netip.ParsePrefix(line)
			if err != nil {
				return Entries{}, fmt.Errorf("%d: invalid CIDR %q: %w", i+1, line, err)
			}
			e.Prefixes = append(e.Prefixes, unmap(p).Masked())
			continue
		}
		if addr, err := netip.ParseAddr(line); err == nil {
			if addr.Zone() != "" {
				return Entries{}, fmt.Errorf("%d: IP address %q has a zone", i+1, line)
			}
			e.Prefixes = append(e.Prefixes, unmap(netip.PrefixFrom(addr, addr.BitLen())))
			continue
		}
		name, err := domain.Normalize(line)
		if err != nil {
			return Entries{}, fmt.Errorf("%d: %q is not a domain, IP address or CIDR: %w", i+1, line, err)
		}
		e.Domains = append(e.Domains, name)
	}
	return e, nil
}

// unmap turns an IPv4-mapped IPv6 prefix (::ffff:10.0.0.0/104) into the IPv4
// prefix it denotes; other prefixes are returned unchanged.
func unmap(p netip.Prefix) netip.Prefix {
	if a := p.Addr(); a.Is4In6() && p.Bits() >= 96 {
		return netip.PrefixFrom(a.Unmap(), p.Bits()-96)
	}
	return p
}
