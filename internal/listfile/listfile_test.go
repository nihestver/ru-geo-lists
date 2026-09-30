package listfile

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFileName(t *testing.T) {
	cases := map[Kind]string{KindCombined: "ru.txt", KindDomains: "ru-domains.txt", KindIPv4: "ru-ipv4.txt", KindIPv6: "ru-ipv6.txt"}
	for k, want := range cases {
		if got := FileName("ru", k, "txt"); got != want {
			t.Errorf("FileName(ru, %s) = %q want %q", k, got, want)
		}
	}
	if got := FileName("ru", KindCombined, "lst"); got != "ru.lst" {
		t.Errorf("extension not applied: %q", got)
	}
	if _, err := ParseKind("all"); err == nil {
		t.Error("ParseKind(all) should fail")
	}
	k, err := ParseKind("ipv6")
	if err != nil || k != KindIPv6 {
		t.Errorf("ParseKind(ipv6) = %v, %v", k, err)
	}
}

func sample() List {
	return List{
		Domains: []string{"a.example", "example.com", "xn--p1ai"},
		IPv4:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.0.0/16")},
		IPv6:    []netip.Prefix{netip.MustParsePrefix("2001:db8::/32")},
	}
}

func TestLinesAndValidate(t *testing.T) {
	l := sample()
	cases := map[Kind][]string{
		KindCombined: {"a.example", "example.com", "xn--p1ai", "10.0.0.0/8", "192.168.0.0/16", "2001:db8::/32"},
		KindDomains:  {"a.example", "example.com", "xn--p1ai"},
		KindIPv4:     {"10.0.0.0/8", "192.168.0.0/16"},
		KindIPv6:     {"2001:db8::/32"},
	}
	for k, want := range cases {
		got := l.Lines(k)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Lines(%s) = %v want %v", k, got, want)
		}
		if err := Validate(Encode(got), k); err != nil {
			t.Errorf("Validate(%s): %v", k, err)
		}
	}
	// Kind restrictions.
	if err := Validate(Encode(cases[KindCombined]), KindDomains); err == nil {
		t.Error("combined content must not validate as domains-only")
	}
	if err := Validate(Encode(cases[KindDomains]), KindIPv4); err == nil {
		t.Error("domains must not validate as ipv4-only")
	}
	if err := Validate(Encode(cases[KindIPv4]), KindIPv6); err == nil {
		t.Error("ipv4 must not validate as ipv6-only")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]string{
		"empty file":          "",
		"bom":                 "\xef\xbb\xbfexample.com\n",
		"crlf":                "example.com\r\n",
		"no final newline":    "example.com",
		"empty line":          "a.com\n\nb.com\n",
		"trailing empty line": "a.com\n\n",
		"comment":             "# comment\na.com\n",
		"whitespace":          "a.com \n",
		"upper case":          "A.com\n",
		"leading dot":         ".a.com\n",
		"wildcard":            "*.a.com\n",
		"prefix":              "domain:a.com\n",
		"unicode":             "пример.рф\n",
		"duplicate domain":    "a.com\na.com\n",
		"unsorted domains":    "b.com\na.com\n",
		"bare ip":             "1.2.3.4\n",
		"host bits":           "10.0.0.1/8\n",
		"non canonical v6":    "2001:0db8::/32\n",
		"non canonical v4":    "010.0.0.0/8\n",
		"duplicate cidr":      "10.0.0.0/8\n10.0.0.0/8\n",
		"unsorted cidr":       "192.168.0.0/16\n10.0.0.0/8\n",
		"unsorted cidr bits":  "10.0.0.0/16\n10.0.0.0/8\n",
		"domain after cidr":   "10.0.0.0/8\na.com\n",
		"v4 after v6":         "2001:db8::/32\n10.0.0.0/8\n",
		"bare ipv6":           "2001:db8::1\n",
		"invalid cidr":        "10.0.0.0/33\n",
	}
	for name, data := range cases {
		if err := Validate([]byte(data), KindCombined); err == nil {
			t.Errorf("%s: expected error for %q", name, data)
		}
	}
}

func TestValidateAcceptsSingleLabel(t *testing.T) {
	if err := Validate([]byte("ru\nsu\nxn--p1ai\n"), KindDomains); err != nil {
		t.Fatal(err)
	}
}

func TestWriteReadValidateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "ru.txt")
	lines := sample().Lines(KindCombined)
	if err := Write(path, lines); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strings.Join(lines, "\n")+"\n" {
		t.Fatalf("unexpected content %q", data)
	}
	if err := ValidateFile(path, KindCombined); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLines(path)
	if err != nil || !reflect.DeepEqual(got, lines) {
		t.Fatalf("ReadLines = %v, %v", got, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
	if err := Write(filepath.Join(dir, "empty.txt"), nil); err == nil {
		t.Fatal("writing an empty list must fail")
	}
	if _, err := ReadLines(filepath.Join(dir, "missing.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}
