package main

import (
    "bytes"
    "context"
    "fmt"
    "os/exec"
    "strconv"
    "strings"
    "sync"
)

var (
    bufferPool = sync.Pool{New: func() interface{} { return bytes.NewBuffer(nil) }}
    currentPID = os.Getpid()
)

func scrapeJstat(pid int) (*gcMetrics, error) {
    ctx, cancel := context.WithTimeout(context.Background(), config.JstatTimeout)
    defer cancel()

    jstatPath := config.JstatPath
    if jstatPath == "" {
        jstatPath = "jstat"
    }

    cmd := exec.CommandContext(ctx, jstatPath, "-gc", strconv.Itoa(pid))
    buf := bufferPool.Get().(*bytes.Buffer)
    buf.Reset()
    defer bufferPool.Put(buf)

    cmd.Stdout = buf
    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("jstat failed: %w", err)
    }

    lines := strings.Split(buf.String(), "\n")
    if len(lines) < 2 {
        return nil, fmt.Errorf("invalid jstat output")
    }

    headers := strings.Fields(lines[0])
    values := strings.Fields(lines[1])
    if len(headers) != len(values) {
        return nil, fmt.Errorf("header/value mismatch")
    }

    data := make(map[string]float64)
    for i, h := range headers {
        val, _ := strconv.ParseFloat(values[i], 64)
        data[h] = val
    }

    return &gcMetrics{
        s0c:   data["S0C"] * 1024,
        s1c:   data["S1C"] * 1024,
        s0u:   data["S0U"] * 1024,
        s1u:   data["S1U"] * 1024,
        ec:    data["EC"] * 1024,
        eu:    data["EU"] * 1024,
        oc:    data["OC"] * 1024,
        ou:    data["OU"] * 1024,
        mc:    data["MC"] * 1024,
        mu:    data["MU"] * 1024,
        ccsc:  data["CCSC"] * 1024,
        ccsu:  data["CCSU"] * 1024,
        ygc:   data["YGC"],
        fgc:   data["FGC"],
        ygct:  data["YGCT"],
        fgct:  data["FGCT"],
        gct:   data["GCT"],
    }, nil
}