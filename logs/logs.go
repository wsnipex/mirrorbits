// Copyright (c) 2014-2015 Ludovic Fauvet
// Licensed under the MIT license

package logs

import (
	"bytes"
	"fmt"
	"github.com/op/go-logging"
	. "github.com/wsnipex/mirrorbits/config"
	"github.com/wsnipex/mirrorbits/core"
	"github.com/wsnipex/mirrorbits/mirrors"
	"io"
	stdlog "log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	log          = logging.MustGetLogger("main")
	rlogger      runtimeLogger
	dlogger      downloadsLogger
	loglevel     logging.Level
	nameToLevels = map[string]logging.Level{
		"CRITICAL": logging.CRITICAL,
		"ERROR":    logging.ERROR,
		"WARNING":  logging.WARNING,
		"NOTICE":   logging.NOTICE,
		"INFO":     logging.INFO,
		"DEBUG":    logging.DEBUG,
	}
)

type runtimeLogger struct {
	f *os.File
}

type downloadsLogger struct {
	sync.RWMutex
	l *stdlog.Logger
	f io.WriteCloser
}

func (d *downloadsLogger) Close() {
	if d.f != nil {
		d.f.Close()
		d.f = nil
	}
	d.l = nil
}

// ReloadLogs will reopen the logs to allow rotations
func ReloadLogs() {
	ReloadRuntimeLogs()
	if core.Daemon {
		ReloadDownloadLogs()
	}
}

func isTerminal(f *os.File) bool {
	stat, _ := f.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return true
	}
	return false
}

func ReloadRuntimeLogs() {
	if rlogger.f == os.Stderr && core.RunLog == "" {
		if SafeGetConfig() == nil || GetConfig().ErrorLog == "" {
			// Logger already set up and connected to the console.
			// Don't reload to avoid breaking journald.
			return
		}
	}

	if rlogger.f != nil && rlogger.f != os.Stderr {
		rlogger.f.Close()
	}

	runlog := ""
	if core.RunLog != "" {
		runlog = core.RunLog
	} else if SafeGetConfig() != nil {
		runlog = GetConfig().ErrorLog
	}

	if runlog != "" {
		var err error
		rlogger.f, _, err = openLogFile(runlog)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot open log file for writing")
			rlogger.f = os.Stderr
		}
	} else {
		rlogger.f = os.Stderr
	}

	logBackend := logging.NewLogBackend(rlogger.f, "", 0)
	logBackend.Color = isTerminal(rlogger.f) //TODO make color optional

	logging.SetBackend(logBackend)

	if core.Debug {
		logging.SetFormatter(logging.MustStringFormatter("%{shortfile:-20s}%{time:2006/01/02 15:04:05.000 MST} %{message}"))
		logging.SetLevel(logging.DEBUG, "main")
	} else {
		loglevel = logging.INFO
		if SafeGetConfig() != nil {
			loglevel = nameToLevels[GetConfig().LogLevel]
		}
		logging.SetFormatter(logging.MustStringFormatter("%{time:2006/01/02 15:04:05.000 MST} %{message}"))
		logging.SetLevel(loglevel, "main")
		log.Critical("LogLevel set to: %s", logging.GetLevel("main"))
	}
}

func openLogFile(logfile string) (*os.File, bool, error) {
	newfile := true

	s, _ := os.Stat(logfile)
	if s != nil && s.Size() > 0 {
		newfile = false
	}

	f, err := os.OpenFile(logfile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0664)
	if err != nil {
		return nil, false, err
	}

	return f, newfile, nil
}

func setDownloadLogWriter(writer io.Writer, createHeader bool) {
	dlogger.l = stdlog.New(writer, "", stdlog.Ldate|stdlog.Lmicroseconds)

	if createHeader {
		var buf bytes.Buffer
		hostname, _ := os.Hostname()
		fmt.Fprintf(&buf, "# Log file created at: %s\n", time.Now().Format("2006/01/02 15:04:05"))
		fmt.Fprintf(&buf, "# Running on machine: %s\n", hostname)
		fmt.Fprintf(&buf, "# Binary: Built with %s %s for %s/%s\n", runtime.Compiler, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		writer.Write(buf.Bytes())
	}
}

func ReloadDownloadLogs() {
	dlogger.Lock()
	defer dlogger.Unlock()

	dlogger.Close()

	if GetConfig().AccessLog == "" {
		return
	}

	logfile := GetConfig().AccessLog
	f, createHeader, err := openLogFile(logfile)
	if err != nil {
		log.Critical("Cannot open log file %s", logfile)
		return
	}

	setDownloadLogWriter(f, createHeader)
}

// This function will write a download result in the logs.
func LogDownload(typ string, statuscode int, p *mirrors.Results, err error, ua string) {
	dlogger.RLock()
	defer dlogger.RUnlock()

	if dlogger.l == nil {
		// Logs are disabled
		return
	}

	errstr := "<unknown>"
	if err != nil {
		errstr = err.Error()
	}

	if (statuscode == 302 || statuscode == 200) && p != nil && len(p.MirrorList) > 0 {
		var distance, countries string
		m := p.MirrorList[0]
		distance = strconv.FormatFloat(float64(m.Distance), 'f', 2, 32)
		countries = strings.Join(m.CountryFields, ",")
		fallback := ""
		if p.Fallback == true {
			fallback = " fallback:true"
		}
		sameASNum := ""
		if m.Asnum > 0 && m.Asnum == p.ClientInfo.ASNum {
			sameASNum = "same"
		}

		dlogger.l.Printf("%s %d \"%s\" ip:%s mirror:%s%s %sasn:%d distance:%skm countries:%s useragent:%s",
			typ, statuscode, p.FileInfo.Path, p.IP, m.ID, fallback, sameASNum, m.Asnum, distance, countries, ua)
	} else if statuscode == 404 && p != nil {
		dlogger.l.Printf("%s 404 \"%s\" ip:%s", typ, p.FileInfo.Path, p.IP)
	} else if statuscode == 500 && p != nil {
		mirrorID := "unknown"
		if len(p.MirrorList) > 0 {
			mirrorID = p.MirrorList[0].ID
		}
		dlogger.l.Printf("%s 500 \"%s\" ip:%s mirror:%s error:%s", typ, p.FileInfo.Path, p.IP, mirrorID, errstr)
	} else {
		var path, ip string
		if p != nil {
			path = p.FileInfo.Path
			ip = p.IP
		}
		dlogger.l.Printf("%s %d \"%s\" ip:%s error:%s", typ, statuscode, path, ip, errstr)
	}
}
