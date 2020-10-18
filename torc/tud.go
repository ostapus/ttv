package torc

import "C"
import (
	"fmt"
	tt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"strconv"
	"time"
)

const LOAD_FROM_START = 10
const LOAD_FROM_END = 10

func customPathMaker(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
	tud, _ := torClient.GetTorrent(infoHash.HexString())
	if tud == nil {
		if tud, _ = torClient.GetTorrent(info.Name); tud == nil {
			panic(log.Error("customPathMaker: GetTorrent failed for: %s", info.Name))
		}
	}
	dir := tud.Tags.getString("download", "")
	if dir == "" {
		panic(log.Error("customPathMaker: Tags: download is '' for: %s", info.Name))
	}
	return dir
}

type TorrentWithUserData struct {
	//
	c                   *TorrentClient
	files               []*TorrentFile
	maxConnections      int
	unpaused            time.Time
	unpaused_downloaded int64
	dl_rate             int
	upload_bytes_init   int64
	//
	torrent  *tt.Torrent
	Category string
	//
	Name          string
	Paused        bool
	ForceDownload bool
	InfoReady     bool
	Tags          *Tags
	//
	ignore_yml_write   time.Time
	onstart_downloaded int64
	onstart_uploaded   int64
}

func NewTorrentWithUserData(tags *Tags) *TorrentWithUserData {
	rc := TorrentWithUserData{}
	rc.onstart_downloaded = -1
	rc.onstart_uploaded = -1
	rc.ClearTags()
	rc.SetTags(tags)
	return &rc
}

func (tu *TorrentWithUserData) AddTags(tags *Tags) {
	if tags == nil {
		return
	}
	for k, v := range *tags {
		tu.Tags.SetIfNew(k, v)
	}
}

func (tu *TorrentWithUserData) ClearTags() {
	tu.Tags = &Tags{}
}

func (tu *TorrentWithUserData) SetTags(tags *Tags) {
	for k, v := range *tags {
		tu.Tags.Set(k, v)
	}
}

func (tu *TorrentWithUserData) SaveTorrent() {
	if tu.Tags.getString("torrent_saved", "no") == "yes" {
		return
	}

	tfile := tu.Tags.getString("fullpath", "")
	log.Debug("%s -> %s", tu.Name, tfile)
	if tfile == "" {
		log.Error("Failed to SaveTorrent: fullpath is empty")
		return
	}
	writer, err := os.Create(tfile)
	if err != nil {
		log.Error("Failed to os.Create: %s - %s", tu.Name, err)
		return
	}
	err = tu.torrent.Metainfo().Write(writer)
	if err != nil {
		log.Error("Failed to Metainfo.Write: %s - %s", tu.Name, err)
		return
	}
	tu.Tags.Set("torrent_saved", "yes")
}

func (tu *TorrentWithUserData) LoadTags() {
	pathname := tu.Tags.getString("tags_fullpath", "")
	log.Debug("loading tags from %s", pathname)
	tags := &Tags{}
	if data, err := ioutil.ReadFile(pathname); err == nil {
		if err := yaml.Unmarshal(data, tags); err != nil {
			log.Error("failed to yaml.Unmarshal: %v : %v", pathname, err)
		}
	}
	log.Debug("loaded tags for : %s from %s", tu.Name, pathname)
	log.Debug("\n%s", tags.String())
	if len(*tags) <= 0 {
		log.Warn("tags length %s is 0, keep old", pathname)
		return
	}
	tu.Tags = tags
	tu.Tags.Validate("force Validate after LoadTags")
}

func (tu *TorrentWithUserData) SaveTags() {
	log.Trace("%s", tu.Name)
	if tu.Tags.Validated() {
		log.Trace("%s tags already saved, there were no changes", tu.Name)
		return
	}
	tu.Tags.Validate("force Validated after SaveTags")
	pathname := tu.Tags.getString("tags_fullpath", "")
	log.Debug("%s -> %s", tu.Name, pathname)
	if pathname == "" {
		log.Error("tag: tags_fullpath is '' - %v", tu.Tags)
		return
	}
	// ignore file events for yml for next 4 seconds, due our own write
	tu.ignore_yml_write = time.Now().Add(time.Second * 4)
	data, err := yaml.Marshal(&tu.Tags)
	if err != nil {
		log.Error("failed to yaml.Marshal: %v : %v", pathname, err)
	} else if err = ioutil.WriteFile(pathname, data, 0664); err != nil {
		log.Error("failed to WriteFile %v - %v", pathname, err)
	}
}

