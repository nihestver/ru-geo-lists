package cidr

import (
	"math/rand"
	"net/netip"
	"reflect"
	"testing"
)

func p(s string) netip.Prefix { return netip.MustParsePrefix(s) }

func ps(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, p(s))
	}
	return out
}

func TestFromBytes(t *testing.T) {
	got, err := FromBytes([]byte{10, 1, 2, 3}, 8)
	if err != nil || got != p("10.0.0.0/8") {
		t.Fatalf("got %v, %v", got, err)
	}
	v6 := netip.MustParseAddr("2a02:6b8:0:1::5").AsSlice()
	got, err = FromBytes(v6, 32)
	if err != nil || got != p("2a02:6b8::/32") {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := FromBytes([]byte{1, 2, 3}, 8); err == nil {
		t.Fatal("expected error for 3-byte ip")
	}
	if _, err := FromBytes([]byte{1, 2, 3, 4}, 33); err == nil {
		t.Fatal("expected error for /33")
	}
	if _, err := FromBytes(v6, 129); err == nil {
		t.Fatal("expected error for /129")
	}
}

func TestSortOrder(t *testing.T) {
	in := ps("2a02:6b8::/32", "10.0.0.0/8", "::/0", "10.0.0.0/16", "1.2.3.0/24", "0.0.0.0/0", "2001:db8::/32")
	Sort(in)
	want := ps("0.0.0.0/0", "1.2.3.0/24", "10.0.0.0/8", "10.0.0.0/16", "::/0", "2001:db8::/32", "2a02:6b8::/32")
	if !reflect.DeepEqual(in, want) {
		t.Fatalf("got %v want %v", in, want)
	}
}

func TestNormalize(t *testing.T) {
	in := ps("10.0.0.5/24", "10.0.0.0/24", "10.0.0.0/8", "2a02:6b8:0:1::5/32")
	want := ps("10.0.0.0/8", "10.0.0.0/24", "2a02:6b8::/32")
	if got := Normalize(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAggregateCases(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"siblings merge", []string{"10.0.0.0/25", "10.0.0.128/25"}, []string{"10.0.0.0/24"}},
		{"covered dropped", []string{"10.0.0.0/8", "10.1.0.0/16", "10.0.0.0/8"}, []string{"10.0.0.0/8"}},
		{"adjacent but unaligned stay", []string{"10.0.1.0/24", "10.0.2.0/24"}, []string{"10.0.1.0/24", "10.0.2.0/24"}},
		{"chain merges up", []string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/23"}, []string{"10.0.0.0/22"}},
		{"unmasked input", []string{"10.0.0.77/24"}, []string{"10.0.0.0/24"}},
		{"top of space", []string{"255.255.254.0/24", "255.255.255.0/24"}, []string{"255.255.254.0/23"}},
		{"halves make everything", []string{"0.0.0.0/1", "128.0.0.0/1"}, []string{"0.0.0.0/0"}},
		{"ipv6 siblings", []string{"2001:db8::/33", "2001:db8:8000::/33"}, []string{"2001:db8::/32"}},
		{"ipv6 top of space", []string{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe/128", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128"}, []string{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe/127"}},
		{"mixed families kept apart", []string{"::/1", "8000::/1", "0.0.0.0/1"}, []string{"0.0.0.0/1", "::/0"}},
		{"empty", nil, nil},
	}
	for _, c := range cases {
		got := Aggregate(ps(c.in...))
		want := ps(c.want...)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v", c.name, got, want)
		}
	}
}

// TestAggregateEquivalence checks, by exhaustive enumeration of a small
// address space, that Aggregate never changes the set of matched addresses
// and that its output is canonical (sorted, disjoint, no mergeable siblings).
func TestAggregateEquivalence(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 50; round++ {
		var in []netip.Prefix
		n := rng.Intn(60) + 1
		for i := 0; i < n; i++ {
			bits := 16 + rng.Intn(17) // 16..32
			addr := netip.AddrFrom4([4]byte{10, 0, byte(rng.Intn(256)), byte(rng.Intn(256))})
			in = append(in, netip.PrefixFrom(addr, bits))
		}
		out := Aggregate(in)
		checkEquivalentV4(t, in, out)
		checkCanonical(t, out, 32)
		if again := Aggregate(out); !reflect.DeepEqual(again, out) {
			t.Fatalf("Aggregate is not idempotent: %v vs %v", again, out)
		}
	}
}

func TestAggregateEquivalenceIPv6(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	base := netip.MustParseAddr("2001:db8::").As16()
	for round := 0; round < 30; round++ {
		var in []netip.Prefix
		n := rng.Intn(60) + 1
		for i := 0; i < n; i++ {
			bits := 112 + rng.Intn(17) // 112..128
			b := base
			b[14], b[15] = byte(rng.Intn(256)), byte(rng.Intn(256))
			in = append(in, netip.PrefixFrom(netip.AddrFrom16(b), bits))
		}
		out := Aggregate(in)
		checkCanonical(t, out, 128)
		// Enumerate the whole 2001:db8::/112 space (65536 addresses).
		for x := 0; x < 65536; x++ {
			b := base
			b[14], b[15] = byte(x>>8), byte(x)
			a := netip.AddrFrom16(b)
			if Contains(in, a) != Contains(out, a) {
				t.Fatalf("address %s: in=%v out=%v (in=%v, out=%v)", a, Contains(in, a), Contains(out, a), in, out)
			}
		}
	}
}

func TestComplement(t *testing.T) {
	cases := []struct {
		name   string
		in     []string
		family int
		want   []string
	}{
		{"half", []string{"0.0.0.0/1"}, 4, []string{"128.0.0.0/1"}},
		{"empty set", nil, 4, []string{"0.0.0.0/0"}},
		{"everything", []string{"0.0.0.0/0"}, 4, nil},
		{"everything in halves", []string{"::/1", "8000::/1"}, 6, nil},
		{"single /8", []string{"10.0.0.0/8"}, 4, []string{"0.0.0.0/5", "8.0.0.0/7", "11.0.0.0/8", "12.0.0.0/6", "16.0.0.0/4", "32.0.0.0/3", "64.0.0.0/2", "128.0.0.0/1"}},
		{"ipv6 ignores ipv4", []string{"10.0.0.0/8", "::/1"}, 6, []string{"8000::/1"}},
		{"last address", []string{"255.255.255.255/32"}, 4, []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3", "224.0.0.0/4", "240.0.0.0/5", "248.0.0.0/6", "252.0.0.0/7", "254.0.0.0/8", "255.0.0.0/9", "255.128.0.0/10", "255.192.0.0/11", "255.224.0.0/12", "255.240.0.0/13", "255.248.0.0/14", "255.252.0.0/15", "255.254.0.0/16", "255.255.0.0/17", "255.255.128.0/18", "255.255.192.0/19", "255.255.224.0/20", "255.255.240.0/21", "255.255.248.0/22", "255.255.252.0/23", "255.255.254.0/24", "255.255.255.0/25", "255.255.255.128/26", "255.255.255.192/27", "255.255.255.224/28", "255.255.255.240/29", "255.255.255.248/30", "255.255.255.252/31", "255.255.255.254/32"}},
	}
	for _, c := range cases {
		got := Complement(ps(c.in...), c.family)
		want := ps(c.want...)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v", c.name, got, want)
		}
	}
}

func TestComplementInvolution(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for round := 0; round < 30; round++ {
		var in []netip.Prefix
		for i := 0; i < rng.Intn(40)+1; i++ {
			bits := 1 + rng.Intn(32)
			addr := netip.AddrFrom4([4]byte{byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256))})
			in = append(in, netip.PrefixFrom(addr, bits))
		}
		comp := Complement(in, 4)
		back := Complement(comp, 4)
		if want := Aggregate(in); !reflect.DeepEqual(back, want) {
			t.Fatalf("complement of complement differs:\n got %v\nwant %v", back, want)
		}
		for i := 0; i < 2000; i++ {
			a := netip.AddrFrom4([4]byte{byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256))})
			if Contains(in, a) == Contains(comp, a) {
				t.Fatalf("address %s is in both or neither of set and complement", a)
			}
		}
		checkCanonical(t, comp, 32)
	}
}

