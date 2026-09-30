// Package build turns geosite.dat/geoip.dat categories into published lists.
package build

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nihestver/ru-geo-lists/internal/cidr"
	"github.com/nihestver/ru-geo-lists/internal/config"
	"github.com/nihestver/ru-geo-lists/internal/domain"
	"github.com/nihestver/ru-geo-lists/internal/geodat"
	"github.com/nihestver/ru-geo-lists/internal/listfile"
)

const maxExamples = 5

// Options configures Run.
type Options struct {
	Config      *config.Config
	GeoSitePath string
	GeoIPPath   string
	// AllowLargeChange turns safety-threshold violations into warnings.
	AllowLargeChange bool
	// DryRun computes and checks everything but writes nothing.
	DryRun bool
	// Logf receives progress messages; nil disables logging.
	Logf func(format string, args ...any)
}

// CategoryStats describes how one geosite category was converted.
type CategoryStats struct {
	Code       string
	Total      int
	RootDomain int
	Full       int
	// Keyword and Regex entries cannot be expressed as one domain per line and are skipped.
	Keyword int
	Regex   int
	// Attributes counts domain attributes, which are ignored.
	Attributes int
	// Invalid counts entries that failed normalization; UnknownTLD counts
	// entries whose top-level domain is not in the Public Suffix List.
	Invalid    int
	UnknownTLD int
	// Examples holds up to five sample values per skipped class
	// ("keyword", "regexp", "invalid", "unknown-tld").
	Examples map[string][]string
}

// Skipped returns the number of entries not written to the list.
func (c CategoryStats) Skipped() int {
	return c.Keyword + c.Regex + c.Invalid + c.UnknownTLD
}

func (c *CategoryStats) example(class, value string) {
	if c.Examples == nil {
		c.Examples = map[string][]string{}
	}
	if len(c.Examples[class]) < maxExamples {
		c.Examples[class] = append(c.Examples[class], value)
	}
}

// GeoIPStats describes how one geoip category was converted.
type GeoIPStats struct {
	Code         string
	Total        int
	IPv4         int
	IPv6         int
	InverseMatch bool
}

// FileStats describes one output file and its change against the previous version.
type FileStats struct {
	Name string
	Path string
	Kind listfile.Kind
	// Lines is the new line count; Previous is the old one (-1 when the file did not exist).
	Lines    int
	Previous int
	Added    int
	Removed  int
	// Violation is set when the change exceeds the safety threshold.
	Violation string
}

// IsNew reports whether the file did not exist before.
func (f FileStats) IsNew() bool { return f.Previous < 0 }

// ListReport describes one configured list.
type ListReport struct {
	Name         string
	Domains      int
	IPv4         int
	IPv6         int
	Categories   []CategoryStats
	GeoIPs       []GeoIPStats
	ExtraDomains []string
	// CoveredRemoved counts subdomains dropped because a parent domain is in the list.
	CoveredRemoved int
	// SingleLabel lists top-level-domain entries such as "ru" for the report.
	SingleLabel []string
	// PrefixesBeforeAggregation is the prefix count before merging.
	PrefixesBeforeAggregation int
	Files                     []FileStats

	content listfile.List
}

// Report is the result of Run.
type Report struct {
	Lists []ListReport
	// Violations lists files whose change exceeds the threshold.
	Violations []string
	// Written is true when files were written to disk.
	Written bool
}

// ErrThreshold is returned (wrapped) when a list changed more than allowed.
var ErrThreshold = errors.New("list size changed more than the safety threshold allows")

