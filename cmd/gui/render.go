package gui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/DataDog/datadog-agent/pkg/collector/autodiscovery"
	"github.com/DataDog/datadog-agent/pkg/collector/check"
	"github.com/DataDog/datadog-agent/pkg/config"
	log "github.com/cihub/seelog"
)

var fmap = template.FuncMap{
	"doNotEscape":        doNotEscape,
	"lastError":          lastError,
	"lastErrorTraceback": lastErrorTraceback,
	"lastErrorMessage":   lastErrorMessage,
	"pythonLoaderError":  pythonLoaderError,
	"formatUnixTime":     formatUnixTime,
	"humanize":           mkHuman,
	"humanizeInt":        mkHumanInt,
	"formatTitle":        formatTitle,
	"add":                add,
}

type CheckStats struct {
	Name  string
	Stats []*check.Stats
}

type Errors struct {
	Name       string
	LoaderErrs map[string]autodiscovery.LoaderErrors
	ParsingErr string
}

func renderStatus(data []byte, request string) (string, error) {
	var b = new(bytes.Buffer)
	stats := make(map[string]interface{})
	json.Unmarshal(data, &stats)

	e := fillTemplate(b, stats, request+"Status")
	if e != nil {
		return "", e
	}
	return b.String(), nil
}

func fillTemplate(w io.Writer, stats map[string]interface{}, request string) error {
	t := template.New(request + ".tmpl")
	t.Funcs(fmap)
	t, e := t.ParseFiles("templates/" + request + ".tmpl")
	if e != nil {
		return e
	}

	e = t.Execute(w, stats)
	return e
}

func renderCheck(name string, stats []*check.Stats) (string, error) {
	var b = new(bytes.Buffer)

	t := template.New("singleCheck.tmpl")
	t.Funcs(fmap)
	t, e := t.ParseFiles("templates/singleCheck.tmpl")
	if e != nil {
		return "", e
	}

	cs := CheckStats{name, stats}
	e = t.Execute(b, cs)
	if e != nil {
		return "", e
	}
	return b.String(), nil
}

func renderError(name string) (string, error) {
	var b = new(bytes.Buffer)

	t := template.New("loaderErr.tmpl")
	t.Funcs(fmap)
	t, e := t.ParseFiles("templates/loaderErr.tmpl")
	if e != nil {
		return "", e
	}

	loaderErrs := autodiscovery.GetLoaderErrors()

	// Check if there's a message in the log indicating the config file had an error
	parserErr := ""
	logFile, e := os.Open(config.Datadog.GetString("log_file"))
	if e != nil {
		log.Errorf("GUI - Error reading log file: " + e.Error())
		return "", e
	}
	scanner := bufio.NewScanner(logFile)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), name+".yaml is not a valid config file:") {
			parserErr = scanner.Text()

			// Get the lines containing the error
			for scanner.Scan() {
				if !(strings.Contains(scanner.Text(), "| WARN |") ||
					strings.Contains(scanner.Text(), "| INFO |") ||
					strings.Contains(scanner.Text(), "| ERROR |")) {
					parserErr += scanner.Text()
				} else {
					break
				}
			}
		}
	}

	errs := Errors{name, loaderErrs, parserErr}
	e = t.Execute(b, errs)
	if e != nil {
		return "", e
	}
	return b.String(), nil
}

/****** Helper functions for the template formatting ******/

var timeFormat = "2006-01-02 15:04:05.000000 UTC"

func doNotEscape(value string) template.HTML {
	return template.HTML(value)
}

func pythonLoaderError(value string) template.HTML {
	value = strings.Replace(value, "', '", "", -1)
	value = strings.Replace(value, "['", "", -1)
	value = strings.Replace(value, "\\n']", "", -1)
	value = strings.Replace(value, "']", "", -1)
	value = strings.Replace(value, "\\n", "<br>", -1)
	value = strings.Replace(value, "  ", "&nbsp;&nbsp;&nbsp;", -1) // unchecked
	var loaderErrorArray []string
	json.Unmarshal([]byte(value), &loaderErrorArray)
	return template.HTML(value)
}

func lastError(value string) template.HTML {
	return template.HTML(value)
}

func lastErrorTraceback(value string) template.HTML {
	var lastErrorArray []map[string]string

	err := json.Unmarshal([]byte(value), &lastErrorArray)
	if err != nil || len(lastErrorArray) == 0 {
		return template.HTML("No traceback")
	}

	lastErrorArray[0]["traceback"] = strings.Replace(lastErrorArray[0]["traceback"], "\n", "<br>", -1)
	lastErrorArray[0]["traceback"] = strings.Replace(lastErrorArray[0]["traceback"], "  ", "&nbsp;&nbsp;&nbsp;", -1)

	return template.HTML(lastErrorArray[0]["traceback"])
}

func lastErrorMessage(value string) template.HTML {
	var lastErrorArray []map[string]string
	err := json.Unmarshal([]byte(value), &lastErrorArray)
	if err == nil && len(lastErrorArray) > 0 {
		if _, ok := lastErrorArray[0]["message"]; ok {
			return template.HTML(lastErrorArray[0]["message"])
		}
	}
	return template.HTML("UNKNOWN ERROR")
}

func formatUnixTime(unixTime float64) string {
	var (
		sec  int64
		nsec int64
	)
	ts := fmt.Sprintf("%f", unixTime)
	secs := strings.Split(ts, ".")
	sec, _ = strconv.ParseInt(secs[0], 10, 64)
	if len(secs) == 2 {
		nsec, _ = strconv.ParseInt(secs[1], 10, 64)
	}
	t := time.Unix(sec, nsec)
	return t.Format(timeFormat)
}

func mkHuman(f float64) string {
	i := int64(f)
	str := fmt.Sprintf("%d", i)

	if i > 1000000 {
		str = "over 1M"
	} else if i > 100000 {
		str = "over 100K"
	}

	return str
}

func mkHumanInt(i int64) string {
	str := fmt.Sprintf("%d", i)

	if i > 1000000 {
		str = "over 1M"
	} else if i > 100000 {
		str = "over 100K"
	}

	return str
}

func formatTitle(title string) string {
	if title == "os" {
		return "OS"
	}

	// Split camel case words
	var words []string
	l := 0
	for s := title; s != ""; s = s[l:] {
		l = strings.IndexFunc(s[1:], unicode.IsUpper) + 1
		if l <= 0 {
			l = len(s)
		}
		words = append(words, s[:l])
	}
	title = strings.Join(words, " ")

	// Capitalize the first letter
	return strings.Title(title)
}

func add(x, y int) int {
	return x + y
}