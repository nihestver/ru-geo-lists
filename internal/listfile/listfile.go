// Package listfile writes and validates the published plain-text lists.
//
// File format: exactly one domain or one CIDR per line, no comments, no
// empty lines, no BOM, LF line endings with a final newline, lower case, no
// duplicates. Combined files contain domains first, then IPv4 prefixes, then
// IPv6 prefixes; every section is sorted.
package listfile

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/nihestver/ru-geo-lists/internal/cidr"
	"github.com/nihestver/ru-geo-lists/internal/domain"
)

// Kind selects which part of a list a file contains.
type Kind int

const (
	// KindCombined holds domains, IPv4 and IPv6 prefixes together.
	KindCombined Kind = iota
	// KindDomains holds domains only.
	KindDomains
	// KindIPv4 holds IPv4 prefixes only.
	KindIPv4
	// KindIPv6 holds IPv6 prefixes only.
	KindIPv6
)

var kindNames = map[Kind]string{
	KindCombined: "combined",
	KindDomains:  "domains",
	KindIPv4:     "ipv4",
	KindIPv6:     "ipv6",
}

// ParseKind converts a config value ("combined", "domains", "ipv4", "ipv6").
func ParseKind(s string) (Kind, error) {
	for k, name := range kindNames {
		if name == s {
			return k, nil
		}
	}
	return 0, fmt.Errorf("unknown output kind %q (want combined, domains, ipv4 or ipv6)", s)
}

func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Suffix is appended to the list name to build the file name.
func (k Kind) Suffix() string {
	if k == KindCombined {
		return ""
	}
	return "-" + k.String()
}

// FileName returns the file name for a list and kind, e.g. "ru-ipv4.txt".
func FileName(list string, k Kind, ext string) string {
	return list + k.Suffix() + "." + ext
}

// List is the fully processed content of one list. Every slice must already
// be sorted (domains in byte order, prefixes with cidr.Compare).
type List struct {
	Domains []string
	IPv4    []netip.Prefix
	IPv6    []netip.Prefix
}

// Lines returns the file content for the given kind, one entry per line.
func (l List) Lines(k Kind) []string {
	var out []string
	if k == KindCombined || k == KindDomains {
		out = append(out, l.Domains...)
	}
	if k == KindCombined || k == KindIPv4 {
		for _, p := range l.IPv4 {
			out = append(out, p.String())
		}
	}
	if k == KindCombined || k == KindIPv6 {
		for _, p := range l.IPv6 {
			out = append(out, p.String())
		}
	}
	return out
}

// Encode joins lines with LF and adds the final newline.
func Encode(lines []string) []byte {
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// Write stores lines at path atomically (temporary file plus rename). It
// refuses to write an empty list.
func Write(path string, lines []string) error {
	if len(lines) == 0 {
		return fmt.Errorf("%s: refusing to write an empty list", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, Encode(lines), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadLines returns the lines of an existing list file without validating
// them. A missing file yields an error satisfying errors.Is(err, os.ErrNotExist).
func ReadLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// SameContent checks that every file holds exactly the same bytes as the
// first one. The published copies of a list (for example ru.txt and ru.lst)
// must never diverge.
func SameContent(paths []string) error {
	if len(paths) < 2 {
		return nil
	}
	first, err := os.ReadFile(paths[0])
	if err != nil {
		return err
	}
	for _, p := range paths[1:] {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, first) {
			return fmt.Errorf("%s differs from %s", p, paths[0])
		}
	}
	return nil
}

// ValidateFile reads path and checks it with Validate.
func ValidateFile(path string, k Kind) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := Validate(data, k); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Validate checks that data is a well-formed list of the given kind.
func Validate(data []byte, k Kind) error {
	if len(data) == 0 {
		return errors.New("file is empty")
	}
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		return errors.New("file starts with a UTF-8 BOM")
	}
	if i := bytes.IndexByte(data, '\r'); i >= 0 {
		return fmt.Errorf("line %d: contains a carriage return (CRLF line endings?)", 1+bytes.Count(data[:i], []byte{'\n'}))
	}
	if data[len(data)-1] != '\n' {
		return errors.New("file does not end with a newline")
	}
	lines := strings.Split(string(data[:len(data)-1]), "\n")

	const (
		secDomains = iota
		secIPv4
		secIPv6
	)
	section := secDomains
	havePrev := false
	var prevDomain string
	var prevPrefix netip.Prefix
	for i, line := range lines {
		n := i + 1
		if line == "" {
			return fmt.Errorf("line %d: empty line", n)
		}
		if strings.ContainsAny(line, " \t\v\f") {
			return fmt.Errorf("line %d: contains whitespace", n)
		}
		var sec int
		if strings.Contains(line, "/") {
			pfx, err := netip.ParsePrefix(line)
			if err != nil {
				return fmt.Errorf("line %d: invalid CIDR %q: %w", n, line, err)
			}
			if pfx.Masked() != pfx {
				return fmt.Errorf("line %d: CIDR %q has host bits set (want %s)", n, line, pfx.Masked())
			}
			if pfx.String() != line {
				return fmt.Errorf("line %d: CIDR %q is not in canonical form (want %s)", n, line, pfx)
			}
			sec = secIPv4
			if !pfx.Addr().Is4() {
				sec = secIPv6
			}
			if sec < section {
				return fmt.Errorf("line %d: %s appears after the %s section", n, line, sectionName(section))
			}
			if sec > section {
				section, havePrev = sec, false
			}
			if havePrev {
				switch c := cidr.Compare(prevPrefix, pfx); {
				case c == 0:
					return fmt.Errorf("line %d: duplicate of %s", n, prevPrefix)
				case c > 0:
					return fmt.Errorf("line %d: %s is out of order (after %s)", n, pfx, prevPrefix)
				}
			}
			prevPrefix, havePrev = pfx, true
		} else {
			sec = secDomains
			if section != secDomains {
				return fmt.Errorf("line %d: domain %q appears after the %s section", n, line, sectionName(section))
			}
			if err := domain.Validate(line); err != nil {
				return fmt.Errorf("line %d: %w", n, err)
			}
			if havePrev {
				switch {
				case line == prevDomain:
					return fmt.Errorf("line %d: duplicate of %q", n, line)
				case line < prevDomain:
					return fmt.Errorf("line %d: %q is out of order (after %q)", n, line, prevDomain)
				}
			}
			prevDomain, havePrev = line, true
		}
		switch k {
		case KindDomains:
			if sec != secDomains {
				return fmt.Errorf("line %d: %q is not a domain in a domains-only file", n, line)
			}
		case KindIPv4:
			if sec != secIPv4 {
				return fmt.Errorf("line %d: %q is not an IPv4 CIDR in an IPv4-only file", n, line)
			}
		case KindIPv6:
			if sec != secIPv6 {
				return fmt.Errorf("line %d: %q is not an IPv6 CIDR in an IPv6-only file", n, line)
			}
		}
	}
	return nil
}

func sectionName(sec int) string {
	switch sec {
	case 1:
		return "IPv4"
	case 2:
		return "IPv6"
	default:
		return "domain"
	}
}
