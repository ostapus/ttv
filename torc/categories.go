package torc

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"
)

type Op uint32

const (
	CategoryCreated = 1 << iota
	CategoryRemoved
	CategoryLoaded
	TorrentFileCreated
	TorrentFileRemoved
)

type Event struct {
	Category *tCategory
	File     string
	FullPath string
	Op       Op
}

type tCategory struct {
	name     string
	fullpath string
	download string
	ready    bool
}

func (t tCategory) String() string {
	return fmt.Sprintf("tCategory{ %s %s %s rdy: %v",
		t.name, t.fullpath, t.download, t.ready)
}

var (
	categories = make(map[string]*tCategory, 0)
	fsw        *fsnotify.Watcher
	basedir    string
	notify     chan Event
)

func init() {
	var err error
	if fsw, err = fsnotify.NewWatcher(); err != nil {
		panic(log.Error("failed to create fsnotify watcher: %v", err))
	}
	go fswEvent()
}

func NewCategoryWatcher(dir string) chan Event {
	notify = make(chan Event, 5)
	go func() {
		scanCategories(dir)
		notify <- Event{Op: CategoryLoaded}
	}()
	return notify
}

func GetCategories() map[string]*tCategory {
	return categories
}

func GetCategory(name string) (*tCategory, bool) {
	v, ok := categories[name]
	return v, ok
}

func GetCategoryOrDefault(name string, defvalue string) (category *tCategory) {
	category, ok := categories[name]
	if !ok {
		category, ok = categories[defvalue]
		if !ok {
			panic(log.Error("GetCategoryOrDefault: %s (or %s) doesn't exists", name, defvalue))
			return
		}
	}
	return
}

func scanCategories(dir string) {
	log.Debug("scanning categories in %s : '%s'", basedir, dir)
	if path.IsAbs(dir) {
		basedir = dir
	} else {
		basedir = path.Clean(dir)
		if cwd, err := os.Getwd(); err == nil {
			basedir = path.Join(cwd, basedir)
		}
		if st, err := os.Stat(basedir); !(err == nil && st.IsDir()) {
			panic(log.Error("torrentsDir '%s' doesn't exists or not directory", basedir))
		}
	}

	// scan for new categories
	entries, _ := ioutil.ReadDir(basedir)
	for _, e := range entries {
		if !e.IsDir() {
			log.Error("category %v/%v is not directory, ignoring", basedir, e.Name())
			continue
		}
		_c, ok := categories[e.Name()]
		if !ok {
			_c = &tCategory{
				name:     e.Name(),
				fullpath: path.Join(basedir, e.Name()),
				download: path.Join(basedir, e.Name(), "downloads"),
				ready:    true,
			}
			categories[e.Name()] = _c
		}

		if st, err := os.Stat(_c.download); !(err == nil && st.IsDir()) {
			log.Error("category %s has no downloads %s . ignoring for now", _c.name, _c.download)
			_c.ready = false
			continue
		}
		onCategoryCreated(_c)
	}

	// create watchers
	log.Debug("watching root dir: %s", basedir)
	if err := fsw.Add(basedir); err != nil {
		panic(log.Error("failed to fswatcher.add %s: %v", basedir, err))
	}

	for _, v := range categories {
		if err := fsw.Add(v.fullpath); err != nil {
			panic(log.Error("failed to fswatcher.add %s: %v", v.fullpath, err))
		}
		log.Debug("watching for %s", v.fullpath)
	}
	//

	log.Debug("done")
}

func findCategoryByPath(path string) (category *tCategory, isDownloadDir bool, filePart string) {
	// find category
	for _, v := range categories {
		if path == v.download {
			log.Debug("category %s, download dir event %s == %s", v.name, v.download, path)
			return v, true, ""
		}
		if path == v.fullpath {
			log.Debug("category %s, basedir dir event %s == %s", v.name, v.fullpath, path)
			return v, false, ""
		}
		if strings.HasPrefix(path, v.fullpath) {
			log.Debug("category %s, sub file/dir event %s in %s", v.name, path, v.fullpath)
			filePart := strings.TrimPrefix(path, v.fullpath)
			return v, false, strings.TrimPrefix(filePart, "/")
		}
	}
	log.Debug("not found in categories")
	return nil, false, ""
}