func (tu *TorrentWithUserData) SyncFiles() {
	if !tu.InfoReady {
		panic("Can't sync files while InfoReady is false")
	}
	tu.files = make([]*TorrentFile, len(tu.torrent.Files()))
	for i, v := range tu.torrent.Files() {
		f := NewTorrentFile(tu, v)
		tu.files[i] = f
	}
}

type TorrentInfo struct {
	Name            string            `json:"Name"`
	Size            int64             `json:"Size"`
	FilesCount      int               `json:"FilesCount"`
	Files           []TorrentFileInfo `json:"Files"`
	Seeders         int               `json:"Seeders"`
	Leechers        int               `json:"Leechers"`
	Completed       bool              `json:"Completed"`
	Completion      int               `json:"Completion"`
	BytesDownloaded int64             `json:"BytesDownloaded"`
	BytesUploaded   int64             `json:"BytesUploaded"`
	Paused          bool              `json:"Paused"`
	OpenPlays       int               `json:"OpenPlays"`
	Tags            Tags              `json:"Tags"`
	DownloadRate    int               `json:"DownloadRate"`
}

func (tu *TorrentWithUserData) TorrentInfo() (info TorrentInfo) {
	t := tu.torrent
	if t == nil {
		return
	}
	st := t.Stats()
	files := make([]TorrentFileInfo, 0)
	for _, tf := range tu.Files() {
		files = append(files, tf.Info())
	}
	info = TorrentInfo{
		Name:            t.Name(),
		Size:            t.Length(),
		FilesCount:      len(t.Files()),
		Files:           files,
		Seeders:         st.ConnectedSeeders,
		Leechers:        st.ActivePeers,
		Completed:       t.BytesMissing() <= 0,
		BytesDownloaded: st.BytesReadUsefulData.Int64(),
		BytesUploaded:   st.BytesWrittenData.Int64(),
		Paused:          tu.Paused,
		OpenPlays:       tu.ActiveReaders(),
		Tags:            *tu.Tags,
		Completion:      tu.Completion(),
		DownloadRate:    tu.dl_rate,
	}
	return
}

func (tu *TorrentWithUserData) Files() []*TorrentFile {
	if !tu.InfoReady {
		return make([]*TorrentFile, 0)
	}
	return tu.files
}

func (tu *TorrentWithUserData) GetFileByName(name string) *TorrentFile {
	for _, f := range tu.Files() {
		if f.file.DisplayPath() == name || f.file.Path() == name {
			return f
		}
	}
	return nil
}

func (tu *TorrentWithUserData) GetFileByIndex(index int) *TorrentFile {
	if index < 0 || index >= len(tu.files) {
		return nil
	}
	return tu.files[index]
}

func (tu *TorrentWithUserData) GetFile(val interface{}) (file *TorrentFile) {
	switch val.(type) {
	case string:
		file = tu.GetFileByName(val.(string))
	case int:
		file = tu.GetFileByIndex(val.(int))
	}
	return
}

func (tu *TorrentWithUserData) Pause() {
	if tu.InPlay() {
		return
	}
	if !tu.Paused {
		log.Debug("pausing %s", tu.Name)
		tu.torrent.DisallowDataDownload()
	}
	tu.Paused = true
}

func (tu *TorrentWithUserData) Resume(reason string) {
	if tu.Paused {
		log.Debug("resuming %s - %s", tu.Name, reason)
		tu.torrent.AllowDataDownload()
		tu.torrent.DownloadAll()
		tu.unpaused = time.Now()
		tu.unpaused_downloaded = tu.torrent.BytesCompleted()
	}
	tu.Paused = false
}

func (tu *TorrentWithUserData) Completed() bool {
	return tu.torrent.BytesMissing() <= 0
}

