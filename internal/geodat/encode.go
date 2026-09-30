package geodat

import "google.golang.org/protobuf/encoding/protowire"

// MarshalGeoSiteList encodes entries as a GeoSiteList message. It is used to
// build test fixtures and to round-trip real files in tests.
func MarshalGeoSiteList(entries []GeoSite) []byte {
	var b []byte
	for _, e := range entries {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, marshalGeoSite(e))
	}
	return b
}

// MarshalGeoIPList encodes entries as a GeoIPList message.
func MarshalGeoIPList(entries []GeoIP) []byte {
	var b []byte
	for _, e := range entries {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, marshalGeoIP(e))
	}
	return b
}

func marshalGeoSite(e GeoSite) []byte {
	var b []byte
	if e.Code != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, e.Code)
	}
	for _, d := range e.Domains {
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendBytes(b, marshalDomain(d))
	}
	return b
}

func marshalDomain(d Domain) []byte {
	var b []byte
	if d.Type != 0 {
		b = protowire.AppendTag(b, 1, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(d.Type))
	}
	if d.Value != "" {
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendString(b, d.Value)
	}
	for _, a := range d.Attributes {
		b = protowire.AppendTag(b, 3, protowire.BytesType)
		b = protowire.AppendBytes(b, marshalAttribute(a))
	}
	return b
}

func marshalAttribute(a Attribute) []byte {
	var b []byte
	if a.Key != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, a.Key)
	}
	if a.Bool != nil {
		b = protowire.AppendTag(b, 2, protowire.VarintType)
		b = protowire.AppendVarint(b, protowire.EncodeBool(*a.Bool))
	}
	if a.Int != nil {
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(*a.Int))
	}
	return b
}

func marshalGeoIP(e GeoIP) []byte {
	var b []byte
	if e.Code != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, e.Code)
	}
	for _, c := range e.CIDRs {
		var cb []byte
		cb = protowire.AppendTag(cb, 1, protowire.BytesType)
		cb = protowire.AppendBytes(cb, c.IP)
		if c.Prefix != 0 {
			cb = protowire.AppendTag(cb, 2, protowire.VarintType)
			cb = protowire.AppendVarint(cb, uint64(c.Prefix))
		}
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendBytes(b, cb)
	}
	if e.InverseMatch {
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	return b
}
