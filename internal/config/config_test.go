package config

import (
	"strings"
	"testing"
)

const valid = `
sources:
  geosite:
    url: https://example.com/geosite.dat
    checksum_url: https://example.com/geosite.dat.sha256sum
    min_size_bytes: 100
  geoip:
    url: https://example.com/geoip.dat
output:
  extensions: [lst]
lists:
  - name: ru
    geosite: [category-ru]
    geoip: [ru]
    extra_domains: [dion.vc]
    outputs: [combined, domains, ipv4, ipv6]
  - name: youtube
    geosite: [youtube]
    outputs: [combined]
`

func TestParseValidAndDefaults(t *testing.T) {
	cfg, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output.Dir != "lists" || strings.Join(cfg.Output.Extensions, ",") != "lst" || !cfg.Output.AggregateCIDRs {
		t.Fatalf("defaults not applied: %+v", cfg.Output)
	}
	if cfg.Safety.MaxChangeRatio != 0.30 || cfg.Safety.SmallChangeLines != 10 {
		t.Fatalf("safety defaults not applied: %+v", cfg.Safety)
	}
	if len(cfg.Lists) != 2 || cfg.Lists[0].ExtraDomains[0] != "dion.vc" {
		t.Fatalf("lists not parsed: %+v", cfg.Lists)
	}
	kinds, err := cfg.Lists[0].Kinds()
	if err != nil || len(kinds) != 4 {
		t.Fatalf("Kinds = %v, %v", kinds, err)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"unknown key":         strings.Replace(valid, "extensions: [lst]", "extensions: [lst]\n  colour: red", 1),
		"dotted extension":    strings.Replace(valid, "extensions: [lst]", "extensions: [.lst]", 1),
		"no extensions":       strings.Replace(valid, "extensions: [lst]", "extensions: []", 1),
		"duplicate extension": strings.Replace(valid, "extensions: [lst]", "extensions: [lst, lst]", 1),
		"scalar extensions":   strings.Replace(valid, "extensions: [lst]", "extensions: lst", 1),
		"bad name":            strings.Replace(valid, "name: ru", "name: RU_list", 1),
		"duplicate name":      strings.Replace(valid, "name: youtube", "name: ru", 1),
		"unknown output":      strings.Replace(valid, "outputs: [combined]", "outputs: [all]", 1),
		"duplicate output":    strings.Replace(valid, "outputs: [combined]", "outputs: [combined, combined]", 1),
		"ipv4 without geoip":  strings.Replace(valid, "outputs: [combined]", "outputs: [ipv4]", 1),
		"no sources for list": strings.Replace(valid, "geosite: [youtube]", "", 1),
		"no lists":            strings.SplitN(valid, "lists:", 2)[0] + "lists: []\n",
		"zero ratio":          valid + "safety:\n  max_change_ratio: 0\n",
		"missing geoip url":   strings.Replace(valid, "url: https://example.com/geoip.dat", "url: \"\"", 1),
		"empty code":          strings.Replace(valid, "geoip: [ru]", "geoip: [\"\"]", 1),
		"not yaml":            "lists: [",
	}
	for name, data := range cases {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