func (tu *TorrentWithUserData) Completion() (percents int) {
	if tu.InfoReady {
		bm := float64(tu.torrent.BytesCompleted())
		tl := float64(tu.torrent.Length())
		percents = int((bm / tl) * 100.0)
	}
	return
}

func (tu *TorrentWithUserData) TrackProgress() {
	if tu.Completed() {
		log.Trace("%s is completed, no SubscribePieceStateChanges", tu.Name)
		return
	}
	log.Trace("SubscribePieceStateChanges %s", tu.Name)
	s := tu.torrent.SubscribePieceStateChanges()
	go func() {
		for {
			max_rate := 0
			max_seeders := 0
			completed := tu.Completed()
			_v := <-s.Values
			log.Trace("TrackProgressFunc: s.Values: %v", _v)
			if _v == nil {
				log.Debug("TrackProgressFunc s.Values is nil, closed ?, leaving TrackProgress func")
				return
			}
			v := _v.(tt.PieceStateChange)
			if v.Complete {
				log.Trace("TrackProgressFunc: %s, piece %d completed", tu.Name, v.Index)
				tdelta := time.Now().Sub(tu.unpaused).Seconds()
				tu.dl_rate = int(float64(tu.torrent.BytesCompleted()-tu.unpaused_downloaded) / tdelta)
				if max_rate < tu.dl_rate {
					max_rate = tu.dl_rate
				}
				info := tu.TorrentInfo()
				if max_seeders < info.Seeders {
					max_seeders = info.Seeders
				}
				tu.Tags.Set("max_rate", max_rate)
				tu.Tags.Set("max_seeders", max_seeders)
			}
			if !completed && tu.Completed() {
				added, _ := time.Parse(time.RFC822, tu.Tags.getString("added", time.Now().Format(time.RFC822)))
				total_time := int(time.Now().Sub(added).Seconds())
				tu.Tags.SetIfNew("completed", time.Now().Format(time.RFC822))
				tu.Tags.SetIfNew("total_time", total_time)
				tu.Tags.SetIfNew("last_rate", tu.dl_rate)
				log.Info("DownloadCompleted for %s, last rate: %d B/s, took: %v sec", tu.Name, tu.dl_rate, total_time)
				s.Close()
			}
		}
	}()
}

func (tu *TorrentWithUserData) ActiveReaders() (count int) {
	for _, f := range tu.Files() {
		count += f.ReadersOpen
	}
	return
}

func (tu *TorrentWithUserData) InPlay() bool {
	return tu.ActiveReaders() > 0
}

func (tu *TorrentWithUserData) CanDelete() (ok bool) {
	if tu.InPlay() {
		log.Trace("%s can't drop, in play yet", tu.Name)
		return false
	}
	if tu.Tags.getString("force_delete", "no") == "yes" {
		log.Trace("force_delete is set, no checks anymore")
	} else {
		if s := tu.Tags.getString("seed_until", ""); s != "" {
			log.Trace("%s seed_until is set %s", tu.Name, s)
			seed_until, err := time.Parse(time.RFC822, s)
			if err != nil {
				log.Trace("%s seed_until is %s but failed to parse to date, keep file", tu.Name, s)
				return false
			}
			delta := seed_until.Sub(time.Now())
			log.Trace("%s delta between seed_until and now %v hours", tu.Name, delta.Hours())
			if delta.Seconds() > 0 {
				log.Trace("%s keep seeding, not expired yet", tu.Name)
				return false
			}
			log.Trace("%s private torrent, expired.. keep deleting", tu.Name)
		}
	}
	log.Info("%s can be deleted", tu.Name)
	return true
}

