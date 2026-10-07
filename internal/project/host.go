package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Host is a machine of the inventory.
//
// The inventory is layered like the node files, from the least to the most
// specific:
//
//	all/*.yml
//	domains/{domain}/*.yml
//	hosts/{hostname}/*.yml
//	fqdns/{domain}/{hostname}/*.yml
//
// The layers are directories of the hosts directory, or the directories it
// names with {layer} replaced by their path, like nodes/{layer}/recipes. The
// files of a layer are merged in the order of their names. A host is declared
// by a layer of hosts or fqdns holding at least one file, so a hostname may
// have a different inventory in each domain.
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

// Hosts returns every host of the inventory, sorted by name.
func (p *Project) Hosts() ([]*Host, error) {
	if !strings.Contains(p.HostsDir, LayerPlaceholder) {
		if files, err := layerFiles(filepath.Join(p.Root, p.HostsDir)); err != nil {
			return nil, err
		} else if len(files) > 0 {
			// Catch the host files of the flat inventory, which are not layers.
			return nil, fmt.Errorf("unexpected inventory file %s: move it to a layer directory", filepath.Join(p.HostsDir, files[0]))
		}
	}
	prefix, suffix := p.layerAffixes()
	hosts := []*Host{}
	for _, pattern := range []string{"hosts/*", "fqdns/*/*"} {
		matches, err := filepath.Glob(filepath.Join(p.Root, p.layerDir(pattern)))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			// The layer path sits between the parts of the hosts directory.
			layer := strings.TrimSuffix(strings.TrimPrefix(m, prefix), suffix)
			parts := strings.Split(filepath.ToSlash(layer), "/")
			name, domain := parts[len(parts)-1], ""
			if pattern != "hosts/*" {
				domain = parts[len(parts)-2]
			}
			if hidden(name) || hidden(domain) {
				continue
			}
			files, err := layerFiles(m)
			if err != nil {
				return nil, err
			}
			if len(files) == 0 {
				continue
			}
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
// domain, merged from the files of every layer.
func (p *Project) Host(name, domain string) (*Host, error) {
	if !validName(name) {
		return nil, fmt.Errorf("invalid hostname: %q", name)
	}
	if domain != "" && !validName(domain) {
		return nil, fmt.Errorf("invalid domain: %q", domain)
	}
	// own marks the layers declaring the host itself.
	type layer struct {
		path string
		own  bool
	}
	layers := []layer{{path: "all"}}
	if domain != "" {
		layers = append(layers, layer{path: filepath.Join("domains", domain)})
	}
	layers = append(layers, layer{path: filepath.Join("hosts", name), own: true})
	if domain != "" {
		layers = append(layers, layer{path: filepath.Join("fqdns", domain, name), own: true})
	}

	h := &Host{Name: name, Domain: domain, RunList: []string{}, Files: []string{}}
	declared := false
	for _, l := range layers {
		rel := p.layerDir(l.path)
		files, err := layerFiles(filepath.Join(p.Root, rel))
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			f := &hostFile{}
			if err := decodeFile(filepath.Join(p.Root, rel, file), f); err != nil {
				return nil, err
			}
			for _, r := range f.RunList {
				if !slices.Contains(h.RunList, r) {
					h.RunList = append(h.RunList, r)
				}
			}
			h.Files = append(h.Files, filepath.ToSlash(filepath.Join(rel, file)))
			declared = declared || l.own
		}
	}
	if !declared {
		return nil, fmt.Errorf("host %s not found in %s", h.FQDN(), p.HostsDir)
	}
	return h, nil
}

// layerDir returns the directory of the layer path, relative to the project
// root.
func (p *Project) layerDir(path string) string {
	if strings.Contains(p.HostsDir, LayerPlaceholder) {
		return filepath.Clean(strings.Replace(p.HostsDir, LayerPlaceholder, path, 1))
	}
	return filepath.Join(p.HostsDir, path)
}

// layerAffixes returns the absolute directory before the path of a layer and
// the part of the hosts directory after it.
func (p *Project) layerAffixes() (string, string) {
	before, after, found := strings.Cut(filepath.FromSlash(p.HostsDir), LayerPlaceholder)
	if !found {
		before, after = p.HostsDir, ""
	}
	prefix := filepath.Join(p.Root, before) + string(filepath.Separator)
	if after = filepath.Clean(after); after == "." || after == string(filepath.Separator) {
		after = ""
	}
	return prefix, after
}

// layerFiles returns the names of the YAML files of the layer directory dir,
// sorted. A missing directory or a file is an empty layer.
func layerFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read the inventory: %w", err)
	}
	files := []string{}
	for _, e := range entries {
		name := e.Name()
		if ext := filepath.Ext(name); hidden(name) || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() {
			files = append(files, name)
		}
	}
	return files, nil
}

func validName(name string) bool {
	return name != "" && !hidden(name) && !strings.ContainsAny(name, `/\*?[`)
}

func hidden(name string) bool {
	return strings.HasPrefix(name, ".")
}