// Run converts every configured list, checks the results and, unless DryRun
// is set or a check fails, writes the output files. On a threshold violation
// the returned report is still complete so that it can be shown.
func Run(opt Options) (*Report, error) {
	cfg := opt.Config
	logf := opt.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	needSite, needIP := false, false
	for _, l := range cfg.Lists {
		needSite = needSite || len(l.GeoSite) > 0
		needIP = needIP || len(l.GeoIP) > 0
	}
	var (
		sites []geodat.GeoSite
		ips   []geodat.GeoIP
		err   error
	)
	if needSite {
		if sites, err = geodat.ReadGeoSiteList(opt.GeoSitePath); err != nil {
			return nil, err
		}
		logf("loaded %s: %d categories", opt.GeoSitePath, len(sites))
	}
	if needIP {
		if ips, err = geodat.ReadGeoIPList(opt.GeoIPPath); err != nil {
			return nil, err
		}
		logf("loaded %s: %d categories", opt.GeoIPPath, len(ips))
	}

	rep := &Report{}
	for _, spec := range cfg.Lists {
		lr, err := buildList(spec, cfg, sites, ips, logf)
		if err != nil {
			return nil, fmt.Errorf("list %q: %w", spec.Name, err)
		}
		rep.Lists = append(rep.Lists, *lr)
	}

	for i := range rep.Lists {
		lr := &rep.Lists[i]
		kinds, err := cfg.Lists[i].Kinds()
		if err != nil {
			return nil, err
		}
		for _, k := range kinds {
			name := listfile.FileName(lr.Name, k, cfg.Output.Extension)
			path := filepath.Join(cfg.Output.Dir, name)
			lines := lr.content.Lines(k)
			if len(lines) == 0 {
				return nil, fmt.Errorf("list %q: output %s would be empty", lr.Name, name)
			}
			if err := listfile.Validate(listfile.Encode(lines), k); err != nil {
				return nil, fmt.Errorf("list %q: generated %s is invalid (bug): %w", lr.Name, name, err)
			}
			fs := FileStats{Name: name, Path: path, Kind: k, Lines: len(lines), Previous: -1}
			prev, err := listfile.ReadLines(path)
			switch {
			case err == nil:
				fs.Previous = len(prev)
				fs.Added, fs.Removed = diff(prev, lines)
				if exceeds(fs.Previous, fs.Lines, cfg.Safety) {
					fs.Violation = fmt.Sprintf("%s: %d -> %d lines (%+.1f%%), threshold is %.0f%% (changes of up to %d lines are always allowed)",
						name, fs.Previous, fs.Lines, 100*float64(fs.Lines-fs.Previous)/float64(fs.Previous), 100*cfg.Safety.MaxChangeRatio, cfg.Safety.SmallChangeLines)
					rep.Violations = append(rep.Violations, fs.Violation)
				}
			case errors.Is(err, os.ErrNotExist):
				fs.Added = len(lines)
			default:
				return nil, err
			}
			lr.Files = append(lr.Files, fs)
		}
	}

	if len(rep.Violations) > 0 {
		for _, v := range rep.Violations {
			logf("WARNING: %s", v)
		}
		if !opt.AllowLargeChange {
			return rep, fmt.Errorf("%w:\n  %s", ErrThreshold, strings.Join(rep.Violations, "\n  "))
		}
		logf("continuing because large changes are explicitly allowed")
	}
	if opt.DryRun {
		return rep, nil
	}
	for _, lr := range rep.Lists {
		for _, fs := range lr.Files {
			if err := listfile.Write(fs.Path, lr.content.Lines(fs.Kind)); err != nil {
				return rep, err
			}
			logf("wrote %s (%d lines)", fs.Path, fs.Lines)
		}
	}
	rep.Written = true
	return rep, nil
}

