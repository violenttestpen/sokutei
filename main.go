package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"sort"
	"time"

	"github.com/fatih/color"
)

// https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getprocesstimes?redirectedfrom=MSDN

const (
	progressDoneRune    = "█"
	progressPendingRune = "▒"
)

var (
	noShell bool
	runs    int64
	shell   string
	warmup  int64

	setupCmd string

	noColor bool
)

type benchmarkResult struct {
	cmd string

	mean  int64
	stdev float64
	min   int64
	max   int64

	denominator float64
	unit        string

	meanUserTime    int64
	userDenominator float64
	userUnit        string

	meanKernelTime    int64
	kernelDenominator float64
	kernelUnit        string
}

func init() {
	switch runtime.GOOS {
	case "windows":
		shell = "cmd.exe"
	default:
		shell = "/bin/sh"
	}
}

func runSetup(ctx context.Context, cmdToSetup string) error {
	cmdParts := list2Cmdline(cmdToSetup)
	if len(cmdParts) == 0 {
		return errors.New("empty command string")
	}
	return exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...).Run()
}

func runBenchmark(ctx context.Context, cmdToBenchmark string) (*benchmarkResult, error) {
	cmdParts := list2Cmdline(cmdToBenchmark)
	if len(cmdParts) == 0 {
		return nil, errors.New("empty command string")
	}

	fmt.Print("Performing warmup runs")
	for i := int64(0); i < warmup; i++ {
		cmd := exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
		if err := cmd.Run(); err != nil {
			return nil, err
		}
	}
	clearCurrentTerminalLine(color.Output)

	var currentEstimate int64
	var currentDenominator float64
	var currentUnitEstimate string
	var totalUserTime int64
	var totalKernelTime int64
	elapsedRuns := make([]int64, runs)

	fmt.Print("Initial time measurement")
	for i := int64(0); i < runs; i++ {
		processTimer.Reset()
		if err := processTimer.Run(ctx, cmdParts[0], cmdParts[1:]...); err != nil {
			return nil, err
		}
		totalUserTime += processTimer.GetUserTime()
		totalKernelTime += processTimer.GetKernelTime()
		elapsedRuns[i] = processTimer.GetRealTime()

		// Calculate current estimate and ETA
		currentEstimate = (currentEstimate*i + elapsedRuns[i]) / (i + 1)
		currentDenominator, currentUnitEstimate = getMeasurementMetrics(currentEstimate)
		etaEstimate := time.Duration(currentEstimate * (runs - i))

		clearCurrentTerminalLine(color.Output)
		line := fmt.Sprintf("Current estimate: %s ",
			color.GreenString("%.2f %s", float64(currentEstimate)/currentDenominator, currentUnitEstimate))
		printProgressLine(line, float64(i+1)/float64(runs), etaEstimate)
	}
	clearCurrentTerminalLine(color.Output)

	// Calculate mean, min and max timings
	minElapsed := int64(math.MaxInt64)
	maxElapsed := int64(math.MinInt64)
	var totalElapsed int64
	for _, elapsed := range elapsedRuns {
		totalElapsed += int64(elapsed)
		if elapsed < minElapsed {
			minElapsed = elapsed
		}
		if elapsed > maxElapsed {
			maxElapsed = elapsed
		}
	}

	result := &benchmarkResult{
		cmd:            cmdToBenchmark,
		mean:           totalElapsed / runs,
		min:            minElapsed,
		max:            maxElapsed,
		meanUserTime:   totalUserTime / runs,
		meanKernelTime: totalKernelTime / runs,
	}

	// Calculate standard deviation and get appropriate meansurement unit
	result.stdev = stdev(elapsedRuns, result.mean)
	result.denominator, result.unit = getMeasurementMetrics(result.mean)
	result.userDenominator, result.userUnit = getMeasurementMetrics(result.meanUserTime)
	result.kernelDenominator, result.kernelUnit = getMeasurementMetrics(result.meanKernelTime)
	return result, nil
}

func main() {
	flag.BoolVar(&noShell, "N", false, "Run benchmarks without an intermediate shell")
	flag.Int64Var(&runs, "runs", 10, "Number of rounds to warmup")
	flag.Int64Var(&warmup, "warmup", 0, "Number of rounds to warmup")
	flag.StringVar(&setupCmd, "setup", "", "Command to run before all benchmarks")
	flag.StringVar(&shell, "S", shell, "The intermediate shell to run benchmarks in")
	flag.BoolVar(&noColor, "no-color", false, "Disable coloured output")
	flag.Parse()
	cmds := flag.Args()

	color.NoColor = noColor
	ctx := context.Background()

	if setupCmd != "" {
		if err := runSetup(ctx, setupCmd); err != nil {
			fmt.Println("An error occurred during setup:", err)
			return
		}
	}

	if !noShell && shell != "" {
		// Measuring shell spawning time
	}

	results := make([]*benchmarkResult, 0, len(cmds))
	for i, cmd := range cmds {
		fmt.Printf("Benchmark #%d: %s\n", i+1, cmd)
		result, err := runBenchmark(ctx, cmd)
		if err != nil {
			fmt.Println("An error occurred during benchmark:", err)
		} else {
			fmt.Fprintf(color.Output, "  Time (%s ± %s):\t%s ± %s\t%s\n",
				color.GreenString("mean"),
				color.GreenString("σ"),
				color.GreenString("%.2f %s", float64(result.mean)/result.denominator, result.unit),
				color.GreenString("%.2f %s", result.stdev/result.denominator, result.unit),
				fmt.Sprintf("[User: %s, System: %s]",
					color.CyanString("%.2f %s", float64(result.meanUserTime)/result.userDenominator, result.userUnit),
					color.CyanString("%.2f %s", float64(result.meanKernelTime)/result.kernelDenominator, result.kernelUnit)))
			fmt.Fprintf(color.Output, "  Range (%s … %s):\t%s … %s\t%s\n",
				color.CyanString("min"),
				color.RedString("max"),
				color.CyanString("%.2f %s", float64(result.min)/result.denominator, result.unit),
				color.RedString("%.2f %s", float64(result.max)/result.denominator, result.unit),
				color.HiBlackString("%d runs", runs))
			fmt.Println()

			results = append(results, result)
		}
	}

	if len(results) > 1 {
		fmt.Println("Summary")

		sort.SliceStable(results, func(i, j int) bool { return results[i].mean < results[j].mean })
		var fastestResult *benchmarkResult
		for _, result := range results {
			if fastestResult == nil {
				fastestResult = result
				fmt.Fprintf(color.Output, "  '%s' ran\n", color.CyanString(fastestResult.cmd))
			} else {
				meanMultiplier := float64(result.mean) / float64(fastestResult.mean)
				posStdevMultiplier := (float64(result.mean)+result.stdev)/(float64(fastestResult.mean)+fastestResult.stdev) - meanMultiplier
				negStdevMultiplier := meanMultiplier - (float64(result.mean)-result.stdev)/(float64(fastestResult.mean)-fastestResult.stdev)
				fmt.Fprintf(color.Output, "    %s ± %s times faster than '%s'\n",
					color.GreenString("%.2f", meanMultiplier),
					color.GreenString("%.2f", math.Abs(posStdevMultiplier)+math.Abs(negStdevMultiplier)),
					color.RedString(result.cmd))
			}
		}
	}
}
