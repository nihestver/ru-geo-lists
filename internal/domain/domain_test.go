package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"example.com", "example.com"},
		{"Example.COM", "example.com"},
		{"  example.com \n", "example.com"},
		{"domain:example.com", "example.com"},
		{"full:www.example.com", "www.example.com"},
		{"*.example.com", "example.com"},
		{".example.com", "example.com"},
		{"domain:*.example.com.", "example.com"},
		{"example.com.", "example.com"},
		{"пример.рф", "xn--e1afmkfd.xn--p1ai"},
		{"ПРИМЕР.РФ", "xn--e1afmkfd.xn--p1ai"},
		{"xn--e1afmkfd.xn--p1ai", "xn--e1afmkfd.xn--p1ai"},
		{"рф", "xn--p1ai"},
		{"ru", "ru"},
		{"_dmarc.example.com", "_dmarc.example.com"},
		{"inno.local", "inno.local"},
		{"a-b.c", "a-b.c"},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if err != nil {
			t.Errorf("Normalize(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeRejects(t *testing.T) {
	bad := []string{
		"",
		"   ",
		".",
		"domain:",
		"*.",
		"exa mple.com",
		"example..com",
		"-example.com",
		"example-.com",
		"exa$mple.com",
		"xn--zzzzzzzzzzzzzzzzzz-.com",
		"a." + strings.Repeat("b", 64) + ".com",
		strings.Repeat("a.", 130) + "com",
		"ab\uFFFDc.com", // disallowed rune
		"\u0301abc.com", // leading combining mark
	}
	for _, in := range bad {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, want error", in, got)
		}
	}
}

func TestValidateRejectsNonCanonical(t *testing.T) {
	bad := []string{"Example.com", "пример.рф", "example.com.", ".example.com", "xn--@@.com", "1.2.3.4", "example.123", "2001:db8::1"}
	for _, in := range bad {
		if err := Validate(in); err == nil {
			t.Errorf("Validate(%q) = nil, want error", in)
		}
	}
	good := []string{"example.com", "ru", "xn--p1ai", "a_b.example.org", "1.2.3.4.in-addr.arpa", "youtube"}
	for _, in := range good {
		if err := Validate(in); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", in, err)
		}
	}
}

func TestKnownTLD(t *testing.T) {
	known := []string{"ru", "su", "xn--p1ai", "yandex", "moscow", "tatar", "youtube", "example.com", "a.b.co.uk", "dion.vc", "inno.tech",
		"youtube.com.jm", "jm", "x.jm", "foo.github.io", "github.io"}
	for _, d := range known {
		if !KnownTLD(d) {
			t.Errorf("KnownTLD(%q) = false, want true", d)
		}
	}
	unknown := []string{"inno.local", "host.internal", "foo.example", "cromulent", "x.onion-not-real"}
	for _, d := range unknown {
		if KnownTLD(d) {
			t.Errorf("KnownTLD(%q) = true, want false", d)
		}
	}
}

func TestIsSingleLabel(t *testing.T) {
	if !IsSingleLabel("ru") || IsSingleLabel("mail.ru") {
		t.Fatal("IsSingleLabel misbehaves")
	}
}

func TestSetMinimal(t *testing.T) {
	s := NewSet()
	for _, d := range []string{
		"mail.ru", "yandex.ru", "ru", // both covered by "ru"
		"example.com", "www.example.com", "a.b.example.com", // covered by example.com
		"example.org", "notexample.org", // notexample.org is NOT covered by example.org
		"xn--p1ai", "xn--e1afmkfd.xn--p1ai",
		"telegram.org", "telegram.org", // duplicate
	} {
		s.Add(d)
	}
	want := []string{"example.com", "example.org", "notexample.org", "ru", "telegram.org", "xn--p1ai"}
	if got := s.Minimal(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Minimal() = %v, want %v", got, want)
	}
	wantCovered := []string{"a.b.example.com", "mail.ru", "www.example.com", "xn--e1afmkfd.xn--p1ai", "yandex.ru"}
	if got := s.Covered(); !reflect.DeepEqual(got, wantCovered) {
		t.Fatalf("Covered() = %v, want %v", got, wantCovered)
	}
	if s.Len() != 11 {
		t.Fatalf("Len() = %d, want 11", s.Len())
	}
}

func TestMinimalIsSortedAndDeterministic(t *testing.T) {
	s := NewSet()
	for _, d := range []string{"b.com", "a.com", "c.com", "0.com", "xn--p1ai", "a-b.com", "a.b.com"} {
		s.Add(d)
	}
	first := s.Minimal()
	want := []string{"0.com", "a-b.com", "a.com", "b.com", "c.com", "xn--p1ai"}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("Minimal() = %v, want %v", first, want)
	}
	for i := 0; i < 20; i++ {
		if got := s.Minimal(); !reflect.DeepEqual(got, first) {
			t.Fatalf("Minimal() is not deterministic: %v vs %v", got, first)
		}
	}
}
