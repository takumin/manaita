package config

type Config struct {
	LogLevel  string
	LogFormat string
	Chdir     string

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
