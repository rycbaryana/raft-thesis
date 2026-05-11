package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	workers           = 10
	measureDuration   = 5 * time.Second
	warmupDuration    = 3 * time.Second
	roundPause        = 1 * time.Second
	roundsPerScenario = 3
	opTimeout         = 3 * time.Second
	startupTimeout    = 45 * time.Second
	shutdownTimeout   = 3 * time.Second
	scenarioCooldown  = 2 * time.Second
	baseNodeID        = 1
	benchKey          = "bench_key"
	benchValue        = "val"
	writeWorkloadName = "WriteHeavy"
	readWorkloadName  = "ReadHeavy"
	csvOutputPath     = "build/bench_results.csv"
	nodeLogsDir       = "build/bench_nodes"
	nodeLogLevel      = "error"
)

var clusterSizes = []int{3, 5, 7, 11, 13, 17}

var benchHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   256,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   2 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

type metrics struct {
	workload       string
	nodes          int
	attempts       int64
	success        int64
	attemptRPS     float64
	rps            float64
	successRatePct float64
	meanMs         float64
	p50Ms          float64
	p99Ms          float64
	transportErrs  int64
	notLeader503   int64
	timeout504     int64
	otherStatuses  int64
}

type clusterRuntime struct {
	nodeIDs  []int
	cmds     []*exec.Cmd
	logFiles []*os.File
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "benchmark failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := buildNodeBinary(); err != nil {
		return err
	}

	fmt.Printf("Workload,Nodes,AttemptRPS,SuccessRPS,Success(%%),Mean(ms),p50(ms),p99(ms)\n")
	csvRows := [][]string{{
		"kind",
		"workload",
		"nodes",
		"round",
		"attempt_rps",
		"success_rps",
		"success_rate_pct",
		"mean_ms",
		"p50_ms",
		"p99_ms",
		"transport_err",
		"not_leader_503",
		"timeout_504",
		"other_status",
	}}

	for _, size := range clusterSizes {
		writeAgg, writeRounds, err := runScenario(size, writeWorkloadName, false)
		if err != nil {
			return err
		}
		printMetrics(writeAgg)
		csvRows = append(csvRows, metricsToCSVRows(writeRounds, writeAgg)...)

		readAgg, readRounds, err := runScenario(size, readWorkloadName, true)
		if err != nil {
			return err
		}
		printMetrics(readAgg)
		csvRows = append(csvRows, metricsToCSVRows(readRounds, readAgg)...)
	}

	if err := writeCSV(csvOutputPath, csvRows); err != nil {
		return err
	}
	fmt.Printf("CSV,%s\n", csvOutputPath)
	fmt.Printf("NodeLogsDir,%s\n", nodeLogsDir)
	return nil
}

func runScenario(size int, workload string, readHeavy bool) (metrics, []metrics, error) {
	rt, err := startCluster(size, workload)
	if err != nil {
		return metrics{}, nil, fmt.Errorf("start cluster size=%d workload=%s: %w", size, workload, err)
	}
	time.Sleep(1 * time.Second)

	leaderID := int64(rt.nodeIDs[0])
	if readHeavy {
		if err := ensureReadSeed(rt.nodeIDs, &leaderID); err != nil {
			return metrics{}, nil, fmt.Errorf("seed read workload size=%d: %w", size, err)
		}
	}
	_ = runWorkload(size, workload, readHeavy, &leaderID, warmupDuration)

	rounds := make([]metrics, 0, roundsPerScenario)
	for i := 0; i < roundsPerScenario; i++ {
		m := runWorkload(size, workload, readHeavy, &leaderID, measureDuration)
		rounds = append(rounds, m)
		time.Sleep(roundPause)
	}
	if err := rt.stop(); err != nil {
		return metrics{}, nil, fmt.Errorf("stop cluster size=%d workload=%s: %w", size, workload, err)
	}
	time.Sleep(scenarioCooldown)
	return aggregateMetrics(workload, size, rounds), rounds, nil
}

func buildNodeBinary() error {
	if err := os.MkdirAll("build", 0o755); err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}
	cmd := exec.Command("go", "build", "-o", "build/node", "./cmd/node")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build node binary: %w", err)
	}
	return nil
}

