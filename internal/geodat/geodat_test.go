package geodat

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func boolPtr(v bool) *bool  { return &v }
func intPtr(v int64) *int64 { return &v }
func ip(s string) []byte    { a := netip.MustParseAddr(s); return a.AsSlice() }

func TestGeoSiteRoundTrip(t *testing.T) {
	in := []GeoSite{
		{Code: "CATEGORY-RU", Domains: []Domain{
			{Type: DomainRootDomain, Value: "yandex.ru"},
			{Type: DomainFull, Value: "mail.ru", Attributes: []Attribute{{Key: "ads", Bool: boolPtr(true)}}},
			{Type: DomainPlain, Value: "sber"},
			{Type: DomainRegex, Value: `^vk\d+\.com$`, Attributes: []Attribute{{Key: "rank", Int: intPtr(7)}}},
		}},
		{Code: "EMPTY"},
	}
	out, err := ParseGeoSiteList(MarshalGeoSiteList(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
}

func TestGeoIPRoundTrip(t *testing.T) {
	in := []GeoIP{
		{Code: "RU", CIDRs: []CIDR{{IP: ip("5.8.0.0"), Prefix: 19}, {IP: ip("2a02:6b8::"), Prefix: 32}}},
		{Code: "NOT-RU", CIDRs: []CIDR{{IP: ip("10.0.0.0"), Prefix: 8}}, InverseMatch: true},
	}
	out, err := ParseGeoIPList(MarshalGeoIPList(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
}

func TestCodeFallbackToNewField(t *testing.T) {
	// Newer schema: "code" (field 4 for GeoSite, field 5 for GeoIP) instead of country_code.
	var site []byte
	site = protowire.AppendTag(site, 4, protowire.BytesType)
	site = protowire.AppendString(site, "new-style")
	var list []byte
	list = protowire.AppendTag(list, 1, protowire.BytesType)
	list = protowire.AppendBytes(list, site)
	entries, err := ParseGeoSiteList(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Code != "new-style" {
		t.Fatalf("got %+v", entries)
	}

	var geo []byte
	geo = protowire.AppendTag(geo, 5, protowire.BytesType)
	geo = protowire.AppendString(geo, "geo-new")
	list = nil
	list = protowire.AppendTag(list, 1, protowire.BytesType)
	list = protowire.AppendBytes(list, geo)
	ips, err := ParseGeoIPList(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0].Code != "geo-new" {
		t.Fatalf("got %+v", ips)
	}
}

func TestUnknownFieldsAreSkipped(t *testing.T) {
	// A fixed64 field and an unknown length-delimited field must not break parsing.
	var site []byte
	site = protowire.AppendTag(site, 1, protowire.BytesType)
	site = protowire.AppendString(site, "X")
	site = protowire.AppendTag(site, 99, protowire.Fixed64Type)
	site = protowire.AppendFixed64(site, 12345)
	site = protowire.AppendTag(site, 100, protowire.BytesType)
	site = protowire.AppendString(site, "ignored")
	var list []byte
	list = protowire.AppendTag(list, 1, protowire.BytesType)
	list = protowire.AppendBytes(list, site)
	entries, err := ParseGeoSiteList(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Code != "X" {
		t.Fatalf("got %+v", entries)
	}
}

func TestBadIPLength(t *testing.T) {
	in := []GeoIP{{Code: "BAD", CIDRs: []CIDR{{IP: []byte{1, 2, 3}, Prefix: 8}}}}
	if _, err := ParseGeoIPList(MarshalGeoIPList(in)); err == nil {
		t.Fatal("expected error for 3-byte ip")
	}
}

func TestTruncatedInput(t *testing.T) {
	b := MarshalGeoSiteList([]GeoSite{{Code: "RU", Domains: []Domain{{Type: DomainRootDomain, Value: "example.ru"}}}})
	if _, err := ParseGeoSiteList(b[:len(b)-3]); err == nil {
		t.Fatal("expected error for truncated input")
	}
}

func TestFindIsCaseInsensitive(t *testing.T) {
	sites := []GeoSite{{Code: "CATEGORY-RU"}, {Code: "TELEGRAM"}}
	if _, err := FindGeoSite(sites, "category-ru"); err != nil {
		t.Fatal(err)
	}
	_, err := FindGeoSite(sites, "telegramm")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if got := err.Error(); !contains(got, "telegram") {
		t.Fatalf("error should suggest similar codes, got %q", got)
	}
	ips := []GeoIP{{Code: "RU"}, {Code: "RU-BLOCKED"}}
	if _, err := FindGeoIP(ips, "Ru"); err != nil {
		t.Fatal(err)
	}
}

func TestSimilarCodes(t *testing.T) {
	codes := []string{"CATEGORY-RU", "CATEGORY-RU-BLOCKED", "RU", "TELEGRAM", "YOUTUBE", "GOOGLE"}
	got := SimilarCodes(codes, "category-r", 3)
	want := []string{"category-ru", "category-ru-blocked"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := SimilarCodes(codes, "zzzzzzzz", 5); len(got) != 0 {
		t.Fatalf("expected no suggestions, got %v", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
