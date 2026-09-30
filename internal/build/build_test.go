package build

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nihestver/ru-geo-lists/internal/config"
	"github.com/nihestver/ru-geo-lists/internal/geodat"
	"github.com/nihestver/ru-geo-lists/internal/listfile"
)

func boolPtr(v bool) *bool { return &v }

func mkCIDR(t *testing.T, s string) geodat.CIDR {
	t.Helper()
	i := strings.IndexByte(s, '/')
	addr, bits := s[:i], s[i+1:]
	var n uint32
	for _, c := range bits {
		n = n*10 + uint32(c-'0')
	}
	ip := parseIP(t, addr)
	return geodat.CIDR{IP: ip, Prefix: n}
}

func parseIP(t *testing.T, s string) []byte {
	t.Helper()
	var b []byte
	if strings.Contains(s, ":") {
		b = make([]byte, 16)
		// Only the simple forms used in this test are supported.
		parts := strings.Split(s, "::")
		head := strings.Split(parts[0], ":")
		for i, h := range head {
			var v uint16
			for _, c := range h {
				v <<= 4
				switch {
				case c >= '0' && c <= '9':
					v |= uint16(c - '0')
				default:
					v |= uint16(c-'a') + 10
				}
			}
			b[2*i], b[2*i+1] = byte(v>>8), byte(v)
		}
		return b
	}
	for _, p := range strings.Split(s, ".") {
		var v int
		for _, c := range p {
			v = v*10 + int(c-'0')
		}
		b = append(b, byte(v))
	}
	return b
}

func fixtures(t *testing.T, dir string) (string, string) {
	t.Helper()
	sites := []geodat.GeoSite{
		{Code: "CATEGORY-RU", Domains: []geodat.Domain{
			{Type: geodat.DomainRootDomain, Value: "Mail.RU"},
			{Type: geodat.DomainRootDomain, Value: "yandex.ru"},
			{Type: geodat.DomainFull, Value: "www.yandex.ru"},
			{Type: geodat.DomainRootDomain, Value: "ru"},
			{Type: geodat.DomainRootDomain, Value: "пример.рф"},
			{Type: geodat.DomainRootDomain, Value: "example.com"},
			{Type: geodat.DomainFull, Value: "a.example.com"},
			{Type: geodat.DomainRootDomain, Value: "example.com"},
			{Type: geodat.DomainPlain, Value: "sber"},
			{Type: geodat.DomainRegex, Value: `^ok\.ru$`},
			{Type: geodat.DomainRootDomain, Value: "foo.local"},
			{Type: geodat.DomainRootDomain, Value: "bad domain"},
			{Type: geodat.DomainRootDomain, Value: "vk.com", Attributes: []geodat.Attribute{{Key: "cn", Bool: boolPtr(true)}}},
			{Type: geodat.DomainFull, Value: "x.vk.com"},
			{Type: geodat.DomainRootDomain, Value: "other.org"},
		}},
		{Code: "TELEGRAM", Domains: []geodat.Domain{
			{Type: geodat.DomainRootDomain, Value: "t.me"},
			{Type: geodat.DomainRootDomain, Value: "telegram.org"},
		}},
		{Code: "ONLY-REGEX", Domains: []geodat.Domain{{Type: geodat.DomainRegex, Value: "^a$"}}},
		{Code: "EMPTY"},
	}
	ips := []geodat.GeoIP{
		{Code: "RU", CIDRs: []geodat.CIDR{
			mkCIDR(t, "10.0.0.0/25"), mkCIDR(t, "10.0.0.128/25"), mkCIDR(t, "10.0.0.0/24"),
			mkCIDR(t, "192.168.5.7/16"),
			mkCIDR(t, "2a02:6b8::/32"), mkCIDR(t, "2a02:6b8:0:1::/64"),
		}},
		{Code: "TELEGRAM", CIDRs: []geodat.CIDR{mkCIDR(t, "149.154.160.0/20"), mkCIDR(t, "2001:b28:f23d::/48")}},
		{Code: "NOT-PRIVATE", CIDRs: []geodat.CIDR{mkCIDR(t, "0.0.0.0/1")}, InverseMatch: true},
		{Code: "EMPTYIP"},
	}
	sitePath := filepath.Join(dir, "geosite.dat")
	ipPath := filepath.Join(dir, "geoip.dat")
	if err := os.WriteFile(sitePath, geodat.MarshalGeoSiteList(sites), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipPath, geodat.MarshalGeoIPList(ips), 0o644); err != nil {
		t.Fatal(err)
	}
	return sitePath, ipPath
}

