package torc

import (
	sha "crypto/sha1"
	"hash"
	"encoding/hex"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

var h hash.Hash

func init() {
	h = sha.New()
}

type Cache struct {
	data_dir string
	fmap map[string]int64

	sync.Mutex
}

func NewCache(dir string) (c *Cache) {
	log.Trace("cache dir is %v", dir)
	rc := Cache{}
	rc.data_dir = dir
	stat, err := os.Stat(dir)
	if err != nil {
		if err := os.MkdirAll(dir, 0777); err != nil {
			panic(err)
		}
	} else {
		if ! stat.IsDir() {
			panic("dir " + dir + " already exists and not directory")
		}
	}
	rc.fmap = make(map[string]int64)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(err)
	}

	now := time.Now().Unix()

	re := regexp.MustCompile(`(.+)\.(\d+)$`)
	for _, f := range files {
		if r := re.FindStringSubmatch(f.Name()); len(r) > 0 {
			ext := r[2]
			name := r[1]
			v, _ := strconv.ParseInt(ext,10,64)
			if v < now {
				_name := dir + string(os.PathSeparator) + f.Name()
				log.Trace("  %s expired, removing file", f.Name())
				_ = os.Remove(_name)
				continue
			}
			rc.fmap[name] = v
			log.Trace("  %s expires in %d min", f.Name(), time.Unix(v-now, 0).Minute())
		}
	}
	go clearance(&rc)
	return &rc
}

func clearance(c *Cache) {
	for {
		time.Sleep(1*time.Minute)
		c.Lock()
		var remove string = ""
		now := time.Now().Unix()
		for {
			for k, v := range c.fmap {
				if v < now {
					log.Trace(" %s expired %v sec ago, removing", remove, now - v)
					remove = k
					break
				}
			}
			if remove != "" {
				delete( c.fmap, remove)
				_ = os.Remove(remove)
				remove = ""
			} else {
				break
			}
		}
		c.Unlock()
	}
}

func _hash(key string) string {
	h.Reset()
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Cache) Write(key string, value []byte, ttl time.Duration) error {
	c.Lock()
	defer c.Unlock()

	_skey := _hash(key)
	ts,ok := c.fmap[_skey]
	if ok {
		_fname := c.data_dir + string(os.PathSeparator) + _skey + "." + strconv.FormatInt(ts, 10)
		log.Trace(" have key: %s, refreshing file", key)
		_ = os.Remove(_fname)
	}
	exp := time.Now().Add(ttl).Unix()
	log.Trace("key: %s (%s), expires in %v min", key, _skey, ttl.Minutes())
	_fname := c.data_dir + string(os.PathSeparator) + _skey + "." + strconv.FormatInt( exp, 10)
	c.fmap[_skey] = exp
	return ioutil.WriteFile(_fname, value, 0666)
}

func (c *Cache) Read(key string) ([]byte, error) {
	c.Lock()
	defer c.Unlock()

	_skey := _hash(key)
	now := time.Now().Unix()

	if ts,ok := c.fmap[_skey]; ok {
		_fname := c.data_dir + string(os.PathSeparator) + _skey + "." + strconv.FormatInt(ts,10)
		log.Trace("key: %s (%s), expires in %v min", key, _skey, time.Unix(ts-now, 0).Minute())
		if now <= ts {
			log.Trace("key %s is good, returning from cache", key)
			if data, err := ioutil.ReadFile(_fname); err == nil {
				return data, nil
			}
		} else {
			log.Trace("removing expired file %s", _fname)
			_ = os.Remove(_fname)
			delete(c.fmap, _skey)
		}
	}
	return nil, nil
}