func buildList(spec config.List, cfg *config.Config, sites []geodat.GeoSite, ips []geodat.GeoIP, logf func(string, ...any)) (*ListReport, error) {
	lr := &ListReport{Name: spec.Name}
	set := domain.NewSet()

	for _, code := range spec.GeoSite {
		entry, err := geodat.FindGeoSite(sites, code)
		if err != nil {
			return nil, fmt.Errorf("geosite: %w", err)
		}
		if len(entry.Domains) == 0 {
			return nil, fmt.Errorf("geosite category %q is empty", entry.Code)
		}
		cs := CategoryStats{Code: entry.Code, Total: len(entry.Domains)}
		usable := 0
		for _, d := range entry.Domains {
			cs.Attributes += len(d.Attributes)
			switch d.Type {
			case geodat.DomainPlain:
				cs.Keyword++
				cs.example("keyword", d.Value)
				continue
			case geodat.DomainRegex:
				cs.Regex++
				cs.example("regexp", d.Value)
				continue
			case geodat.DomainRootDomain:
				cs.RootDomain++
			case geodat.DomainFull:
				cs.Full++
			default:
				cs.Invalid++
				cs.example("invalid", fmt.Sprintf("%s (unknown type %d)", d.Value, d.Type))
				continue
			}
			name, err := domain.Normalize(d.Value)
			if err != nil {
				cs.Invalid++
				cs.example("invalid", fmt.Sprintf("%s (%v)", d.Value, err))
				continue
			}
			if !domain.KnownTLD(name) {
				cs.UnknownTLD++
				cs.example("unknown-tld", name)
				continue
			}
			set.Add(name)
			usable++
		}
		if usable == 0 {
			return nil, fmt.Errorf("geosite category %q has %d entries but none can be expressed as a plain domain", entry.Code, cs.Total)
		}
		logf("list %s: geosite %s: %d entries, %d usable, %d skipped (keyword %d, regexp %d, invalid %d, unknown TLD %d), %d attributes ignored",
			spec.Name, entry.Code, cs.Total, usable, cs.Skipped(), cs.Keyword, cs.Regex, cs.Invalid, cs.UnknownTLD, cs.Attributes)
		lr.Categories = append(lr.Categories, cs)
	}

	for _, raw := range spec.ExtraDomains {
		// Extra domains are trusted configuration: no Public Suffix List check.
		name, err := domain.Normalize(raw)
		if err != nil {
			return nil, fmt.Errorf("extra domain %q: %w", raw, err)
		}
		set.Add(name)
		lr.ExtraDomains = append(lr.ExtraDomains, name)
	}

	lr.content.Domains = set.Minimal()
	lr.Domains = len(lr.content.Domains)
	lr.CoveredRemoved = set.Len() - lr.Domains
	for _, d := range lr.content.Domains {
		if domain.IsSingleLabel(d) {
			lr.SingleLabel = append(lr.SingleLabel, d)
		}
	}
	if lr.CoveredRemoved > 0 {
		logf("list %s: %d subdomains removed because a parent domain is present", spec.Name, lr.CoveredRemoved)
	}

	var all []netip.Prefix
	for _, code := range spec.GeoIP {
		entry, err := geodat.FindGeoIP(ips, code)
		if err != nil {
			return nil, fmt.Errorf("geoip: %w", err)
		}
		if len(entry.CIDRs) == 0 {
			return nil, fmt.Errorf("geoip category %q is empty", entry.Code)
		}
		gs := GeoIPStats{Code: entry.Code, Total: len(entry.CIDRs), InverseMatch: entry.InverseMatch}
		var ps []netip.Prefix
		for i, c := range entry.CIDRs {
			p, err := cidr.FromBytes(c.IP, c.Prefix)
			if err != nil {
				return nil, fmt.Errorf("geoip category %q: cidr %d: %w", entry.Code, i, err)
			}
			ps = append(ps, p)
		}
		if entry.InverseMatch {
			logf("WARNING: list %s: geoip %s has inverse_match set; publishing the complement of its %d prefixes", spec.Name, entry.Code, len(ps))
			ps = append(cidr.Complement(ps, 4), cidr.Complement(ps, 6)...)
		}
		v4, v6 := cidr.Split(ps)
		gs.IPv4, gs.IPv6 = len(v4), len(v6)
		logf("list %s: geoip %s: %d prefixes (%d IPv4, %d IPv6)", spec.Name, entry.Code, len(ps), len(v4), len(v6))
		lr.GeoIPs = append(lr.GeoIPs, gs)
		all = append(all, ps...)
	}
	if len(spec.GeoIP) > 0 {
		lr.PrefixesBeforeAggregation = len(all)
		if cfg.Output.AggregateCIDRs {
			all = cidr.Aggregate(all)
		} else {
			all = cidr.Normalize(all)
		}
		lr.content.IPv4, lr.content.IPv6 = cidr.Split(all)
		lr.IPv4, lr.IPv6 = len(lr.content.IPv4), len(lr.content.IPv6)
		if len(all) != lr.PrefixesBeforeAggregation {
			logf("list %s: %d prefixes after aggregation (was %d)", spec.Name, len(all), lr.PrefixesBeforeAggregation)
		}
	}
	return lr, nil
}

func exceeds(prev, cur int, s config.Safety) bool {
	delta := cur - prev
	if delta < 0 {
		delta = -delta
	}
	if delta <= s.SmallChangeLines {
		return false
	}
	return float64(delta) > s.MaxChangeRatio*float64(prev)
}

func diff(prev, cur []string) (added, removed int) {
	old := make(map[string]struct{}, len(prev))
	for _, l := range prev {
		old[l] = struct{}{}
	}
	now := make(map[string]struct{}, len(cur))
	for _, l := range cur {
		now[l] = struct{}{}
		if _, ok := old[l]; !ok {
			added++
		}
	}
	for _, l := range prev {
		if _, ok := now[l]; !ok {
			removed++
		}
	}
	return added, removed
}

func delta(f FileStats) string {
	switch {
	case f.IsNew():
		return "new"
	case f.Added == 0 && f.Removed == 0:
		return "="
	default:
		return fmt.Sprintf("+%d/-%d", f.Added, f.Removed)
	}
}