const baseConfig = `
sources:
  geosite: {url: https://example.invalid/geosite.dat}
  geoip: {url: https://example.invalid/geoip.dat}
output:
  dir: %s
  extensions: [txt, lst]
lists:
  - name: ru
    geosite: [category-ru]
    geoip: [ru]
    extra_domains: [dion.vc, inno.local, "  Inno.Tech "]
    outputs: [combined, domains, ipv4, ipv6]
  - name: telegram
    geosite: [Telegram]
    geoip: [telegram]
    outputs: [combined]
  - name: notpriv
    geoip: [not-private]
    outputs: [ipv4]
`

func setup(t *testing.T, cfgText string) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "lists")
	sitePath, ipPath := fixtures(t, dir)
	cfg, err := config.Parse([]byte(strings.Replace(cfgText, "%s", out, 1)))
	if err != nil {
		t.Fatal(err)
	}
	return Options{Config: cfg, GeoSitePath: sitePath, GeoIPPath: ipPath, Logf: t.Logf}, out
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunProducesExpectedFiles(t *testing.T) {
	opt, out := setup(t, baseConfig)
	rep, err := Run(opt)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Written || len(rep.Violations) != 0 {
		t.Fatalf("unexpected report state: written=%v violations=%v", rep.Written, rep.Violations)
	}
	domains := "dion.vc\nexample.com\ninno.local\ninno.tech\nother.org\nru\nvk.com\nxn--e1afmkfd.xn--p1ai\n"
	v4 := "10.0.0.0/24\n192.168.0.0/16\n"
	v6 := "2a02:6b8::/32\n"
	txt := map[string]string{
		"ru.txt":           domains + v4 + v6,
		"ru-domains.txt":   domains,
		"ru-ipv4.txt":      v4,
		"ru-ipv6.txt":      v6,
		"telegram.txt":     "t.me\ntelegram.org\n149.154.160.0/20\n2001:b28:f23d::/48\n",
		"notpriv-ipv4.txt": "128.0.0.0/1\n",
	}
	// Every file is published twice with the same bytes.
	want := map[string]string{}
	for name, content := range txt {
		want[name] = content
		want[strings.TrimSuffix(name, ".txt")+".lst"] = content
	}
	for name, content := range want {
		if got := read(t, filepath.Join(out, name)); got != content {
			t.Errorf("%s:\n got %q\nwant %q", name, got, content)
		}
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != len(want) {
		t.Errorf("expected %d files, found %d", len(want), len(entries))
	}

	ru := rep.Lists[0]
	if ru.Domains != 8 || ru.IPv4 != 2 || ru.IPv6 != 1 || ru.CoveredRemoved != 5 || ru.PrefixesBeforeAggregation != 6 {
		t.Errorf("ru counts: %+v", ru)
	}
	if strings.Join(ru.SingleLabel, ",") != "ru" {
		t.Errorf("single-label: %v", ru.SingleLabel)
	}
	if strings.Join(ru.ExtraDomains, ",") != "dion.vc,inno.local,inno.tech" {
		t.Errorf("extra domains: %v", ru.ExtraDomains)
	}
	cs := ru.Categories[0]
	if cs.Code != "CATEGORY-RU" || cs.Total != 15 || cs.RootDomain != 10 || cs.Full != 3 || cs.Keyword != 1 || cs.Regex != 1 ||
		cs.Attributes != 1 || cs.Invalid != 1 || cs.UnknownTLD != 1 || cs.Skipped() != 4 {
		t.Errorf("category stats: %+v", cs)
	}
	if cs.Examples["keyword"][0] != "sber" || cs.Examples["regexp"][0] != `^ok\.ru$` || cs.Examples["unknown-tld"][0] != "foo.local" || !strings.HasPrefix(cs.Examples["invalid"][0], "bad domain") {
		t.Errorf("examples: %+v", cs.Examples)
	}
	// inverse_match of an IPv4-only set also matches every IPv6 address, so the
	// complement carries ::/0 (it is only published if an ipv6 output is configured).
	if g := rep.Lists[2].GeoIPs[0]; !g.InverseMatch || g.IPv4 != 1 || g.IPv6 != 1 {
		t.Errorf("inverse match stats: %+v", g)
	}
	for _, f := range ru.Files {
		if !f.IsNew() || f.Added != f.Lines || f.Removed != 0 {
			t.Errorf("first run file stats: %+v", f)
		}
	}
	md := rep.Markdown()
	for _, s := range []string{"| `ru.txt`, `ru.lst` | 11 | — | new |", "geosite:category-ru", "inverse_match", "single-label (TLD) entries: ru", "5 subdomains removed", "3 prefixes merged", "All files are within"} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q:\n%s", s, md)
		}
	}
	msg := rep.CommitMessage()
	if !strings.HasPrefix(msg, "Update lists: ru new, telegram new, notpriv new\n\n") || !strings.Contains(msg, "ru.txt, ru.lst: 11 lines (new); 8 domains, 2 IPv4, 1 IPv6\n") {
		t.Errorf("commit message:\n%s", msg)
	}
}

