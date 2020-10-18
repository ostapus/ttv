package torc

import (
	"fmt"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func GetEnv(name string, def string) string {
	v := os.Getenv(name)
	if v == "" {
		v = def
	}
	log.Debug("%s='%s'", name, v)
	return v
}

func getTrackerList() (rc [][]string) {
	url := "https://trackerslist.com/best.txt"
	log.Trace("Getting TrackerList from %s", url)
	resp, err := http.Get(url)
	list := make([]string, 0)
	if err != nil {
		log.Error("failed to get from %v: %v", url, err)
	} else {
		defer resp.Body.Close()
		data, err := ioutil.ReadAll(resp.Body)
		if err == nil {
			for _, v := range strings.Split(string(data), "\n") {
				if len(v) > 0 && (strings.HasPrefix(v, "http") || strings.HasPrefix(v, "udp")) {
					list = append(list, v)
				}
			}
		} else {
			log.Error("failed to get from err: %v %v -> '%v'", err, url, data)
		}
	}
	rc = make([][]string, 0)
	if len(list) > 5 {
		rc = append(rc, list)
	}
	log.Trace("Loaded trackers : %v", len(rc))
	return rc
}

func getExternalIP() string {
	url := "https://api.ipify.org?format=text"
	log.Trace("Getting IP address from  ipify")
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
	log.Trace("My external IP is:%s", rc)
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
	log.Trace("external port: %v", rc)
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

func (t Tags) getTime(name string, defval time.Time) time.Time {
	v, ok := t[name]
	if !ok {
		return defval
	}
	_time, err := time.Parse(time.RFC822, v.(string))
	if err != nil {
		return defval
	}
	return _time
}

func (t Tags) Set(name string, value interface{}) {
	oval := t[name]
	if oval != value {
		t[name] = value
		t.Invalidate(fmt.Sprintf("%s: %v != %v, tags are invalidated", name, oval, value))
	}
}

func (t Tags) SetIfNew(name string, value interface{}) {
	if _, ok := t[name]; !ok {
		t.Set(name, value)
	}
}

func (t Tags) Remove(name string) {
	if _, ok := t[name]; ok {
		t.Invalidate(fmt.Sprintf("%s key removed, tags are invalidated", name))
	}
	delete(t, name)
}

func (t Tags) Invalidate(comment string) {
	log.Trace(comment)
	t["tags_updated"] = "yes"
}

func (t Tags) Validate(comment string) {
	log.Trace(comment)
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

func ReadTagsFromFile(pathname string) (tags *Tags) {
	log.Debug("from %s", pathname)
	rc := Tags{}
	if data, err := ioutil.ReadFile(pathname); err == nil {
		if err := yaml.Unmarshal(data, &rc); err != nil {
			log.Error("failed to yaml.Unmarshal: %v : %v", pathname, err)
		}
	}
	log.Debug("loaded tags from %s", pathname)
	log.Debug("\n%s", rc.String())
	if len(rc) <= 0 {
		log.Warn("tags length %s is 0, keep old", pathname)
		return
	}
	return &rc
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
