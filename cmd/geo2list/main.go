// Command geo2list converts geosite.dat/geoip.dat categories into plain-text
// domain and CIDR lists.
//
// Subcommands:
//
//	fetch     download geosite.dat and geoip.dat with retries and checksum verification
//	build     convert the configured categories into list files
//	validate  check that list files follow the strict format
//	inspect   print the categories contained in .dat files
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nihestver/ru-geo-lists/internal/build"
	"github.com/nihestver/ru-geo-lists/internal/config"
	"github.com/nihestver/ru-geo-lists/internal/fetch"
	"github.com/nihestver/ru-geo-lists/internal/geodat"
	"github.com/nihestver/ru-geo-lists/internal/listfile"
)

const usageText = `usage: geo2list <command> [flags]

commands:
  fetch      download geosite.dat and geoip.dat (retries, size and sha256 checks)
  build      convert configured categories into list files
  validate   check list files against the strict format
  inspect    print categories of .dat files with entry counts

run "geo2list <command> -h" for the flags of a command.
`

func main() {
	log.SetFlags(0)
	log.SetPrefix("geo2list: ")
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fetch":
		err = runFetch(os.Args[2:])
	case "build":
		err = runBuild(os.Args[2:])
	case "validate":
		err = runValidate(os.Args[2:])
	case "inspect":
		err = runInspect(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usageText)
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "configuration file")
	dir := fs.String("dir", ".cache", "directory for downloaded files")
	attempts := fs.Int("attempts", 4, "download attempts per file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	ctx := context.Background()
	for _, src := range []struct {
		name string
		src  config.Source
	}{{"geosite.dat", cfg.Sources.GeoSite}, {"geoip.dat", cfg.Sources.GeoIP}} {
		if src.src.URL == "" {
			continue
		}
		dst := filepath.Join(*dir, src.name)
		res, err := fetch.Download(ctx, dst, fetch.Options{
			URL: src.src.URL, ChecksumURL: src.src.ChecksumURL, MinSize: src.src.MinSizeBytes,
			Attempts: *attempts, Logf: log.Printf,
		})
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst+".sha256sum", []byte(res.SHA256+"  "+src.name+"\n"), 0o644); err != nil {
			return err
		}
		verified := "not verified (no checksum_url)"
		if src.src.ChecksumURL != "" {
			verified = "sha256 verified"
		}
		log.Printf("fetched %s: %d bytes, %s, attempts %d", dst, res.Size, verified, res.Attempts)
		fmt.Printf("%s  %s\n", res.SHA256, dst)
	}
	return nil
}