func TestRunIsDeterministicAndReportsNoChange(t *testing.T) {
	opt, out := setup(t, baseConfig)
	if _, err := Run(opt); err != nil {
		t.Fatal(err)
	}
	first := map[string]string{}
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		first[e.Name()] = read(t, filepath.Join(out, e.Name()))
	}
	rep, err := Run(opt)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range first {
		if got := read(t, filepath.Join(out, name)); got != content {
			t.Errorf("%s changed between identical runs", name)
		}
	}
	for _, lr := range rep.Lists {
		for _, f := range lr.Files {
			if f.IsNew() || f.Added != 0 || f.Removed != 0 || f.Previous != f.Lines {
				t.Errorf("second run should report no change: %+v", f)
			}
		}
	}
	if msg := rep.CommitMessage(); !strings.HasPrefix(msg, "Update lists\n\n") || !strings.Contains(msg, "ru.txt, ru.lst: 11 lines (=)") {
		t.Errorf("commit message:\n%s", msg)
	}
}

func TestAddingAnExtensionReportsNewCopies(t *testing.T) {
	// First publish .txt only, then add .lst: the .txt files are unchanged,
	// the .lst files are new, and the two must not be merged into one row.
	opt, out := setup(t, strings.Replace(baseConfig, "extensions: [txt, lst]", "extensions: [txt]", 1))
	if _, err := Run(opt); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse([]byte(strings.Replace(baseConfig, "%s", out, 1)))
	if err != nil {
		t.Fatal(err)
	}
	opt.Config = cfg
	rep, err := Run(opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Violations) != 0 || !rep.Written {
		t.Fatalf("unexpected report state: %+v", rep)
	}
	for _, lr := range rep.Lists {
		for _, f := range lr.Files {
			isLst := strings.HasSuffix(f.Name, ".lst")
			if f.IsNew() != isLst || (isLst && f.Added != f.Lines) || (!isLst && (f.Added != 0 || f.Removed != 0)) {
				t.Errorf("file stats: %+v", f)
			}
		}
	}
	md := rep.Markdown()
	for _, s := range []string{"| `ru.txt` | 11 | 11 | = |", "| `ru.lst` | 11 | — | new |", "| `ru-ipv4.txt` | 2 | 2 | = |", "| `ru-ipv4.lst` | 2 | — | new |"} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q:\n%s", s, md)
		}
	}
	msg := rep.CommitMessage()
	for _, s := range []string{"ru.txt: 11 lines (=); 8 domains, 2 IPv4, 1 IPv6\n", "ru.lst: 11 lines (new)\n", "telegram.txt: 4 lines (=); 2 domains, 1 IPv4, 1 IPv6\ntelegram.lst: 4 lines (new)\n"} {
		if !strings.Contains(msg, s) {
			t.Errorf("commit message lacks %q:\n%s", s, msg)
		}
	}
	for _, base := range []string{"ru", "ru-domains", "ru-ipv4", "ru-ipv6", "telegram", "notpriv-ipv4"} {
		if err := listfile.SameContent([]string{filepath.Join(out, base+".txt"), filepath.Join(out, base+".lst")}); err != nil {
			t.Error(err)
		}
	}
}

