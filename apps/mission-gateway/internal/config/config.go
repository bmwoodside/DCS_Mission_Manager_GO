package config

import "os"

type Config struct {
	Addr string

	PublicDir string

	AgentSecret string
}

func FromEnv() Config {
	return Config{
		Addr:        getEnv("ADDR", ":8000"),
		PublicDir:   getEnv("PUBLIC_DIR", "web"),
		AgentSecret: getEnv("AGENT_SECRET", ""),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}