func checkEquivalentV4(t *testing.T, in, out []netip.Prefix) {
	t.Helper()
	var inSet, outSet [65536]bool
	mark := func(set *[65536]bool, prefixes []netip.Prefix) {
		for _, pfx := range prefixes {
			pfx = pfx.Masked()
			b := pfx.Addr().As4()
			start := int(b[2])<<8 | int(b[3])
			size := 1 << (32 - pfx.Bits())
			for x := start; x < start+size && x < 65536; x++ {
				set[x] = true
			}
		}
	}
	mark(&inSet, in)
	mark(&outSet, out)
	if inSet != outSet {
		t.Fatalf("aggregate changed the matched set\n in=%v\nout=%v", in, out)
	}
}

func checkCanonical(t *testing.T, out []netip.Prefix, bitn int) {
	t.Helper()
	for i, pfx := range out {
		if pfx.Masked() != pfx {
			t.Fatalf("prefix %v is not masked", pfx)
		}
		if i == 0 {
			continue
		}
		prev := out[i-1]
		if Compare(prev, pfx) >= 0 {
			t.Fatalf("output not strictly sorted: %v before %v", prev, pfx)
		}
		if prev.Overlaps(pfx) {
			t.Fatalf("output overlaps: %v and %v", prev, pfx)
		}
		if prev.Bits() == pfx.Bits() && pfx.Bits() > 0 {
			parent := netip.PrefixFrom(prev.Addr(), prev.Bits()-1).Masked()
			if parent.Contains(pfx.Addr()) {
				t.Fatalf("output has mergeable siblings: %v and %v", prev, pfx)
			}
		}
	}
}
