package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"golang.org/x/term"
)

var denominators = []int64{int64(time.Hour), int64(time.Minute), int64(time.Second), int64(time.Millisecond), int64(time.Microsecond), int64(time.Nanosecond)}
var units = []string{"h", "m", "s", "ms", "µs", "ns"}

// Translate a sequence of arguments into a command line string, using the same rules as the MS C runtime:
// 1) Arguments are delimited by white space, which is either a space or a tab.
// 2) A string surrounded by double quotation marks is interpreted as a single argument,
//	regardless of white space contained within.  A quoted string can be embedded in an argument.
// 3) A double quotation mark preceded by a backslash is interpreted as a literal double quotation mark.
// 4) Backslashes are interpreted literally, unless they immediately precede a double quotation mark.
// 5) If backslashes immediately precede a double quotation mark,
// 	every pair of backslashes is interpreted as a literal backslash.
//  If the number of backslashes is odd, the last backslash escapes the next double quotation mark as described in rule 3.
func list2Cmdline(cmd string) []string {
	var cmdParts []string
	var inQuote rune

	var b strings.Builder
	for i, ch := range cmd {
		if (ch == '"' || ch == '\'') && (i == 0 || cmd[i-1] != '\\') {
			switch inQuote {
			case rune(0):
				inQuote = ch
			case ch:
				inQuote = rune(0)
			default:
				b.WriteRune(ch)
			}
		} else if (ch == ' ' || ch == '\t') && inQuote == 0 {
			cmdParts = append(cmdParts, b.String())
			b.Reset()
		} else {
			b.WriteRune(ch)
		}
	}
	cmdParts = append(cmdParts, b.String())
	return cmdParts
}

func getMeasurementMetrics(timing int64) (float64, string) {
	for i, denominator := range denominators {
		if timing/int64(denominator) > 0 {
			return float64(denominator), units[i]
		}
	}
	return 0, ""
}

func stdev(values []int64, mean int64) float64 {
	var numerator int64
	for _, value := range values {
		delta := value - mean
		numerator += delta * delta
	}
	return math.Sqrt(float64(numerator) / float64(len(values)-1))
}

func clearCurrentTerminalLine(w io.Writer) {
	w.Write([]byte("\r\033[K"))
}

func printProgressLine(line string, progress float64, eta time.Duration) {
	// Calculate progress bar
	terminalWidth, _, _ := term.GetSize(int(os.Stdout.Fd()))
	terminalWidth -= len(line) + 2 + 12
	progressChunks := int(progress * float64(terminalWidth))
	progressLine := strings.Repeat(progressDoneRune, progressChunks)
	progressLine += strings.Repeat(progressPendingRune, terminalWidth-progressChunks)

	fmt.Fprintf(color.Output, "%s %s ETA %02d:%02d:%02d", line, progressLine,
		int64(eta.Hours()), int64(eta.Minutes()), int64(eta.Seconds()))
}
