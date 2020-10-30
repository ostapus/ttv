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
	Dead          bool
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
	rc.Dead = false
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
	tfile := tu.Tags.getString("fullpath", "")
	if tfile == "" {
		log.Error("Failed to SaveTorrent: fullpath is empty")
		return
	}
	if tu.Tags.getString("torrent_saved", "no") == "yes" {
		if _, err := os.Stat(tfile); err == nil {
			return
		} else {
			log.Info("%s torrent_saved, but no torrent file exists: %s", tfile, err)
		}
	}

	log.Debug("saving %s -> %s", tu.Name, tfile)
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

func (tu *TorrentWithUserData) SyncTags() {
	log.Debug("SyncTags starts")
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
	if tu.Tags.getString("paused", "no") == "yes" {
		tu.Pause(tu.Tags.getString("pause_reason", "paused in reloaded tags"))
	} else {
		tu.Resume(tu.Tags.getString("resume_reason", "not paused in reloaded tags"))
	}
	tu.Tags.Validate("force Validate after SyncTags")
	log.Debug("SyncTags done")
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

func (tu *TorrentWithUserData) Pause(reason string) {
	if tu.Paused {
		return
	}
	if tu.InPlay() {
		return
	}
	log.Debug("pausing %s", tu.Name)
	tu.Tags.Set("paused", "yes")
	if reason != "" {
		tu.Tags.Set("pause_reason", reason)
		tu.Tags.Remove("resume_reason")
	}
	// tu.torrent.DisallowDataDownload()
	tu.SetMaxConnections(1)
	tu.Paused = true
}

func (tu *TorrentWithUserData) Resume(reason string) {
	if !tu.Paused {
		return
	}
	log.Debug("resuming %s - %s", tu.Name, reason)
	tu.Tags.Remove("paused")
	tu.Tags.Remove("pause_reason")
	if reason != "" {
		tu.Tags.Set("resume_reason", reason)
	}
	tu.torrent.AllowDataDownload()
	tu.torrent.DownloadAll()
	tu.unpaused = time.Now()
	tu.unpaused_downloaded = tu.torrent.BytesCompleted()
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

func (tu *TorrentWithUserData) HandleDelete() bool {
	reason := tu.Tags.getString("want_drop", "")
	if reason == "" {
		return false
	}
	drop_data := tu.Tags.getString("drop_data", "")
	if !tu.CanDelete() {
		return false
	}

	log.Trace("%s - %s", tu.Name, reason)
	if !tu.Paused {
		log.Trace("stopping %s before deletion", tu.Name)
		tu.Pause("torrent about to be dropped, pausing first")
		return true
	}
	tu.c.RemoveTorrent(tu.Name)
	log.Info("torrent %s removed from client", tu.Name)
	if drop_data == "yes" {
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
	log.Info("torrent %s (data: %v) removed from client", tu.Name, drop_data)
	tu.Dead = true
	return true

}

func (tu *TorrentWithUserData) Drop(reason string, drop_data string, force ...bool) {
	tu.Tags.Set("want_drop", reason)
	if drop_data == "yes" {
		tu.Tags.Set("delete_data", "yes")
	} else if drop_data == "" {
		drop_data = tu.Tags.getString("drop_data", "no")
	}
	if len(force) > 0 {
		tu.Tags.Set("force_delete", "yes")
	}
}

func (tu *TorrentWithUserData) SetMaxConnections(maxConn int) (changed bool) {
	if tu.Paused {
		return false
	}
	changed = false
	if tu.maxConnections != maxConn {
		tu.maxConnections = tu.torrent.SetMaxEstablishedConns(maxConn)
		log.Debug("%s, maxConn: %d now", tu.Name, maxConn)
		changed = true
	}
	tu.Tags.Set("maxConnections", maxConn)
	return changed
}

func (tu *TorrentWithUserData) ProcessTags() {
	defer func() {
		// save_torrent tag, just if need manual recovery from running client
		if !tu.Dead {
			tu.SaveTorrent()
			tu.SaveTags()

			if tu.Paused {
				tu.Pause("")
			} else {
				tu.Resume("")
			}
		}
	}()

	log.Trace("ProcessTags: %s", tu.Name)
	if tu.Completed() {
		tu.Tags.Set("completed", "yes")
	} else {
		tu.Tags.Set("completed", "no")
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

	//
	if tu.InPlay() {
		tu.Resume("InPlay")
	}

	// manual tags update - remove data
	if tu.Tags.getString("kill_it", "no") == "yes" {
		tu.Drop("kill_it tag found", "yes")
	}
	// manual tags update - keep data
	if tu.Tags.getString("drop_it", "no") == "yes" {
		tu.Drop("drop_it tag found", "no")
	}
	// save_to_library
	if tu.Tags.getString("save_to_library", "") == "yes" {
		tu.Drop("moving to library", "no")
	}
	// expire watch_later after 3 days
	if tu.Tags.getString("watch_later", "") == "yes" {
		tu.Tags.SetIfNew("watch_later_expiration", time.Now().Format(time.RFC822))
		expire_after := tu.Tags.getTime("watch_later_expiration", time.Now()).Add(time.Hour * 24 * 3)
		tu.Tags.SetIfNew("watch_later_expiration", expire_after.Format(time.RFC822))
		if time.Now().After(expire_after) {
			log.Debug("watch_later expired, just removing torrent")
			tu.Drop("watch_later expied, not saved to library", "yes")
		}
	}
	// if source kodi - keep for 3 days if not marked for deletion, somehow stuck w/o tagging
	if tu.Tags.getString("source", "") == "kodi" {
		tu.Tags.SetIfNew("added", time.Now().Format(time.RFC822))
		expire_after := tu.Tags.getTime("added", time.Now()).Add(time.Hour * 24 * 3)
		tu.Tags.SetIfNew("kodi_expires_at", expire_after.Format(time.RFC822))
		if time.Now().After(expire_after) {
			tu.Drop("source kodi, too old, not save or anything", "yes")
		}
	}

	if tu.HandleDelete() {
		return
	}

	//  adjust speed
	private := tu.Tags.getString("private", "") == "yes"
	completed := tu.Completed()

	maxConn := 5
	if tu.c.ActivePlays() > 0 {
		if private {
			if completed {
				maxConn = 200
			}
		}
	} else {
		if private || !completed {
			maxConn = 200
		}
	}

	if tu.InPlay() {
		maxConn = 200
	}

	if completed {
		tu.Resume("torrent completed, ok to upload")
	}

	if tu.SetMaxConnections(maxConn) {
		log.Debug("%s completed: %v, private: %s, ActivePlays: %v , set maxConnections to %d",
			tu.Name,
			tu.Completed(),
			tu.Tags.getString("private", "no"),
			tu.c.ActivePlays(),
			tu.maxConnections)
	}
}
