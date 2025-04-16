package main

import (
    "fmt"
    "os/exec"
    "regexp"
    "strconv"
    "strings"
    "syscall"
)

var appPatterns []*regexp.Regexp

func compileAppPatterns(labels []string) {
    appPatterns = make([]*regexp.Regexp, 0, len(labels))
    for _, label := range labels {
        pattern := regexp.MustCompile(
            fmt.Sprintf(`%s=(["']?)([^"'\s]+)`, 
            regexp.QuoteMeta(label)),
        )
        appPatterns = append(appPatterns, pattern)
    }
}

func getJavaPids() ([]int, error) {
    configMu.RLock()
    defer configMu.RUnlock()

    cmd := exec.Command("pgrep", "-f", "java.*"+config.PidFilter)
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("pgrep failed: %w", err)
    }

    var pids []int
    for _, line := range strings.Split(string(output), "\n") {
        if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
            pids = append(pids, pid)
        }
    }
    return pids, nil
}

func getAppName(pid int) string {
    cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=")
    output, err := cmd.Output()
    if err != nil {
        return fmt.Sprintf("pid-%d", pid)
    }

    args := string(output)
    for _, pattern := range appPatterns {
        if matches := pattern.FindStringSubmatch(args); len(matches) > 2 {
            return strings.Trim(matches[2], `"'`)
        }
    }
    return fmt.Sprintf("pid-%d", pid)
}

func isProcessAlive(pid int) bool {
    return syscall.Kill(pid, 0) == nil
}

func filterAlivePids(pids []int) []int {
    var alive []int
    for _, pid := range pids {
        if isProcessAlive(pid) {
            alive = append(alive, pid)
        }
    }
    return alive
}