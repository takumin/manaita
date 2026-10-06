package config

type Config struct {
	LogLevel  string
	LogFormat string
	Chdir     string

	DryRun          bool
	MitamaeLogLevel string
	Recipes         []string
	Local           bool
	Parallel        int
}

func NewConfig(opts ...Option) *Config {
	c := &Config{}
	for _, o := range opts {
		o.Apply(c)
	}
	return c
}
