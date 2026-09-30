// Package cidr normalizes, sorts, aggregates and complements sets of IP
// prefixes. All operations preserve the set of matched addresses exactly.
package cidr

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"math/bits"
	"net/netip"
	"slices"
)

// FromBytes builds a masked prefix from the raw ip bytes (4 or 16) and prefix
// length stored in a geoip.dat CIDR message.
func FromBytes(ip []byte, prefix uint32) (netip.Prefix, error) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Prefix{}, fmt.Errorf("invalid ip length %d", len(ip))
	}
	if int(prefix) > addr.BitLen() {
		return netip.Prefix{}, fmt.Errorf("prefix length %d too long for %s", prefix, addr)
	}
	return netip.PrefixFrom(addr, int(prefix)).Masked(), nil
}

// Compare orders prefixes deterministically: IPv4 before IPv6, then by
// address, then by prefix length.
func Compare(a, b netip.Prefix) int {
	if c := a.Addr().Compare(b.Addr()); c != 0 {
		return c
	}
	return cmp.Compare(a.Bits(), b.Bits())
}

// Sort sorts prefixes in place using Compare.
func Sort(ps []netip.Prefix) {
	slices.SortFunc(ps, Compare)
}

// Split separates prefixes into IPv4 and IPv6 (IPv4-mapped IPv6 addresses
// count as IPv6, as they do in the source format).
func Split(ps []netip.Prefix) (v4, v6 []netip.Prefix) {
	for _, p := range ps {
		if p.Addr().Is4() {
			v4 = append(v4, p)
		} else {
			v6 = append(v6, p)
		}
	}
	return v4, v6
}

// Normalize masks every prefix, removes duplicates and sorts. It does not
// merge prefixes.
func Normalize(ps []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Masked())
	}
	Sort(out)
	return slices.Compact(out)
}

// Aggregate returns the smallest sorted set of prefixes that covers exactly
// the same addresses as ps: duplicates and prefixes covered by a larger
// prefix are dropped, and adjacent prefixes are merged when the union is
// itself a prefix.
func Aggregate(ps []netip.Prefix) []netip.Prefix {
	v4, v6 := Split(ps)
	out := rangesToPrefixes(mergeIntervals(toIntervals(v4, 32)), 32)
	out = append(out, rangesToPrefixes(mergeIntervals(toIntervals(v6, 128)), 128)...)
	return out
}

// Complement returns the sorted prefixes covering every address of the given
// family (4 or 6) that ps does not cover. Prefixes of the other family are
// ignored.
func Complement(ps []netip.Prefix, family int) []netip.Prefix {
	v4, v6 := Split(ps)
	var (
		in   []netip.Prefix
		bitn int
	)
	switch family {
	case 4:
		in, bitn = v4, 32
	case 6:
		in, bitn = v6, 128
	default:
		panic(fmt.Sprintf("cidr: unknown family %d", family))
	}
	all := lowMask(bitn)
	var gaps []interval
	cursor := u128{}
	done := false
	for _, iv := range mergeIntervals(toIntervals(in, bitn)) {
		if iv.lo.cmp(cursor) > 0 {
			gaps = append(gaps, interval{cursor, iv.lo.sub(one)})
		}
		if iv.hi.cmp(all) == 0 {
			done = true
			break
		}
		cursor = iv.hi.add(one)
	}
	if !done {
		gaps = append(gaps, interval{cursor, all})
	}
	return rangesToPrefixes(gaps, bitn)
}