// Markdown renders the report for $GITHUB_STEP_SUMMARY.
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("## Lists\n\n| File | Lines | Previous | Change |\n|---|---:|---:|---|\n")
	for _, lr := range r.Lists {
		for _, f := range lr.Files {
			prev := "—"
			if !f.IsNew() {
				prev = fmt.Sprint(f.Previous)
			}
			fmt.Fprintf(&b, "| `%s` | %d | %s | %s |\n", f.Name, f.Lines, prev, delta(f))
		}
	}
	b.WriteString("\n## Sources\n\n| List | Domains | IPv4 | IPv6 | Notes |\n|---|---:|---:|---:|---|\n")
	for _, lr := range r.Lists {
		var notes []string
		for _, c := range lr.Categories {
			notes = append(notes, fmt.Sprintf("geosite:%s %d entries (domain %d, full %d; skipped keyword %d, regexp %d, invalid %d, unknown TLD %d; attributes %d)",
				strings.ToLower(c.Code), c.Total, c.RootDomain, c.Full, c.Keyword, c.Regex, c.Invalid, c.UnknownTLD, c.Attributes))
		}
		for _, g := range lr.GeoIPs {
			s := fmt.Sprintf("geoip:%s %d prefixes (IPv4 %d, IPv6 %d)", strings.ToLower(g.Code), g.Total, g.IPv4, g.IPv6)
			if g.InverseMatch {
				s += " **inverse_match: complement published**"
			}
			notes = append(notes, s)
		}
		if len(lr.ExtraDomains) > 0 {
			notes = append(notes, "extra domains: "+strings.Join(lr.ExtraDomains, ", "))
		}
		if lr.CoveredRemoved > 0 {
			notes = append(notes, fmt.Sprintf("%d subdomains removed as covered by a parent", lr.CoveredRemoved))
		}
		if n := lr.PrefixesBeforeAggregation - lr.IPv4 - lr.IPv6; n > 0 {
			notes = append(notes, fmt.Sprintf("%d prefixes merged by aggregation", n))
		}
		if len(lr.SingleLabel) > 0 {
			notes = append(notes, "single-label (TLD) entries: "+strings.Join(lr.SingleLabel, ", "))
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %s |\n", lr.Name, lr.Domains, lr.IPv4, lr.IPv6, strings.Join(notes, "<br>"))
	}
	examples := false
	for _, lr := range r.Lists {
		for _, c := range lr.Categories {
			if len(c.Examples) == 0 {
				continue
			}
			if !examples {
				b.WriteString("\n## Skipped entries (examples)\n\n")
				examples = true
			}
			classes := make([]string, 0, len(c.Examples))
			for class := range c.Examples {
				classes = append(classes, class)
			}
			sort.Strings(classes)
			for _, class := range classes {
				fmt.Fprintf(&b, "- %s / geosite:%s / %s: `%s`\n", lr.Name, strings.ToLower(c.Code), class, strings.Join(c.Examples[class], "`, `"))
			}
		}
	}
	b.WriteString("\n## Safety\n\n")
	if len(r.Violations) == 0 {
		b.WriteString("All files are within the change threshold.\n")
	} else {
		for _, v := range r.Violations {
			fmt.Fprintf(&b, "- :warning: %s\n", v)
		}
	}
	return b.String()
}

// CommitMessage renders a commit title and body with short statistics.
func (r *Report) CommitMessage() string {
	var changed []string
	for _, lr := range r.Lists {
		if len(lr.Files) == 0 {
			continue
		}
		f := lr.Files[0]
		if f.IsNew() || f.Added > 0 || f.Removed > 0 {
			changed = append(changed, fmt.Sprintf("%s %s", lr.Name, delta(f)))
		}
	}
	title := "Update lists"
	if len(changed) > 0 {
		title += ": " + strings.Join(changed, ", ")
	}
	var b strings.Builder
	b.WriteString(title + "\n\n")
	for _, lr := range r.Lists {
		for i, f := range lr.Files {
			fmt.Fprintf(&b, "%s: %d lines (%s)", f.Name, f.Lines, delta(f))
			if i == 0 {
				fmt.Fprintf(&b, "; %d domains, %d IPv4, %d IPv6", lr.Domains, lr.IPv4, lr.IPv6)
			}
			b.WriteString("\n")
		}
	}
	for _, lr := range r.Lists {
		for _, c := range lr.Categories {
			if c.Skipped() > 0 {
				fmt.Fprintf(&b, "%s: geosite:%s skipped %d (keyword %d, regexp %d, invalid %d, unknown TLD %d)\n",
					lr.Name, strings.ToLower(c.Code), c.Skipped(), c.Keyword, c.Regex, c.Invalid, c.UnknownTLD)
			}
		}
	}
	return b.String()
}
