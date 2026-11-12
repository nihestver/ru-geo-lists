package custom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prefixes(e Entries) string {
	var out []string
	for _, p := range e.Prefixes {
		out = append(out, p.String())
	}
	return strings.Join(out, ",")
}

func TestParse(t *testing.T) {
	data := "\xEF\xBB\xBF# my entries\r\n" +
		"\r\n" +
		"Example.COM   # trailing comment\r\n" +
		"  *.sub.example.org.\n" +
		"domain:пример.рф\n" +
		"inno.local\n" +
		"example.com\n" +
		"\t1.2.3.4\n" +
		"10.1.2.3/8\n" +
		"2001:DB8::1\n" +
		"2001:db8:1::/48\n" +
		"::ffff:192.0.2.7\n" +
		"::ffff:198.51.100.0/120\n" +
		"#203.0.113.0/24\n"
	e, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(e.Domains, ","), "example.com,sub.example.org,xn--e1afmkfd.xn--p1ai,inno.local,example.com"; got != want {
		t.Errorf("domains:\n got %s\nwant %s", got, want)
	}
	if got, want := prefixes(e), "1.2.3.4/32,10.0.0.0/8,2001:db8::1/128,2001:db8:1::/48,192.0.2.7/32,198.51.100.0/24"; got != want {
		t.Errorf("prefixes:\n got %s\nwant %s", got, want)
	}
}

func TestParseEmpty(t *testing.T) {
	for _, data := range []string{"", "\n", "# only comments\n\n   \n#\n"} {
		e, err := Parse([]byte(data))
		if err != nil || len(e.Domains) != 0 || len(e.Prefixes) != 0 {
			t.Errorf("Parse(%q) = %+v, %v", data, e, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"ok.com\ntwo words\n":             "2: ",
		"10.0.0.0/33\n":                   "1: invalid CIDR",
		"example.com/path\n":              "1: invalid CIDR",
		"https://example.com\n":           "1: invalid CIDR",
		"\n\nbad_domain!.com\n":           "3: ",
		"fe80::1%eth0\n":                  "has a zone",
		"1.2.3.4.5\n":                     "is not a domain, IP address or CIDR",
		"a.com\nb.com\n-bad-.com\n":       "3: ",
		"example.com, example.org\n":      "1: ",
		"10.0.0.1-10.0.0.9\n":             "1: ",
		"2001:db8::/129\n":                "1: invalid CIDR",
		"ok.com\n1.2.3.4:8080\n":          "2: ",
		"ok.com\n" + "\n" + "300.1.1.1\n": "3: ",
	}
	for data, want := range cases {
		_, err := Parse([]byte(data))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q): got %v, want error containing %q", data, err, want)
		}
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.txt")
	if err := os.WriteFile(path, []byte("a.com\nnot a domain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err == nil || !strings.Contains(err.Error(), path+":2: ") {
		t.Errorf("error should name the file and line, got %v", err)
	}
	if _, err := ReadFile(filepath.Join(dir, "missing.txt")); !os.IsNotExist(err) {
		t.Errorf("missing file: got %v", err)
	}
}
