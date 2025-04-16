package main

import (
    "flag"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/sirupsen/logrus"
)

var configPath string

func init() {
    flag.StringVar(&configPath, "config", "config.yml", "Path to config file")
    flag.Parse()

    // 检查并创建默认配置文件
    if _, err := os.Stat(configPath); os.IsNotExist(err) {
        createDefaultConfig(configPath)
    }

    if err := loadConfig(configPath); err != nil {
        logrus.Fatalf("Config load failed: %v", err)
    }

    go watchConfigReload()
}

func main() {
    collector := NewJvmGcCollector()
    prometheus.MustRegister(collector)

    http.Handle("/metrics", promhttp.Handler())
    http.HandleFunc("/health", healthHandler)
    logrus.Infof("Starting server on :%d", config.ListenPort)
    logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.ListenPort), nil))
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}

func watchConfigReload() {
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGHUP)
    for range sigs {
        if err := loadConfig(configPath); err != nil {
            logrus.Errorf("Config reload failed: %v", err)
        } else {
            logrus.Info("Config reloaded successfully")
        }
    }
}