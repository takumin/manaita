package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Host is a machine of the inventory.
//
// The inventory files are matched by the patterns of the manifest, from the
// least to the most specific, like the node files:
//
//	hosts/all/*.yml
//	hosts/domains/{domain}/*.yml
//	hosts/hosts/{hostname}/*.yml
//	hosts/fqdns/{domain}/{hostname}/*.yml
//
// {hostname} and {domain} are replaced by the values of the host, and a
// pattern is skipped when one of them is empty. The files are merged in the
// order of the patterns and, within a pattern, of their names. A host is
// declared by a pattern holding {hostname} that matches at least one file, so
// a hostname may have a different inventory in each domain.
type Host struct {
	// Name is the short hostname.
	Name string
	// Domain is the DNS domain of the host, empty when it has none.
	Domain string
	// RunList are the recipes applied to the host, in order.
	RunList []string
	// Files are the inventory files merged into the host, relative to the
	// project root.
	Files []string
}

// hostFile is a file of the inventory.
type hostFile struct {
	// RunList is appended to the run list of the less specific files.
	RunList []string `yaml:"run_list"`
}

// hostPlaceholder marks the inventory patterns declaring the hosts.
const hostPlaceholder = "{hostname}"

// Hosts returns every host of the inventory, sorted by name.
func (p *Project) Hosts() ([]*Host, error) {
	hosts := []*Host{}
	found := map[string]bool{}
	for _, pattern := range p.Inventory {
		if !strings.Contains(pattern, hostPlaceholder) {
			continue
		}
		glob, re, err := inventoryPattern(pattern)
		if err != nil {
			return nil, err
		}
		matches, err := filepath.Glob(filepath.Join(p.Root, glob))
		if err != nil {
			return nil, fmt.Errorf("invalid host pattern %q: %w", pattern, err)
		}
		for _, m := range matches {
			rel, err := filepath.Rel(p.Root, m)
			if err != nil {
				return nil, err
			}
			vars, ok := capture(re, filepath.ToSlash(rel))
			name, domain := vars["hostname"], vars["domain"]
			if !ok || hidden(name) || hidden(domain) || found[name+"."+domain] {
				continue
			}
			if info, err := os.Stat(m); err != nil || !info.Mode().IsRegular() {
				continue
			}
			found[name+"."+domain] = true
			h, err := p.Host(name, domain)
			if err != nil {
				return nil, err
			}
			hosts = append(hosts, h)
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].FQDN() < hosts[j].FQDN() })
	return hosts, nil
}

// FQDN returns the name of h with its domain, if any.
func (h *Host) FQDN() string {
	if h.Domain == "" {
		return h.Name
	}
	return h.Name + "." + h.Domain
}

// HostByFQDN returns the host named fqdn, whose first label is the hostname
// and the rest the domain.
func (p *Project) HostByFQDN(fqdn string) (*Host, error) {
	name, domain, _ := strings.Cut(fqdn, ".")
	return p.Host(name, domain)
}

// Host returns the host name of domain, which is empty for a host without a
// domain, merged from the files of every inventory pattern.
func (p *Project) Host(name, domain string) (*Host, error) {
	if !validName(name) {
		return nil, fmt.Errorf("invalid hostname: %q", name)
	}
	if domain != "" && !validName(domain) {
		return nil, fmt.Errorf("invalid domain: %q", domain)
	}
	h := &Host{Name: name, Domain: domain, RunList: []string{}, Files: []string{}}
	declared := false
	seen := map[string]bool{}
	for _, pattern := range p.Inventory {
		files, err := p.match("host", pattern, h, seen)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			f := &hostFile{}
			if err := decodeFile(filepath.Join(p.Root, file), f); err != nil {
				return nil, err
			}
			for _, r := range f.RunList {
				if !slices.Contains(h.RunList, r) {
					h.RunList = append(h.RunList, r)
				}
			}
			h.Files = append(h.Files, file)
			declared = declared || strings.Contains(pattern, hostPlaceholder)
		}
	}
	if !declared {
		return nil, fmt.Errorf("host %s not found in the inventory", h.FQDN())
	}
	return h, nil
}

// inventoryPattern returns the glob matching the files of the inventory
// pattern for any host, and the regexp capturing the placeholders from the
// paths it matches, relative to the project root.
func inventoryPattern(pattern string) (string, *regexp.Regexp, error) {
	clean := filepath.ToSlash(filepath.Clean(pattern))
	var glob, expr strings.Builder
	expr.WriteString("^")
	for i := 0; i < len(clean); i++ {
		if loc := placeholder.FindStringSubmatchIndex(clean[i:]); loc != nil && loc[0] == 0 {
			switch name := clean[i+loc[2] : i+loc[3]]; name {
			case "hostname":
				// The first label of the FQDN is the hostname.
				expr.WriteString(`(?P<hostname>[^/.]+)`)
			case "domain":
				expr.WriteString(`(?P<domain>[^/]+)`)
			default:
				return "", nil, fmt.Errorf("unknown placeholder {%s} in host pattern %q", name, pattern)
			}
			glob.WriteString("*")
			i += loc[1] - 1
			continue
		}
		// The paths already match the glob, so its wildcards only skip the
		// characters between the placeholders.
		switch c := clean[i]; c {
		case '*':
			expr.WriteString(`[^/]*`)
		case '?':
			expr.WriteString(`[^/]`)
		case '[':
			j := i + 1
			for ; j < len(clean) && clean[j] != ']'; j++ {
				if clean[j] == '\\' {
					j++
				}
			}
			glob.WriteString(clean[i:min(j+1, len(clean))])
			expr.WriteString(`[^/]`)
			i = j
			continue
		case '\\':
			if i+1 < len(clean) {
				glob.WriteString(clean[i : i+2])
				i++
				expr.WriteString(regexp.QuoteMeta(clean[i : i+1]))
				continue
			}
		default:
			expr.WriteString(regexp.QuoteMeta(string(c)))
		}
		glob.WriteByte(clean[i])
	}
	expr.WriteString("$")
	re, err := regexp.Compile(expr.String())
	if err != nil {
		return "", nil, fmt.Errorf("invalid host pattern %q: %w", pattern, err)
	}
	return glob.String(), re, nil
}

// capture returns the placeholders captured by re from path. It reports
// false when path does not match, or when a placeholder repeated in the
// pattern captures different values.
func capture(re *regexp.Regexp, path string) (map[string]string, bool) {
	m := re.FindStringSubmatch(path)
	if m == nil {
		return nil, false
	}
	vars := map[string]string{}
	for i, name := range re.SubexpNames() {
		if name == "" {
			continue
		}
		if v, ok := vars[name]; ok && v != m[i] {
			return nil, false
		}
		vars[name] = m[i]
	}
	return vars, true
}

func validName(name string) bool {
	return name != "" && !hidden(name) && !strings.ContainsAny(name, `/\*?[`)
}

func hidden(name string) bool {
	return strings.HasPrefix(name, ".")
}