// Contains reports whether any prefix in ps contains addr.
func Contains(ps []netip.Prefix, addr netip.Addr) bool {
	for _, p := range ps {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// u128 is an unsigned 128-bit integer holding an IPv4 (low 32 bits) or IPv6
// address.
type u128 struct{ hi, lo uint64 }

var one = u128{0, 1}

func (a u128) cmp(b u128) int {
	if c := cmp.Compare(a.hi, b.hi); c != 0 {
		return c
	}
	return cmp.Compare(a.lo, b.lo)
}

func (a u128) add(b u128) u128 {
	lo, carry := bits.Add64(a.lo, b.lo, 0)
	hi, _ := bits.Add64(a.hi, b.hi, carry)
	return u128{hi, lo}
}

func (a u128) sub(b u128) u128 {
	lo, borrow := bits.Sub64(a.lo, b.lo, 0)
	hi, _ := bits.Sub64(a.hi, b.hi, borrow)
	return u128{hi, lo}
}

func (a u128) or(b u128) u128 {
	return u128{a.hi | b.hi, a.lo | b.lo}
}

// trailingZeros returns the number of trailing zero bits (128 for zero).
func (a u128) trailingZeros() int {
	if a.lo != 0 {
		return bits.TrailingZeros64(a.lo)
	}
	return 64 + bits.TrailingZeros64(a.hi)
}

// lowMask returns a value with the n lowest bits set.
func lowMask(n int) u128 {
	switch {
	case n <= 0:
		return u128{}
	case n < 64:
		return u128{0, 1<<uint(n) - 1}
	case n == 64:
		return u128{0, ^uint64(0)}
	case n < 128:
		return u128{1<<uint(n-64) - 1, ^uint64(0)}
	default:
		return u128{^uint64(0), ^uint64(0)}
	}
}

func addrToU128(a netip.Addr) u128 {
	if a.Is4() {
		b := a.As4()
		return u128{0, uint64(binary.BigEndian.Uint32(b[:]))}
	}
	b := a.As16()
	return u128{binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:])}
}

func u128ToAddr(v u128, bitn int) netip.Addr {
	if bitn == 32 {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(v.lo))
		return netip.AddrFrom4(b)
	}
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], v.hi)
	binary.BigEndian.PutUint64(b[8:], v.lo)
	return netip.AddrFrom16(b)
}

// interval is an inclusive address range.
type interval struct{ lo, hi u128 }

func toIntervals(ps []netip.Prefix, bitn int) []interval {
	out := make([]interval, 0, len(ps))
	for _, p := range ps {
		p = p.Masked()
		lo := addrToU128(p.Addr())
		out = append(out, interval{lo, lo.or(lowMask(bitn - p.Bits()))})
	}
	return out
}

// mergeIntervals sorts intervals and merges the ones that overlap or touch.
func mergeIntervals(ivs []interval) []interval {
	if len(ivs) == 0 {
		return nil
	}
	slices.SortFunc(ivs, func(a, b interval) int {
		if c := a.lo.cmp(b.lo); c != 0 {
			return c
		}
		return a.hi.cmp(b.hi)
	})
	out := []interval{ivs[0]}
	for _, iv := range ivs[1:] {
		last := &out[len(out)-1]
		// iv touches or overlaps last when iv.lo <= last.hi+1; guard the
		// increment against wrapping at the top of the address space.
		if last.hi.cmp(lowMask(128)) == 0 || iv.lo.cmp(last.hi.add(one)) <= 0 {
			if iv.hi.cmp(last.hi) > 0 {
				last.hi = iv.hi
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// rangesToPrefixes converts disjoint sorted intervals into the minimal list of
// prefixes covering them.
func rangesToPrefixes(ivs []interval, bitn int) []netip.Prefix {
	var out []netip.Prefix
	for _, iv := range ivs {
		start, end := iv.lo, iv.hi
		for {
			// Largest block aligned at start, then shrink until it fits in [start, end].
			tz := min(start.trailingZeros(), bitn)
			plen := bitn - tz
			var last u128
			for {
				last = start.or(lowMask(bitn - plen))
				if last.cmp(end) <= 0 {
					break
				}
				plen++
			}
			out = append(out, netip.PrefixFrom(u128ToAddr(start, bitn), plen))
			if last.cmp(end) == 0 {
				break
			}
			start = last.add(one)
		}
	}
	return out
}
