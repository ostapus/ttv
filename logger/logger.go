package logger

import (
	"bufio"
	"github.com/fsnotify/fsnotify"
	"log"
	"io"
	"runtime"
	"strings"
	"fmt"
	"os"
	"sync"
)

//go:generate stringer -type=LogLevel

type LogLevel int

const (
	NONE   	LogLevel = iota
	ERROR
	WARN
	INFO
	DEBUG
	TRACE
)

type Log struct {
	logger *log.Logger
	level LogLevel
	sync.Mutex

	utctolocal bool
	watcher *fsnotify.Watcher
	traceStrs []string
}

func (l *Log) UtcToLocal(convert bool) {
	l.utctolocal = convert
}

func prefix() (string) {
	pc, file, line, ok := runtime.Caller(3)
	if !ok {
		return "unknown"
	}
	fname := runtime.FuncForPC(pc).Name()
	li := strings.LastIndexAny(fname, ".")
	if li != -1 {
		fname = fname[li+1:]
	}
	if li := strings.LastIndexByte(file, os.PathSeparator); li != -1 {
		file = file[li+1:]
	}

	return fmt.Sprintf("%s:%d (%s):", file, line, fname)
}

func watcherThread(ret *Log) {
	w := ret.watcher
	for {
		select {
		case event := <-w.Events:
			if event.Op&fsnotify.Remove != 0 {
				ret.Trace("file %v removed", event.Name)
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				ret.Trace("%v was changed, reading", event.Name)
				file, err := os.OpenFile(event.Name, os.O_RDONLY, 0666)
				if err != nil {
					ret.Error("can't open %v - %v", event.Name, err.Error())
					continue
				}
				scan := bufio.NewReader(file)
				nt := make([]string,0)
				for {
					line, err := scan.ReadString('\n')
					if err != nil && err != io.EOF {
						ret.Error("error reading %v - %v", event.Name, err.Error())
					}
					line = strings.Trim(line, " \t\r\n")
					if line != "" {
						ret.Trace("got '%s'", line)
						nt = append(nt, line)
					}
					if err != nil {
						break
					}
				}
				ret.Trace("done reading %d", len(nt))
				ret.Lock()
				ret.traceStrs = nt
				ret.Unlock()
				file.Close()
			} else if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				ret.Lock()
				ret.traceStrs = make([]string, 0)
				ret.Trace("%v got renamed/removed, no trace", event.Name)
				ret.Unlock()
			}
		}
	}
}

func (ret *Log) initLog(out io.Writer, flags int ) {
	ret.traceStrs = make([]string, 0)
	if w, err := fsnotify.NewWatcher(); err == nil {
		ret.watcher = w
		go watcherThread(ret)
	}
}

func (l *Log) Level(v ...interface{}) (LogLevel){

	if len(v) > 0 {
		level := v[0].(LogLevel)
		l.level = level
	}
	return l.level
}

func (l *Log) InitLogger(out io.Writer) {
	l.logger = log.New(out, "", log.Ltime)
	l.level = ERROR
	l.initLog(out, log.Ltime)
}

func (l *Log) SetTraceFile(name string) error {
	if l.watcher != nil {
		if err := l.watcher.Add(name); err != nil {
			return err
		}
		l.watcher.Events <- fsnotify.Event{Name: name, Op: fsnotify.Create}
	}
	return nil
}

func ( l *Log) Println(level LogLevel, v ...interface{}) string {
	l.Lock()
	defer l.Unlock()

	msg := ""
	if len(v) > 0 {
		switch v[0].(type) {
		case string:
			msg = fmt.Sprintf(v[0].(string), v[1:] ...)
		default:
			msg = fmt.Sprintln(v)
		}
	} else {
		return ""
	}

	prefix := prefix()
	do := true
	if l.level < level {
		do = false
		for _, v := range l.traceStrs {
			exc := false
			if strings.HasPrefix(v, "!") {
				exc = true
				v = v[1:]
			}

			if strings.Contains(msg, v) || strings.Contains(prefix, v) {
				if !exc {
					do = true
				}
				break
			}
		}
	}
	s := ""
	if do {
		s = fmt.Sprintf("%s %5s %s", prefix, level, msg)
		l.logger.Println(s)
	}
	return s
}


func (l *Log) Warn(v ...interface{}) string {
	return l.Println(WARN, v ...)
}

func (l *Log) Error(v ...interface{}) string {
	return l.Println(ERROR, v ...)
}

func (l *Log) Info(v ...interface{}) string {
	return l.Println(INFO, v ...)
}

func (l *Log) Debug(v ...interface{}) string {
	return l.Println(DEBUG, v ...)
}

func (l *Log) Trace(v ...interface{}) string {
	return l.Println(TRACE, v ...)
}
