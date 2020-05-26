package torc

import (
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

func GetEnv(name string, def string) string {
	v := os.Getenv(name)
	if v == "" {
		v = def
	}
	log.Debug("%s='%s'", name, v)
	return v
}

func getExternalIP() string {
	url := "https://api.ipify.org?format=text"
	log.Debug("Getting IP address from  ipify")
	resp, err := http.Get(url)
	rc := ""
	if err != nil {
		log.Error("failed to get from %v: %v", url, err)
	} else {
		defer resp.Body.Close()
		ip, err := ioutil.ReadAll(resp.Body)
		re := regexp.MustCompile(`\d+\.\d+\.\d+\.\d+`)
		if err == nil && re.Match(ip) {
			rc = string(ip)
		} else {
			log.Error("failed to get from err: %v %v -> '%v'", err, url, ip)
		}
	}
	log.Debug("My external IP is:%s", rc)
	return rc
}

func getExternalPort(port_file string) int {
	rc := int(0)
	buf, err := ioutil.ReadFile(port_file)
	if err != nil {
		log.Error("failed to read %v - %v", port_file, err)
	} else {
		i, err := strconv.ParseInt(strings.TrimSpace(string(buf)), 10, 32)
		if err != nil {
			log.Error("failed to parse int from %v - %v", string(buf), err)
		} else {
			rc = int(i)
		}
	}
	log.Debug("external port: %v", rc)
	return rc
}

func newError(format string, v ...interface{}) error {
	return errors.Errorf(format, v)
}

//
type Tags map[string]interface{}
type mapOfStrings map[string]string

func (t Tags) get(name string, defval interface{}) interface{} {
	v, ok := t[name]
	if !ok {
		return defval
	}
	return v
}

func (t Tags) getBool(name string, defval bool) bool {
	v, ok := t[name]
	if !ok {
		return defval
	}
	return v.(bool)
}

func (t Tags) getString(name string, defval string) string {
	v, ok := t[name]
	if !ok {
		return defval
	}
	return v.(string)
}

func (t Tags) getInt(name string, defval int) int {
	v, ok := t[name]
	if !ok {
		return defval
	}
	return v.(int)
}

func (t Tags) getInt64(name string, defval int64) int64 {
	v, ok := t[name]
	if !ok {
		return defval
	}
	return v.(int64)
}

func (t Tags) Set(name string, value interface{}) {
	oval := t[name]
	t[name] = value
	if oval != value {
		t.Invalidate()
	}
}

func (t Tags) SetIfNew(name string, value interface{}) {
	if _, ok := t[name]; !ok {
		t[name] = value
		t.Invalidate()
	}
}

func (t Tags) Remove(name string) {
	if _, ok := t[name]; ok {
		t.Invalidate()
	}
	delete(t, name)
}

func (t Tags) Invalidate() {
	t["tags_updated"] = "yes"
}

func (t Tags) Validate() {
	delete(t, "tags_updated")
}

func (t Tags) Validated() bool {
	return t.getString("tags_updated", "no") == "no"
}

func (t Tags) String() (rc string) {
	rc = ""
	for k, v := range t {
		rc += fmt.Sprintf("  '%s': '%v'\n", k, v)
	}
	return
}

func IsValidTorrentFile(fullpathname string, checkExistsFile bool) bool {
	if checkExistsFile {
		if stat, err := os.Stat(fullpathname); err != nil || stat.IsDir() {
			return false
		}
	}
	_, file := path.Split(fullpathname)
	if strings.HasPrefix(file, ".") {
		return false
	}
	if !(strings.HasSuffix(file, ".torrent") ||
		strings.HasSuffix(file, ".magnet") ||
		strings.HasSuffix(file, ".yaml")) {
		return false
	}
	return true
}