func TestRunDryRunWritesNothing(t *testing.T) {
	opt, out := setup(t, baseConfig)
	opt.DryRun = true
	rep, err := Run(opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Written {
		t.Fatal("dry run must not write")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("output directory must not be created in a dry run")
	}
}

func TestThreshold(t *testing.T) {
	opt, out := setup(t, baseConfig)
	// Previous ru-domains.txt with 100 lines: the new one has 8, a change far above 30%.
	var big []string
	for i := 0; i < 100; i++ {
		big = append(big, "d"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+strings.Repeat("y", i/26)+".com")
	}
	if err := listfile.Write(filepath.Join(out, "ru-domains.txt"), big); err != nil {
		t.Fatal(err)
	}
	// Previous ru-ipv6.txt with 5 lines: the new one has 1, but 4 lines are within small_change_lines.
	if err := listfile.Write(filepath.Join(out, "ru-ipv6.txt"), []string{"2001:db8::/32", "2001:db8:1::/48", "2001:db8:2::/48", "2001:db8:3::/48", "2001:db8:4::/48"}); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(opt)
	if !errors.Is(err, ErrThreshold) {
		t.Fatalf("expected ErrThreshold, got %v", err)
	}
	if rep == nil || len(rep.Violations) != 1 || !strings.Contains(rep.Violations[0], "ru-domains.txt: 100 -> 8 lines") {
		t.Fatalf("violations: %+v", rep)
	}
	if got := read(t, filepath.Join(out, "ru-domains.txt")); len(strings.Split(strings.TrimSpace(got), "\n")) != 100 {
		t.Fatal("file must not be overwritten on a threshold violation")
	}
	if _, err := os.Stat(filepath.Join(out, "ru.txt")); !os.IsNotExist(err) {
		t.Fatal("no file may be written when any file violates the threshold")
	}
	if !strings.Contains(rep.Markdown(), ":warning: ru-domains.txt") {
		t.Error("markdown should show the violation")
	}

	opt.AllowLargeChange = true
	rep, err = Run(opt)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Written || len(rep.Violations) != 1 {
		t.Fatalf("expected written with a recorded violation, got %+v", rep)
	}
	if got := read(t, filepath.Join(out, "ru-domains.txt")); got != "dion.vc\nexample.com\ninno.local\ninno.tech\nother.org\nru\nvk.com\nxn--e1afmkfd.xn--p1ai\n" {
		t.Fatalf("file not overwritten: %q", got)
	}
}

func TestExceeds(t *testing.T) {
	s := config.Safety{MaxChangeRatio: 0.30, SmallChangeLines: 10}
	cases := []struct {
		prev, cur int
		want      bool
	}{
		{100, 100, false}, {100, 130, false}, {100, 131, true}, {100, 70, false}, {100, 69, true},
		{20, 30, false}, {20, 31, true}, {5, 15, false}, {0, 10, false}, {0, 11, true}, {1000, 1300, false}, {1000, 1301, true},
	}
	for _, c := range cases {
		if got := exceeds(c.prev, c.cur, s); got != c.want {
			t.Errorf("exceeds(%d, %d) = %v want %v", c.prev, c.cur, got, c.want)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name, replace, with, wantErr string
	}{
		{"missing geosite", "geosite: [category-ru]", "geosite: [category-r]", "category-ru"},
		{"empty geosite", "geosite: [category-ru]", "geosite: [empty]", "is empty"},
		{"only regex", "geosite: [category-ru]", "geosite: [only-regex]", "none can be expressed"},
		{"missing geoip", "geoip: [ru]", "geoip: [ru-blocked]", "category not found"},
		{"empty geoip", "geoip: [ru]", "geoip: [emptyip]", "is empty"},
		{"bad extra domain", "inno.local", "\"bad domain\"", "extra domain"},
	}
	for _, c := range cases {
		opt, _ := setup(t, strings.Replace(baseConfig, c.replace, c.with, 1))
		_, err := Run(opt)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: got %v, want error containing %q", c.name, err, c.wantErr)
		}
	}
}

func TestMissingInputFile(t *testing.T) {
	opt, _ := setup(t, baseConfig)
	opt.GeoSitePath = filepath.Join(t.TempDir(), "nope.dat")
	if _, err := Run(opt); err == nil {
		t.Fatal("expected error for missing geosite.dat")
	}
}
