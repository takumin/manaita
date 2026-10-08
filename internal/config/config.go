package config

type Config struct {
	LogLevel  string
	LogFormat string
	Chdir     string

	// ConfigFile is the path of the configuration file of this machine,
	// read by File. Empty reads no file.
	ConfigFile string
	// File holds the values of ConfigFile, the last source of the flags.
	File *File

	CacheDir string
	Proxy    string

	MitamaeLogLevel string
	Recipes         []string
	Parallel        int
}

func NewConfig(opts ...Option) *Config {
	c := &Config{}
	for _, o := range opts {
		o.Apply(c)
	}
	return c
}
