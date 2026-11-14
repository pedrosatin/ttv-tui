package main

import (
	"math"
	"strconv"
	"strings"
)

func fontPoints(raw string) float64 {
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 6 || v > 72 {
		return 9
	}
	return v
}

func tuiEnvironment(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "TTV_WINDOW_TEST") {
			out = append(out, entry)
		}
	}
	return out
}
