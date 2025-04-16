package main

import (
    "bytes"
    "context"
    "fmt"
    "os"
    "sort"
    "strconv"
    "strings"
    "sync"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/sirupsen/logrus"
)

type JvmGcCollector struct {
    metrics     map[string]*prometheus.GaugeVec
    counters    map[string]*prometheus.CounterVec
    lastMetrics sync.Map
    lastPids    []int
}

type gcMetrics struct {
    s0c, s1c, s0u, s1u float64
    ec, eu, oc, ou     float64
    mc, mu, ccsc, ccsu float64
    ygc, fgc           float64
    ygct, fgct, gct    float64
}

func NewJvmGcCollector() *JvmGcCollector {
    labels := []string{"pid", "app"} 
    return &JvmGcCollector{
        metrics: map[string]*prometheus.GaugeVec{
            "s0_capacity":  newGauge("s0_capacity_bytes", labels),
            "s1_capacity":  newGauge("s1_capacity_bytes", labels),
            "s0_usage":     newGauge("s0_usage_bytes", labels),
            "s1_usage":     newGauge("s1_usage_bytes", labels),
            "eden_capacity": newGauge("eden_capacity_bytes", labels),
            "eden_usage":    newGauge("eden_usage_bytes", labels),
            "old_capacity":  newGauge("old_gen_capacity_bytes", labels),
            "old_usage":     newGauge("old_gen_usage_bytes", labels),
            "meta_capacity": newGauge("metaspace_capacity_bytes", labels),
            "meta_usage":    newGauge("metaspace_usage_bytes", labels),
            "class_capacity": newGauge("compressed_class_capacity_bytes", labels),
            "class_usage":    newGauge("compressed_class_usage_bytes", labels),
        },
        counters: map[string]*prometheus.CounterVec{
            "young_gc_count": newCounter("young_gc_count", labels),
            "young_gc_time":  newCounter("young_gc_time_seconds", labels),
            "full_gc_count":  newCounter("full_gc_count", labels),
            "full_gc_time":   newCounter("full_gc_time_seconds", labels),
            "total_gc_time":  newCounter("total_gc_time_seconds", labels),
        },
    }
}

func newGauge(name string, labels []string) *prometheus.GaugeVec {
    return prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "jvm_gc_" + name,
        Help: "JVM GC " + name,
    }, labels)
}

func newCounter(name string, labels []string) *prometheus.CounterVec {
    return prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "jvm_gc_" + name,
        Help: "JVM GC " + name,
    }, labels)
}

func (c *JvmGcCollector) Describe(ch chan<- *prometheus.Desc) {
    for _, m := range c.metrics {
        m.Describe(ch)
    }
    for _, m := range c.counters {
        m.Describe(ch)
    }
}

func (c *JvmGcCollector) Collect(ch chan<- prometheus.Metric) {
    pids, err := getJavaPids()
    if err != nil {
        logrus.Errorf("Get PIDs failed: %v", err)
        return
    }

    alivePids := filterAlivePids(pids)
    sort.Ints(alivePids)

    c.cleanupStaleMetrics(alivePids)

    var wg sync.WaitGroup
    sem := make(chan struct{}, config.MaxConcurrentScrapes)

    for _, pid := range alivePids {
        if pid == currentPID {
            continue
        }

        wg.Add(1)
        go func(pid int) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()

            if !isProcessAlive(pid) {
                return
            }

            app := getAppName(pid)
            metrics, err := scrapeJstat(pid)
            if err != nil {
                logrus.Errorf("Scrape PID %d failed: %v", pid, err)
                return
            }

            c.updateMetrics(pid, app, metrics)
        }(pid)
    }
    wg.Wait()

    for _, m := range c.metrics {
        m.Collect(ch)
    }
    for _, m := range c.counters {
        m.Collect(ch)
    }
}

func (c *JvmGcCollector) cleanupStaleMetrics(current []int) {
    currentSet := make(map[int]bool)
    for _, pid := range current {
        currentSet[pid] = true
    }

    var toDelete []int
    c.lastMetrics.Range(func(key, _ interface{}) bool {
        pid := key.(int)
        if !currentSet[pid] {
            toDelete = append(toDelete, pid)
        }
        return true
    })

    for _, pid := range toDelete {
        c.removeMetrics(pid)
    }
}

func (c *JvmGcCollector) removeMetrics(pid int) {
    c.lastMetrics.Delete(pid)
    for _, m := range c.metrics {
        m.DeletePartialMatch(prometheus.Labels{"pid": strconv.Itoa(pid)})
    }
    for _, m := range c.counters {
        m.DeletePartialMatch(prometheus.Labels{"pid": strconv.Itoa(pid)})
    }
}

func (c *JvmGcCollector) updateMetrics(pid int, app string, metrics *gcMetrics) {
    labels := prometheus.Labels{
        "pid": strconv.Itoa(pid),
        "app": app,
    }

    c.metrics["s0_capacity"].With(labels).Set(metrics.s0c)
    c.metrics["s1_capacity"].With(labels).Set(metrics.s1c)
    c.metrics["s0_usage"].With(labels).Set(metrics.s0u)
    c.metrics["s1_usage"].With(labels).Set(metrics.s1u)
    c.metrics["eden_capacity"].With(labels).Set(metrics.ec)
    c.metrics["eden_usage"].With(labels).Set(metrics.eu)
    c.metrics["old_capacity"].With(labels).Set(metrics.oc)
    c.metrics["old_usage"].With(labels).Set(metrics.ou)
    c.metrics["meta_capacity"].With(labels).Set(metrics.mc)
    c.metrics["meta_usage"].With(labels).Set(metrics.mu)
    c.metrics["class_capacity"].With(labels).Set(metrics.ccsc)
    c.metrics["class_usage"].With(labels).Set(metrics.ccsu)

    last, exists := c.lastMetrics.Load(pid)
    if !exists {
        c.lastMetrics.Store(pid, metrics)
        return
    }

    lastMetrics := last.(*gcMetrics)
    delta := &gcMetrics{
        ygc:  metrics.ygc - lastMetrics.ygc,
        ygct: metrics.ygct - lastMetrics.ygct,
        fgc:  metrics.fgc - lastMetrics.fgc,
        fgct: metrics.fgct - lastMetrics.fgct,
        gct:  metrics.gct - lastMetrics.gct,
    }

    c.counters["young_gc_count"].With(labels).Add(delta.ygc)
    c.counters["young_gc_time"].With(labels).Add(delta.ygct)
    c.counters["full_gc_count"].With(labels).Add(delta.fgc)
    c.counters["full_gc_time"].With(labels).Add(delta.fgct)
    c.counters["total_gc_time"].With(labels).Add(delta.gct)

    c.lastMetrics.Store(pid, metrics)
}