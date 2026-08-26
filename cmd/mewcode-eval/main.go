// Command mewcode-eval runs the offline end-to-end evaluation fixture set.
// Production callers can import internal/eval and provide an Agent-backed
// Executor; this command intentionally has no network or provider dependency.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"mewcode/internal/eval"
)

func main() {
	jsonOutput := flag.Bool("json", true, "print a JSON report")
	flag.Parse()
	report, err := eval.Run(context.Background(), eval.DefaultTasks(), eval.DefaultExecutor)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *jsonOutput {
		data, err := report.JSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(string(data))
		return
	}
	fmt.Printf("tasks=%d success=%d success_rate=%.2f avg_ms=%.1f input_tokens=%d output_tokens=%d\n",
		report.Tasks, report.Successful, report.SuccessRate, report.AvgDurationMs, report.TotalInputTokens, report.TotalOutputTokens)
}
