package config

type Option interface {
	Apply(*Config)
}

type LogLevel string

func (o LogLevel) Apply(c *Config) {
	c.LogLevel = string(o)
}

type LogFormat string

func (o LogFormat) Apply(c *Config) {
	c.LogFormat = string(o)
}

type Chdir string

func (o Chdir) Apply(c *Config) {
	c.Chdir = string(o)
}

type Parallel int

func (o Parallel) Apply(c *Config) {
	c.Parallel = int(o)
}

type ConfigFile string

func (o ConfigFile) Apply(c *Config) {
	c.ConfigFile = string(o)
}
