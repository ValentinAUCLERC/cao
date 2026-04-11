package app

import (
	"io"
	"os"
	"strings"
)

type outputStyle struct {
	enabled bool
}

func detectOutputStyle(w io.Writer) outputStyle {
	file, ok := w.(*os.File)
	if !ok {
		return outputStyle{}
	}
	if force := os.Getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
		return outputStyle{enabled: true}
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0" {
		return outputStyle{}
	}
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		return outputStyle{}
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return outputStyle{}
	}
	return outputStyle{enabled: true}
}

func (s outputStyle) wrap(code, value string) string {
	if !s.enabled || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func (s outputStyle) heading(value string) string {
	return s.wrap("1;36", value)
}

func (s outputStyle) command(value string) string {
	return s.wrap("1;36", value)
}

func (s outputStyle) label(value string) string {
	return s.wrap("1", value)
}

func (s outputStyle) code(value string) string {
	return s.wrap("36", value)
}

func (s outputStyle) success(value string) string {
	return s.wrap("1;32", value)
}

func (s outputStyle) warning(value string) string {
	return s.wrap("1;33", value)
}

func (s outputStyle) danger(value string) string {
	return s.wrap("1;31", value)
}

func (s outputStyle) info(value string) string {
	return s.wrap("1;34", value)
}

func (s outputStyle) muted(value string) string {
	return s.wrap("2", value)
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}
