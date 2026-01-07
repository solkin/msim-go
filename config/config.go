package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port               int
	DBPath             string
	ReadTimeout        int // seconds
	WriteTimeout       int // seconds
	FilePortRangeStart int
	FilePortRangeEnd   int
}

func Load() *Config {
	cfg := &Config{
		Port:               3215,
		DBPath:             "msim.db",
		ReadTimeout:        120,
		WriteTimeout:       30,
		FilePortRangeStart: 35000,
		FilePortRangeEnd:   35999,
	}

	if portStr := os.Getenv("MSIM_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.Port = port
		}
	}

	if dbPath := os.Getenv("MSIM_DB_PATH"); dbPath != "" {
		cfg.DBPath = dbPath
	}

	if timeoutStr := os.Getenv("MSIM_READ_TIMEOUT"); timeoutStr != "" {
		if timeout, err := strconv.Atoi(timeoutStr); err == nil {
			cfg.ReadTimeout = timeout
		}
	}

	if timeoutStr := os.Getenv("MSIM_WRITE_TIMEOUT"); timeoutStr != "" {
		if timeout, err := strconv.Atoi(timeoutStr); err == nil {
			cfg.WriteTimeout = timeout
		}
	}

	if portStr := os.Getenv("MSIM_FILE_PORT_START"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.FilePortRangeStart = port
		}
	}

	if portStr := os.Getenv("MSIM_FILE_PORT_END"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.FilePortRangeEnd = port
		}
	}

	return cfg
}
