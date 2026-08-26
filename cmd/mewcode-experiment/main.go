package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"mewcode/internal/experiments"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	report, err := experiments.Run(context.Background(), *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, err := report.JSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
