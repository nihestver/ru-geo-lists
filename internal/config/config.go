// Package config loads the declarative YAML configuration of geo2list.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nihestver/ru-geo-lists/internal/listfile"
)

// Config is the root of config.yaml.
type Config struct {
	Sources Sources `yaml:"sources"`
	Output  Output  `yaml:"output"`
	Safety  Safety  `yaml:"safety"`
	Lists   []List  `yaml:"lists"`
}

// Sources describes where geosite.dat and geoip.dat come from.
type Sources struct {
	GeoSite Source `yaml:"geosite"`
	GeoIP   Source `yaml:"geoip"`
}

// Source is one downloadable upstream file.
type Source struct {
	URL string `yaml:"url"`
	// ChecksumURL points to a "<sha256 hex>  <name>" file; empty disables verification.
	ChecksumURL string `yaml:"checksum_url"`
	// MinSizeBytes makes a suspiciously small download fail.
	MinSizeBytes int64 `yaml:"min_size_bytes"`
}

// Output controls where and how lists are written.
type Output struct {
	Dir string `yaml:"dir"`
	// Extensions lists the file extensions to publish. Every list file is
	// written once per extension with byte-identical content.
	Extensions []string `yaml:"extensions"`
	// AggregateCIDRs merges adjacent and overlapping prefixes (never changes
	// the set of matched addresses).
	AggregateCIDRs bool `yaml:"aggregate_cidrs"`
}

// Safety protects the published lists from a broken upstream.
type Safety struct {
	// MaxChangeRatio is the largest accepted relative change of a file's line
	// count compared with the previously published version (0.3 = 30%).
	MaxChangeRatio float64 `yaml:"max_change_ratio"`
	// SmallChangeLines: changes of at most this many lines are always accepted,
	// so that tiny lists are not blocked by a handful of entries.
	SmallChangeLines int `yaml:"small_change_lines"`
}

// List describes one published list and its output files.
type List struct {
	Name         string   `yaml:"name"`
	GeoSite      []string `yaml:"geosite"`
	GeoIP        []string `yaml:"geoip"`
	ExtraDomains []string `yaml:"extra_domains"`
	Outputs      []string `yaml:"outputs"`
}

// Kinds returns the parsed output kinds of the list.
func (l List) Kinds() ([]listfile.Kind, error) {
	kinds := make([]listfile.Kind, 0, len(l.Outputs))
	for _, o := range l.Outputs {
		k, err := listfile.ParseKind(o)
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, k)
	}
	return kinds, nil
}

// Default returns the configuration used for keys that are absent from the file.
func Default() Config {
	return Config{
		Output: Output{Dir: "lists", Extensions: []string{"txt"}, AggregateCIDRs: true},
		Safety: Safety{MaxChangeRatio: 0.30, SmallChangeLines: 10},
	}
}

// Load reads and validates a YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes and validates YAML configuration data. Unknown keys are errors.
func Parse(data []byte) (*Config, error) {
	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	extRe  = regexp.MustCompile(`^[a-z0-9]+$`)
)

// Validate checks the configuration for consistency.
func (c *Config) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Output.Dir == "" {
		fail("output.dir must not be empty")
	}
	if len(c.Output.Extensions) == 0 {
		fail("output.extensions must not be empty")
	}
	seenExt := map[string]bool{}
	for _, ext := range c.Output.Extensions {
		if !extRe.MatchString(ext) {
			fail("output.extensions: %q must be letters/digits without a dot", ext)
		}
		if seenExt[ext] {
			fail("output.extensions: duplicate %q", ext)
		}
		seenExt[ext] = true
	}
	if c.Safety.MaxChangeRatio <= 0 || c.Safety.MaxChangeRatio > 100 {
		fail("safety.max_change_ratio %v must be in (0, 100]", c.Safety.MaxChangeRatio)
	}
	if c.Safety.SmallChangeLines < 0 {
		fail("safety.small_change_lines must not be negative")
	}
	if c.Sources.GeoSite.MinSizeBytes < 0 || c.Sources.GeoIP.MinSizeBytes < 0 {
		fail("sources.*.min_size_bytes must not be negative")
	}
	if len(c.Lists) == 0 {
		fail("lists must not be empty")
	}
	seen := map[string]bool{}
	needSite, needIP := false, false
	for i, l := range c.Lists {
		where := fmt.Sprintf("lists[%d] (%q)", i, l.Name)
		if !nameRe.MatchString(l.Name) {
			fail("%s: name must match %s", where, nameRe)
		}
		if seen[l.Name] {
			fail("%s: duplicate name", where)
		}
		seen[l.Name] = true
		for _, code := range append(append([]string{}, l.GeoSite...), l.GeoIP...) {
			if strings.TrimSpace(code) == "" {
				fail("%s: empty category code", where)
			}
		}
		for _, d := range l.ExtraDomains {
			if strings.TrimSpace(d) == "" {
				fail("%s: empty extra domain", where)
			}
		}
		hasDomains := len(l.GeoSite) > 0 || len(l.ExtraDomains) > 0
		hasIPs := len(l.GeoIP) > 0
		if !hasDomains && !hasIPs {
			fail("%s: needs geosite, geoip or extra_domains", where)
		}
		if len(l.Outputs) == 0 {
			fail("%s: outputs must not be empty", where)
		}
		seenKind := map[string]bool{}
		for _, o := range l.Outputs {
			k, err := listfile.ParseKind(o)
			if err != nil {
				fail("%s: %v", where, err)
				continue
			}
			if seenKind[o] {
				fail("%s: duplicate output %q", where, o)
			}
			seenKind[o] = true
			switch k {
			case listfile.KindDomains:
				if !hasDomains {
					fail("%s: output %q needs geosite or extra_domains", where, o)
				}
			case listfile.KindIPv4, listfile.KindIPv6:
				if !hasIPs {
					fail("%s: output %q needs geoip", where, o)
				}
			}
		}
		needSite = needSite || len(l.GeoSite) > 0
		needIP = needIP || hasIPs
	}
	if needSite && c.Sources.GeoSite.URL == "" {
		fail("sources.geosite.url is required by the configured lists")
	}
	if needIP && c.Sources.GeoIP.URL == "" {
		fail("sources.geoip.url is required by the configured lists")
	}
	return errors.Join(errs...)
}