func startCluster(size int, workload string) (*clusterRuntime, error) {
	nodeIDs := make([]int, 0, size)
	for i := 0; i < size; i++ {
		nodeIDs = append(nodeIDs, baseNodeID+i)
	}
	bootstrapCSV := idsToCSV(nodeIDs)

	rt := &clusterRuntime{
		nodeIDs:  nodeIDs,
		cmds:     make([]*exec.Cmd, 0, size),
		logFiles: make([]*os.File, 0, size),
	}

	for _, id := range nodeIDs {
		if err := killPortOwner(nodePort(id)); err != nil {
			return nil, fmt.Errorf("cleanup port for node %d: %w", id, err)
		}
	}

	for _, id := range nodeIDs {
		cmd := exec.Command(
			"./build/node",
			"-id", strconv.Itoa(id),
			"-bootstrap", bootstrapCSV,
			"-log-level", nodeLogLevel,
			"-storage", "memory",
		)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start node %d: %w", id, err)
		}
		rt.cmds = append(rt.cmds, cmd)
	}

	if err := waitClusterReady(rt.nodeIDs); err != nil {
		_ = rt.stop()
		return nil, err
	}
	return rt, nil
}

func (rt *clusterRuntime) stop() error {
	var firstErr error
	for _, cmd := range rt.cmds {
		if cmd.Process == nil {
			continue
		}
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	deadline := time.Now().Add(shutdownTimeout)
	for _, cmd := range rt.cmds {
		if cmd.Process == nil {
			continue
		}
		waitCh := make(chan error, 1)
		go func(c *exec.Cmd) {
			waitCh <- c.Wait()
		}(cmd)

		timeout := time.Until(deadline)
		if timeout <= 0 {
			timeout = 1 * time.Millisecond
		}
		select {
		case err := <-waitCh:
			if err != nil && firstErr == nil {
				firstErr = err
			}
		case <-time.After(timeout):
			_ = cmd.Process.Kill()
			err := <-waitCh
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	for _, id := range rt.nodeIDs {
		if err := killPortOwner(nodePort(id)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, f := range rt.logFiles {
		if f == nil {
			continue
		}
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func waitClusterReady(nodeIDs []int) error {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	leaderID := nodeIDs[0]
	for {
		time.Sleep(1 * time.Second)
		select {
		case <-ctx.Done():
			return fmt.Errorf("cluster readiness timeout: %w", ctx.Err())
		default:
		}

		opCtx, opCancel := context.WithTimeout(ctx, opTimeout)
		ok, nextLeader, _, err := putWithLeaderFollow(opCtx, leaderID, benchKey, benchValue)
		opCancel()
		if err == nil && ok {
			return nil
		}
		if nextLeader != 0 {
			leaderID = nextLeader
		}
	}
}

func runWorkload(size int, workload string, readHeavy bool, leaderID *int64, duration time.Duration) metrics {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var successCount atomic.Int64
	var attempts atomic.Int64
	var transportErrs atomic.Int64
	var notLeader503 atomic.Int64
	var timeout504 atomic.Int64
	var otherStatus atomic.Int64
	latencies := make([]time.Duration, 0, 100000)
	var latMu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()

			localLat := make([]time.Duration, 0, 2048)
			for {
				select {
				case <-ctx.Done():
					latMu.Lock()
					latencies = append(latencies, localLat...)
					latMu.Unlock()
					return
				default:
				}

				currentLeader := int(atomic.LoadInt64(leaderID))
				start := time.Now()
				opCtx, opCancel := context.WithTimeout(ctx, opTimeout)
				var (
					ok      bool
					next    int
					status  int
					err     error
					latency time.Duration
				)
				if readHeavy {
					ok, next, status, err = getWithLeaderFollow(opCtx, currentLeader, benchKey)
				} else {
					ok, next, status, err = putWithLeaderFollow(opCtx, currentLeader, benchKey, benchValue)
				}
				opCancel()
				attempts.Add(1)

				latency = time.Since(start)
				if next != 0 {
					atomic.StoreInt64(leaderID, int64(next))
				}
				if err != nil {
					transportErrs.Add(1)
					continue
				}
				if !ok {
					switch status {
					case http.StatusServiceUnavailable:
						notLeader503.Add(1)
					case http.StatusGatewayTimeout:
						timeout504.Add(1)
					default:
						otherStatus.Add(1)
					}
					continue
				}
				successCount.Add(1)
				localLat = append(localLat, latency)
			}
		}()
	}

	wg.Wait()
	return computeMetrics(workload, size, duration, attempts.Load(), successCount.Load(), latencies, transportErrs.Load(), notLeader503.Load(), timeout504.Load(), otherStatus.Load())
}

func ensureReadSeed(nodeIDs []int, leaderID *int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("seed read-heavy key: %w", ctx.Err())
		default:
		}
		currentLeader := int(atomic.LoadInt64(leaderID))
		opCtx, opCancel := context.WithTimeout(ctx, opTimeout)
		ok, nextLeader, _, err := putWithLeaderFollow(opCtx, currentLeader, benchKey, benchValue)
		opCancel()
		if nextLeader != 0 {
			atomic.StoreInt64(leaderID, int64(nextLeader))
		}
		if err == nil && ok {
			return nil
		}
		if currentLeader == 0 && len(nodeIDs) > 0 {
			atomic.StoreInt64(leaderID, int64(nodeIDs[0]))
		}
	}
}

func computeMetrics(
	workload string,
	nodes int,
	duration time.Duration,
	attempts int64,
	success int64,
	latencies []time.Duration,
	transportErrs int64,
	notLeader503 int64,
	timeout504 int64,
	otherStatus int64,
) metrics {
	result := metrics{
		workload:      workload,
		nodes:         nodes,
		attempts:      attempts,
		success:       success,
		transportErrs: transportErrs,
		notLeader503:  notLeader503,
		timeout504:    timeout504,
		otherStatuses: otherStatus,
		attemptRPS:    float64(attempts) / duration.Seconds(),
		rps:           float64(success) / duration.Seconds(),
	}
	if attempts > 0 {
		result.successRatePct = 100 * float64(success) / float64(attempts)
	}
	if success == 0 || len(latencies) == 0 {
		return result
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	var sum time.Duration
	for _, v := range latencies {
		sum += v
	}

	result.meanMs = durationMs(sum / time.Duration(success))
	result.p50Ms = durationMs(latencies[len(latencies)*50/100])
	result.p99Ms = durationMs(latencies[len(latencies)*99/100])
	return result
}

func printMetrics(m metrics) {
	fmt.Printf("%s,%d,%.2f,%.2f,%.2f,%.3f,%.3f,%.3f\n",
		m.workload,
		m.nodes,
		m.attemptRPS,
		m.rps,
		m.successRatePct,
		m.meanMs,
		m.p50Ms,
		m.p99Ms,
	)
}

func putWithLeaderFollow(ctx context.Context, nodeID int, key, value string) (bool, int, int, error) {
	params := url.Values{}
	params.Set("key", key)
	params.Set("val", value)
	return doWithLeaderFollow(ctx, nodeID, "/put", params)
}

func getWithLeaderFollow(ctx context.Context, nodeID int, key string) (bool, int, int, error) {
	params := url.Values{}
	params.Set("key", key)
	return doWithLeaderFollow(ctx, nodeID, "/get", params)
}

func doWithLeaderFollow(ctx context.Context, nodeID int, path string, params url.Values) (bool, int, int, error) {
	status, leaderHint, err := doHTTP(ctx, nodeID, path, params)
	if err != nil {
		return false, leaderHint, 0, err
	}
	if status == http.StatusOK {
		return true, nodeID, status, nil
	}
	if status == http.StatusServiceUnavailable && leaderHint != 0 && leaderHint != nodeID {
		status, _, err = doHTTP(ctx, leaderHint, path, params)
		if err != nil {
			return false, leaderHint, 0, err
		}
		if status == http.StatusOK {
			return true, leaderHint, status, nil
		}
	}
	return false, leaderHint, status, nil
}

func doHTTP(ctx context.Context, nodeID int, path string, params url.Values) (status int, leaderHint int, err error) {
	u := fmt.Sprintf("http://127.0.0.1:%d%s?%s", nodePort(nodeID), path, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := benchHTTPClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	leaderHint = parseLeaderHint(resp.Header.Get("X-Raft-Leader-Id"))
	return resp.StatusCode, leaderHint, nil
}

func parseLeaderHint(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func aggregateMetrics(workload string, nodes int, rounds []metrics) metrics {
	if len(rounds) == 0 {
		return metrics{workload: workload, nodes: nodes}
	}

	var totalAttempts int64
	var totalSuccess int64
	var totalTransportErrs int64
	var totalNotLeader int64
	var totalTimeout int64
	var totalOther int64
	attemptRPSValues := make([]float64, 0, len(rounds))
	successRPSValues := make([]float64, 0, len(rounds))
	meanValues := make([]float64, 0, len(rounds))
	p50Values := make([]float64, 0, len(rounds))
	p99Values := make([]float64, 0, len(rounds))

	for _, r := range rounds {
		totalAttempts += r.attempts
		totalSuccess += r.success
		totalTransportErrs += r.transportErrs
		totalNotLeader += r.notLeader503
		totalTimeout += r.timeout504
		totalOther += r.otherStatuses
		attemptRPSValues = append(attemptRPSValues, r.attemptRPS)
		successRPSValues = append(successRPSValues, r.rps)
		meanValues = append(meanValues, r.meanMs)
		p50Values = append(p50Values, r.p50Ms)
		p99Values = append(p99Values, r.p99Ms)
	}

	agg := metrics{
		workload:      workload,
		nodes:         nodes,
		attempts:      totalAttempts,
		success:       totalSuccess,
		transportErrs: totalTransportErrs,
		notLeader503:  totalNotLeader,
		timeout504:    totalTimeout,
		otherStatuses: totalOther,
		attemptRPS:    medianFloat64(attemptRPSValues),
		rps:           medianFloat64(successRPSValues),
		meanMs:        medianFloat64(meanValues),
		p50Ms:         medianFloat64(p50Values),
		p99Ms:         medianFloat64(p99Values),
	}
	if totalAttempts > 0 {
		agg.successRatePct = 100 * float64(totalSuccess) / float64(totalAttempts)
	}
	return agg
}

func medianFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func metricsToCSVRows(rounds []metrics, agg metrics) [][]string {
	rows := make([][]string, 0, len(rounds)+1)
	for i, r := range rounds {
		rows = append(rows, []string{
			"round",
			r.workload,
			strconv.Itoa(r.nodes),
			strconv.Itoa(i + 1),
			fmt.Sprintf("%.4f", r.attemptRPS),
			fmt.Sprintf("%.4f", r.rps),
			fmt.Sprintf("%.4f", r.successRatePct),
			fmt.Sprintf("%.4f", r.meanMs),
			fmt.Sprintf("%.4f", r.p50Ms),
			fmt.Sprintf("%.4f", r.p99Ms),
			strconv.FormatInt(r.transportErrs, 10),
			strconv.FormatInt(r.notLeader503, 10),
			strconv.FormatInt(r.timeout504, 10),
			strconv.FormatInt(r.otherStatuses, 10),
		})
	}
	rows = append(rows, []string{
		"aggregate",
		agg.workload,
		strconv.Itoa(agg.nodes),
		"0",
		fmt.Sprintf("%.4f", agg.attemptRPS),
		fmt.Sprintf("%.4f", agg.rps),
		fmt.Sprintf("%.4f", agg.successRatePct),
		fmt.Sprintf("%.4f", agg.meanMs),
		fmt.Sprintf("%.4f", agg.p50Ms),
		fmt.Sprintf("%.4f", agg.p99Ms),
		strconv.FormatInt(agg.transportErrs, 10),
		strconv.FormatInt(agg.notLeader503, 10),
		strconv.FormatInt(agg.timeout504, 10),
		strconv.FormatInt(agg.otherStatuses, 10),
	})
	return rows
}

func writeCSV(path string, rows [][]string) error {
	if err := os.MkdirAll("build", 0o755); err != nil {
		return fmt.Errorf("create build dir for csv: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create csv %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return fmt.Errorf("write csv %s: %w", path, err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush csv %s: %w", path, err)
	}
	return nil
}

func killPortOwner(port int) error {
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port), "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil
		}
		return err
	}
	pids := strings.Fields(string(out))
	for _, pidStr := range pids {
		pid, convErr := strconv.Atoi(pidStr)
		if convErr != nil {
			continue
		}
		proc, findErr := os.FindProcess(pid)
		if findErr != nil {
			continue
		}
		_ = proc.Signal(syscall.SIGTERM)
	}
	return nil
}

func nodePort(id int) int {
	return 8080 + id
}

func idsToCSV(ids []int) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}

func durationMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