func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "configuration file")
	sitePath := fs.String("geosite", filepath.Join(".cache", "geosite.dat"), "path to geosite.dat")
	ipPath := fs.String("geoip", filepath.Join(".cache", "geoip.dat"), "path to geoip.dat")
	summary := fs.String("summary", "", "write a Markdown summary to this file (for $GITHUB_STEP_SUMMARY)")
	commitMsg := fs.String("commit-message", "", "write a commit message with statistics to this file")
	allowLarge := fs.Bool("allow-large-change", false, "accept list size changes above the safety threshold")
	dryRun := fs.Bool("dry-run", false, "compute and check everything but write no list files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	rep, err := build.Run(build.Options{
		Config: cfg, GeoSitePath: *sitePath, GeoIPPath: *ipPath,
		AllowLargeChange: *allowLarge, DryRun: *dryRun, Logf: log.Printf,
	})
	if rep != nil {
		if *summary != "" {
			if werr := os.WriteFile(*summary, []byte(rep.Markdown()), 0o644); werr != nil && err == nil {
				err = werr
			}
		}
		if *commitMsg != "" {
			if werr := os.WriteFile(*commitMsg, []byte(rep.CommitMessage()), 0o644); werr != nil && err == nil {
				err = werr
			}
		}
	}
	if err != nil {
		return err
	}
	for _, lr := range rep.Lists {
		for _, f := range lr.Files {
			state := "unchanged"
			switch {
			case f.IsNew():
				state = "new"
			case f.Added > 0 || f.Removed > 0:
				state = fmt.Sprintf("+%d/-%d", f.Added, f.Removed)
			}
			fmt.Printf("%-28s %7d lines  %s\n", f.Path, f.Lines, state)
		}
	}
	if !rep.Written {
		log.Printf("dry run: no files written")
	}
	return nil
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "configuration file (its lists are validated when no files are given)")
	kindName := fs.String("kind", "combined", "expected content of explicitly given files: combined, domains, ipv4 or ipv6")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: geo2list validate [-config file] [-kind kind] [file ...]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	type target struct {
		path string
		kind listfile.Kind
	}
	var targets []target
	if fs.NArg() > 0 {
		k, err := listfile.ParseKind(*kindName)
		if err != nil {
			return err
		}
		for _, p := range fs.Args() {
			targets = append(targets, target{p, k})
		}
	} else {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return err
		}
		for _, l := range cfg.Lists {
			kinds, err := l.Kinds()
			if err != nil {
				return err
			}
			for _, k := range kinds {
				targets = append(targets, target{filepath.Join(cfg.Output.Dir, listfile.FileName(l.Name, k, cfg.Output.Extension)), k})
			}
		}
	}
	var errs []error
	for _, t := range targets {
		if err := listfile.ValidateFile(t.path, t.kind); err != nil {
			errs = append(errs, err)
			log.Printf("FAIL %s: %v", t.path, err)
			continue
		}
		lines, _ := listfile.ReadLines(t.path)
		fmt.Printf("ok   %-28s %7d lines (%s)\n", t.path, len(lines), t.kind)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d of %d files are invalid", len(errs), len(targets))
	}
	return nil
}

func runInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	sitePath := fs.String("geosite", "", "path to geosite.dat")
	ipPath := fs.String("geoip", "", "path to geoip.dat")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: geo2list inspect [-geosite file] [-geoip file] [substring]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sitePath == "" && *ipPath == "" {
		return errors.New("inspect: give -geosite and/or -geoip")
	}
	filter := strings.ToLower(fs.Arg(0))
	if *sitePath != "" {
		entries, err := geodat.ReadGeoSiteList(*sitePath)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Code < entries[j].Code })
		fmt.Printf("geosite: %d categories\n", len(entries))
		for _, e := range entries {
			if !strings.Contains(strings.ToLower(e.Code), filter) {
				continue
			}
			var counts [4]int
			attrs := 0
			for _, d := range e.Domains {
				if d.Type >= 0 && int(d.Type) < len(counts) {
					counts[d.Type]++
				}
				attrs += len(d.Attributes)
			}
			fmt.Printf("  %-40s %7d  domain=%d full=%d keyword=%d regexp=%d attributes=%d\n",
				e.Code, len(e.Domains), counts[geodat.DomainRootDomain], counts[geodat.DomainFull], counts[geodat.DomainPlain], counts[geodat.DomainRegex], attrs)
		}
	}
	if *ipPath != "" {
		entries, err := geodat.ReadGeoIPList(*ipPath)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Code < entries[j].Code })
		fmt.Printf("geoip: %d categories\n", len(entries))
		for _, e := range entries {
			if !strings.Contains(strings.ToLower(e.Code), filter) {
				continue
			}
			v4, v6 := 0, 0
			for _, c := range e.CIDRs {
				if len(c.IP) == 4 {
					v4++
				} else {
					v6++
				}
			}
			inv := ""
			if e.InverseMatch {
				inv = "  inverse_match=true"
			}
			fmt.Printf("  %-40s %7d  ipv4=%d ipv6=%d%s\n", e.Code, len(e.CIDRs), v4, v6, inv)
		}
	}
	return nil
}
