package neocities

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func paint(code, s string) string {
	return "\033[" + code + "m" + s + "\033[0m"
}

func bold(s string) string       { return paint("1", s) }
func dim(s string) string        { return paint("2", s) }
func green(s string) string      { return paint("32", s) }
func red(s string) string        { return paint("31", s) }
func greenBold(s string) string  { return paint("1;32", s) }
func redBold(s string) string    { return paint("1;31", s) }
func yellowBold(s string) string { return paint("1;33", s) }
func blueBold(s string) string   { return paint("1;34", s) }
func magentaBold(s string) string {
	return paint("1;35", s)
}
func brightRed(s string) string  { return paint("91", s) }
func onCyanBold(s string) string { return paint("1;46", s) }

func colWidths(rows [][]string) []int {
	if len(rows) == 0 {
		return nil
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

func writeRow(w io.Writer, cells []string, widths []int, colors []func(string) string) {
	for i, cell := range cells {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		text := cell
		if i < len(colors) && colors[i] != nil {
			text = colors[i](cell)
		}
		fmt.Fprint(w, text)
		if i < len(cells)-1 {
			if pad := widths[i] - utf8.RuneCountInString(cell); pad > 0 {
				fmt.Fprint(w, strings.Repeat(" ", pad))
			}
		}
	}
	fmt.Fprintln(w)
}
