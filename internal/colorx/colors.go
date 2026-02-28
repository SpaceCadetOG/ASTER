package colorx

import "fmt"

const reset = "\033[0m"

const (
	red    = "\033[31m"
	green  = "\033[32m"
	gold   = "\033[38;5;220m"
	blue   = "\033[34m"
	gray   = "\033[90m"
)

// Side coloring
func Buy(s string) string  { return green + s + reset }
func Sell(s string) string { return red + s + reset }

// Size coloring
func Gold(s string) string { return gold + s + reset }
func Blue(s string) string { return blue + s + reset }
func Gray(s string) string { return gray + s + reset }

// Dynamic size tier (easy to expand later)
func BySize(usd float64, text string) string {
	switch {
	case usd >= 10000:
		return Gold(text)
	case usd >= 3000:
		return Blue(text)
	default:
		return Gray(text)
	}
}

// Utility
func F(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}