func (tu *TorrentWithUserData) Drop(reason string) {
	if !tu.CanDelete() {
		return
	}

	log.Trace("%s - %s", tu.Name, reason)
	if !tu.Paused {
		log.Trace("stopping %s before deletion", tu.Name)
		tu.Pause()
		return
	}
	tu.c.RemoveTorrent(tu.Name)
	log.Info("torrent %s removed from client", tu.Name)
	deleteData := tu.Tags.get("delete_data", "no") == "yes"
	if deleteData {
		ddir := tu.Tags.getString("datapath", "/tmp/should_not_exists")
		if stats, err := os.Stat(ddir); err != nil {
			log.Info("%s doesn't exists. hmmm", ddir)
		} else {
			log.Info("%s exists and isDirectory: %v", ddir, stats.IsDir())
		}

		err := os.RemoveAll(ddir)
		log.Info("removed data for %s from %s - removeAll err: %v", tu.Name, ddir, err)
	}
	// delete .torrent, tags.yaml
	_ = os.Remove(tu.Tags.getString("fullpath", "/tmp/should_not_exists"))
	_ = os.Remove(tu.Tags.getString("tags_fullpath", "/tmp/should_not_exists"))
	log.Info("torrent %s (data: %v) removed from client", tu.Name, deleteData)
}

func (tu *TorrentWithUserData) ProcessTags() {
	log.Trace("ProcessTags: %s", tu.Name)

	paused := "no"
	if tu.Paused {
		paused = "yes"
	}
	completed := "no"
	if tu.Completed() {
		completed = "yes"
	}
	tu.Tags.Set("maxConnections", tu.maxConnections)
	tu.Tags.Set("completed", completed)
	tu.Tags.Set("paused", paused)

	if tu.InPlay() {
		tu.Resume("InPlay")
		return
	}
	//
	// populate some info
	info := tu.TorrentInfo()
	if bytes, err := strconv.ParseInt(tu.Tags.getString("upload_bytes", "0"), 10, 64); err == nil {
		if tu.onstart_uploaded < 0 {
			tu.onstart_uploaded = bytes
		}
		tu.Tags.Set("upload_bytes", fmt.Sprintf("%d", info.BytesUploaded+tu.onstart_uploaded))
	}
	if bytes, err := strconv.ParseInt(tu.Tags.getString("downloaded_bytes", "0"), 10, 64); err == nil {
		if tu.onstart_downloaded < 0 {
			tu.onstart_downloaded = bytes
		}
		tu.Tags.Set("downloaded_bytes", fmt.Sprintf("%d", info.BytesDownloaded+tu.onstart_downloaded))
	}

	// save_to_library
	if tu.Tags.getString("save_to_library", "") == "yes" {
		// delete from client, leave data intact (assume download points to BT
		tu.Tags.Remove("save_to_library")
		tu.Tags.Remove("delete_data")
		tu.Tags.Set("delete_it", "yes")
	}

	// delete_it
	if tu.Tags.getString("delete_it", "") == "yes" {
		tu.Drop("delete_it yes")
		return
	}

	// save_torrent tag, just if need manual recovery from running client
	tu.SaveTorrent()
	tu.SaveTags()

	// watch_later
	if tu.Tags.getString("watch_later", "") == "yes" {
		if tu.c.ActivePlays() <= 0 {
			tu.Resume("ActivePlays <= 0")
		}
	}

	// if source - kodi, remove if not used
	if tu.Tags.getString("source", "") == "kodi" {
		tu.Tags.SetIfNew("added", time.Now().Format(time.RFC822))
		added := tu.Tags.getTime("added", time.Now())
		delta := time.Now().Sub(added).Hours()
		if delta < 5 {
			return
		}
		// if kodi - remove anyway after 5 hours.. otherwise, should be saved to watch_later or library
		//st := tu.torrent.Stats()
		//if tu.Completed() {
		//	return
		//}
		//if st.BytesReadUsefulData.Int64() > 0 {
		//	return
		//}
		tu.Tags.Set("delete_data", "yes")
		tu.Tags.Set("delete_it", "yes")
		log.Trace("%s view only torrent, older than 5 hours. drop", tu.Name)
		tu.Drop("view only kodi")
	}

	// resume downloads if no plays active
	if tu.Tags.getString("source", "") != "kodi" {
		if tu.c.ActivePlays() <= 0 {
			tu.Resume("source not kodi, ActivePlays <= 0")
		}
	}

	//  adjust speed
	if tu.Tags.getString("private", "no") == "no" && tu.Completed() && tu.maxConnections != 1 {
		tu.maxConnections = tu.torrent.SetMaxEstablishedConns(1)
		log.Debug("%s completed and not private, set maxConnections to 1", tu.Name)
	}
}