func onCategoryRemoved(cat *tCategory) {
	if cat.ready {
		cat.ready = false
		log.Debug("%v", cat)
		notify <- Event{Category: cat, Op: CategoryRemoved}
	}
}

func onCategoryCreated(cat *tCategory) {
	log.Debug("%v", cat)
	notify <- Event{Category: cat, Op: CategoryCreated}
}

func onFileRemoved(cat *tCategory, fullpath string, file string) {
	if !IsValidTorrentFile(fullpath, false) {
		return
	}
	log.Debug("%v: %s", cat, file)
	notify <- Event{Category: cat, Op: TorrentFileRemoved, File: file, FullPath: fullpath}
}

func onFileCreated(cat *tCategory, fullpath string, file string) {
	if !IsValidTorrentFile(fullpath, true) {
		return
	}
	log.Debug("%v: %s -> %s, channel len: %v/%v", cat, fullpath, file, len(notify), cap(notify))
	notify <- Event{Category: cat, Op: TorrentFileCreated, File: file, FullPath: fullpath}
	log.Debug("event fired")
}

var (
	writes = make(map[string]*time.Timer, 0)
)

func processFswEvent(event fsnotify.Event) {
	log.Trace("fs event %v", event)
	if strings.HasPrefix(event.Name, ".") {
		log.Trace("ignoring 'hidden' name: %s", event.Name)
		return
	}

	switch event.Op {
	case fsnotify.Rename:
		fallthrough
	case fsnotify.Remove:
		if cat, isdd, file := findCategoryByPath(event.Name); cat == nil {
			log.Warn("file/dir %s removed from watched dirs, but no category found", event.Name)
		} else if isdd {
			log.Debug("%s: download dir is removed %s", cat.name, cat.download)
			onCategoryRemoved(cat)
		} else if file != "" {
			onFileRemoved(cat, event.Name, file)
		} else {
			onCategoryRemoved(cat)
			_ = fsw.Remove(cat.fullpath)
			delete(categories, cat.name)
		}

	case fsnotify.Create:
		fallthrough
	case fsnotify.Write:
		st, err := os.Stat(event.Name)
		if err != nil {
			log.Warn("%s (%v): stat failed. ignore", event.Name, err)
			return
		}
		if st.IsDir() {
			log.Debug("new directory on category level, run rescan")
			scanCategories(basedir)
			return
		}

		if !(strings.HasSuffix(event.Name, ".torrent") ||
			strings.HasSuffix(event.Name, ".magnet") ||
			strings.HasSuffix(event.Name, ".yaml")) {
			log.Trace("ignore unknown extension. not torrent/magnet/yaml: %s", event.Name)
			return
		}
		timer, ok := writes[event.Name]
		log.Trace("starting/extending 2 sec timer on WRITE for '%s', timer: %v, exists: %v", event.Name, timer, ok)
		if ok && timer != nil {
			log.Trace("timer exists for '%s', stopping this, starting new", event.Name)
			timer.Stop()
		}
		writes[event.Name] = time.AfterFunc(time.Second*2, func() {
			if cat, _, file := findCategoryByPath(event.Name); cat == nil {
				log.Warn("file %s done WRITES, but no category found", event.Name)
			} else if file == "" {
				log.Warn("file %s done WRITES, findCategory said is not file", event.Name)
			} else {
				log.Trace("WRITE timer expires, firing onFileCreated event: '%s'", file)
				onFileCreated(cat, event.Name, file)
			}
			writes[event.Name] = nil
		})
	}
}

func fswEvent() {
	for {
		select {
		case event := <-fsw.Events:
			processFswEvent(event)

		case err := <-fsw.Errors:
			panic(log.Error("baseDirWatcher error: %v", err))
		}
	}
}
