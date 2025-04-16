package main

import (
    "fmt"
    "os"
    "strings"
    "sync"
    "time"

    "github.com/sirupsen/logrus"
    "gopkg.in/yaml.v3"
)

type Config struct {
    ListenPort            int           `yaml:"listen_port"`
    ScrapeInterval        time.Duration `yaml:"scrape_interval"`
    JstatTimeout          time.Duration `yaml:"jstat_timeout"`
    MaxMonitoredProcesses int           `yaml:"max_monitored_processes"`
    MaxConcurrentScrapes  int           `yaml:"max_concurrent_scrapes"`
    JstatPath             string        `yaml:"jstat_path"`
    LogLevel              string        `yaml:"log_level"`
    PidFilter             string        `yaml:"pid_filter"`
    AppNameLabels         []string      `yaml:"app_name_labels"`
}

var (
    config   Config
    configMu sync.RWMutex
)

// 创建默认配置文件
func createDefaultConfig(path string) {
    defaultConfig := `listen_port: 9101
scrape_interval: 30s
jstat_timeout: 5s
max_monitored_processes: 1000
max_concurrent_scrapes: 50
jstat_path: jstat
log_level: info
pid_filter: ""
app_name_labels:
  - "-Dapp.name"
  - "-Dapp"
  - "-Dspring.application.name"`

    if err := os.WriteFile(path, []byte(defaultConfig), 0644); err != nil {
        logrus.Fatalf("Failed to create default config: %v", err)
    }
    logrus.Infof("Created default config file at %s", path)
}

func loadConfig(path string) error {
    configMu.Lock()
    defer configMu.Unlock()

    file, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("open config: %w", err)
    }
    defer file.Close()

    var newConfig Config
    if err := yaml.NewDecoder(file).Decode(&newConfig); err != nil {
        return fmt.Errorf("parse config: %w", err)
    }

    if newConfig.AppNameLabels == nil {
        newConfig.AppNameLabels = []string{
            "-Dapp.name", 
            "-Dapp",
            "-Dspring.application.name",
        }
    }

    if err := validateConfig(&newConfig); err != nil {
        return err
    }

    compileAppPatterns(newConfig.AppNameLabels)
    setLogLevel(newConfig.LogLevel)
    config = newConfig
    return nil
}

func validateConfig(c *Config) error {
    for _, label := range c.AppNameLabels {
        if !strings.HasPrefix(label, "-D") || strings.Contains(label, " ") {
            return fmt.Errorf("invalid app_name_label: %q", label)
        }
    }
    return nil
}

func setLogLevel(level string) {
    lvl, err := logrus.ParseLevel(level)
    if err != nil {
        logrus.SetLevel(logrus.InfoLevel)
        logrus.Warnf("Invalid log level %q, using info", level)
        return
    }
    logrus.SetLevel(lvl)
}