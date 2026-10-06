package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Host is a machine of the inventory.
type Host struct {
	// Name is the base name of the inventory file.
	Name string `yaml:"-"`
	// SSH is the ssh destination of the host. It defaults to Name.
	SSH string `yaml:"ssh"`
	// Hostname is the short hostname used to look up the node files. It
	// defaults to Name.
	Hostname string `yaml:"hostname"`
	// Domain is the DNS domain used to look up the node files.
	Domain string `yaml:"domain"`
	// RunList are the recipes applied to the host, in order.
	RunList []string `yaml:"run_list"`
}

// Hosts returns every host of the inventory, sorted by name.
func (p *Project) Hosts() ([]*Host, error) {
	dir := filepath.Join(p.Root, p.HostsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read the inventory: %w", err)
	}
	hosts := []*Host{}
	for _, e := range entries {
		name, ok := hostName(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		h, err := p.loadHost(name, filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
	return hosts, nil
}

// Host returns the host of the inventory named name.
func (p *Project) Host(name string) (*Host, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return nil, fmt.Errorf("invalid host name: %q", name)
	}
	for _, ext := range []string{".yml", ".yaml"} {
		path := filepath.Join(p.Root, p.HostsDir, name+ext)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		return p.loadHost(name, path)
	}
	return nil, fmt.Errorf("host %s not found in %s", name, p.HostsDir)
}

func hostName(file string) (string, bool) {
	for _, ext := range []string{".yml", ".yaml"} {
		if name, ok := strings.CutSuffix(file, ext); ok && name != "" && !strings.HasPrefix(name, ".") {
			return name, true
		}
	}
	return "", false
}

func (p *Project) loadHost(name, path string) (*Host, error) {
	h := &Host{}
	if err := decodeFile(path, h); err != nil {
		return nil, err
	}
	h.Name = name
	if h.SSH == "" {
		h.SSH = name
	}
	if h.Hostname == "" {
		h.Hostname = name
	}
	return h, nil
}
