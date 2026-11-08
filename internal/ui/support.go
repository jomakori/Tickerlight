package ui

import (
	"fmt"
	"strings"
)

// tabURL is the canonical route for a tab.
func tabURL(tab string) string {
	if tab == TabOverview {
		return "/"
	}
	return "/" + tab
}

// currentPage is the aria-current value for a nav link.
func currentPage(on bool) string {
	if on {
		return "page"
	}
	return "false"
}

// wordStyle is the inline size/colour for the arming wordmarks.
func wordStyle(size int, colour string) string {
	s := fmt.Sprintf("font-size:%dpx;font-weight:600", size)
	if colour != "" {
		s += ";color:" + colour
	}
	return s
}

func iconStyle(size int) string {
	if size <= 0 {
		return ""
	}
	return fmt.Sprintf("width:%dpx;height:%dpx", size, size)
}

func joinStates(states []string, sep string) string { return strings.Join(states, sep) }

// sel reports whether s is the selected value of a <select>/chip group.
func sel(cur, v string) bool {
	if cur == "" {
		cur = "all"
	}
	return cur == v
}
