package config


type HomeConfig struct {
	Enabled                 bool          `yaml:"enabled" json:"enabled"`
	NodeID                  string        `yaml:"-" json:"-"`
	Host                    string        `yaml:"host" json:"-"`
	Port                    int           `yaml:"port" json:"-"`
	DisableClusterDiscovery bool          `yaml:"disable-cluster-discovery" json:"-"`
	TLS                     HomeTLSConfig `yaml:"tls" json:"-"`
}


type HomeTLSConfig struct {
	Enable              bool   `yaml:"enable" json:"-"`
	ServerName          string `yaml:"server-name" json:"-"`
	InsecureSkipVerify  bool   `yaml:"insecure-skip-verify" json:"-"`
	CACert              string `yaml:"ca-cert" json:"-"`
	ClientCert          string `yaml:"-" json:"-"`
	ClientKey           string `yaml:"-" json:"-"`
	UseTargetServerName bool   `yaml:"-" json:"-"`
}



func NormalizeHomePort(port int) int {
	if port <= 0 {
		return 8317
	}
	return port
}
